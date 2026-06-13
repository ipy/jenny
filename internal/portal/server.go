// Package portal provides a sidecar HTTP/SSE server for the Jenny WebUI Portal.
package portal

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// webuiDist is the embedded webui build output.
//go:embed webui/dist/*
var webuiDist embed.FS

// embedFS is the filesystem used for serving static files.
var embedFS fs.FS = webuiDist

// SetEmbedFS sets the filesystem for static file serving (for testing).
func SetEmbedFS(fs fs.FS) {
	embedFS = fs
}

// getSubFS returns a sub-filesystem for webui/dist.
func getSubFS() (fs.FS, error) {
	sub, err := fs.Sub(webuiDist, "webui/dist")
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// Portal represents the WebUI Portal sidecar server.
type Portal struct {
	port        int
	authToken   string
	server      *http.Server
	idleTimer   *time.Timer
	idleTimeout time.Duration
	mu          sync.Mutex
	lastAccess  time.Time
	lockPath    string
	pid         int
}

// LockfileData represents the lockfile contents.
type LockfileData struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	AuthToken string `json:"auth_token"`
}

// Start creates and starts a new Portal server.
func Start(ctx context.Context, jennyDir string) (*Portal, error) {
	return startWithConfig(ctx, jennyDir, 10*time.Minute)
}

// startWithConfig creates and starts a portal with a custom idle timeout (for testing).
func startWithConfig(ctx context.Context, jennyDir string, idleTimeout time.Duration) (*Portal, error) {
	lockPath := filepath.Join(jennyDir, "portal.lock")

	// Check for existing lockfile
	if data, err := os.ReadFile(lockPath); err == nil {
		var lf LockfileData
		if json.Unmarshal(data, &lf) == nil {
			// Check if the pid is alive
			if proc, err := os.FindProcess(lf.PID); err == nil {
				if err := proc.Signal(syscall.Signal(0)); err == nil {
					return nil, fmt.Errorf("portal already running on port %d", lf.Port)
				}
			}
			// Stale lockfile - clean it up
			os.Remove(lockPath)
		}
	}

	// Generate auth token (64 hex chars from 32 random bytes)
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generating auth token: %w", err)
	}
	authToken := hex.EncodeToString(tokenBytes)

	// Find a random high port
	port, err := findAvailablePort(33669)
	if err != nil {
		return nil, fmt.Errorf("finding available port: %w", err)
	}

	p := &Portal{
		port:        port,
		authToken:   authToken,
		idleTimeout: idleTimeout,
		lockPath:    lockPath,
		pid:         os.Getpid(),
		lastAccess:  time.Now(),
	}

	// Set up HTTP server
	mux := http.NewServeMux()
	p.setupRoutes(mux)

	p.server = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: mux,
	}

	// Start the server
	go func() {
		if err := p.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("portal server error: %v", err)
		}
	}()

	// Wait for server to be ready
	for i := 0; i < 50; i++ {
		conn, err := net.DialTimeout("tcp", p.server.Addr, 10*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Write lockfile
	if err := p.writeLockfile(); err != nil {
		p.server.Shutdown(context.Background())
		return nil, fmt.Errorf("writing lockfile: %w", err)
	}

	// Start idle timer
	p.resetIdleTimer()
	go p.runIdleMonitor(ctx)

	return p, nil
}

// Port returns the port the server is listening on.
func (p *Portal) Port() int {
	return p.port
}

// AuthToken returns the auth token.
func (p *Portal) AuthToken() string {
	return p.authToken
}

// Shutdown gracefully shuts down the portal server.
func (p *Portal) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Stop idle timer
	if p.idleTimer != nil {
		p.idleTimer.Stop()
	}

	// Remove lockfile
	os.Remove(p.lockPath)

	// Shutdown server
	return p.server.Shutdown(ctx)
}

// resetIdleTimer resets the idle timeout timer.
func (p *Portal) resetIdleTimer() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastAccess = time.Now()
	if p.idleTimer != nil {
		p.idleTimer.Stop()
	}
	p.idleTimer = time.AfterFunc(p.idleTimeout, func() {
		// Timer expired - exit
		os.Remove(p.lockPath)
		os.Exit(0)
	})
}

// runIdleMonitor monitors the last access time and triggers shutdown.
func (p *Portal) runIdleMonitor(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.mu.Lock()
			idle := time.Since(p.lastAccess)
			p.mu.Unlock()
			if idle >= p.idleTimeout {
				os.Remove(p.lockPath)
				os.Exit(0)
			}
		}
	}
}

// writeLockfile writes the lockfile atomically.
func (p *Portal) writeLockfile() error {
	lf := LockfileData{
		PID:       p.pid,
		Port:      p.port,
		AuthToken: p.authToken,
	}

	data, err := json.Marshal(lf)
	if err != nil {
		return fmt.Errorf("marshaling lockfile: %w", err)
	}

	// Write to temp file then rename for atomicity
	tmpPath := p.lockPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("writing temp lockfile: %w", err)
	}
	if err := os.Rename(tmpPath, p.lockPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming lockfile: %w", err)
	}

	return nil
}

// findAvailablePort finds an available port starting from the given port.
func findAvailablePort(start int) (int, error) {
	for port := start; port < 65535; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			ln.Close()
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available port found")
}