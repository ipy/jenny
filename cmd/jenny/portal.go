package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ipy/jenny/internal/constants"
	"github.com/ipy/jenny/internal/portal"
)

// runPortal is the main function for the portal command.
func runPortal(ctx context.Context) error {
	p, err := portal.Start(ctx, constants.JennyHomeDir())
	if err != nil {
		return err
	}

	fmt.Printf("Portal started at http://127.0.0.1:%d\n", p.Port())
	fmt.Printf("Auth token: %s\n", p.AuthToken())

	// Block on signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sig:
	case <-ctx.Done():
	}

	return p.Shutdown(context.Background())
}