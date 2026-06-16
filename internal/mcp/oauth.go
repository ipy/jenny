package mcp

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/ipy/jenny/internal/constants"
)

// OAuthToken represents an OAuth 2.1 token response.
type OAuthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	TokenType    string    `json:"token_type,omitempty"` // typically "Bearer"
}

// OAuthTokenStore handles persistent storage of OAuth tokens.
type OAuthTokenStore struct {
	dir string
}

// NewOAuthTokenStore creates a new OAuth token store.
// Tokens are stored in $JENNY_HOME/mcp-oauth/ directory.
func NewOAuthTokenStore() *OAuthTokenStore {
	return &OAuthTokenStore{
		dir: filepath.Join(constants.JennyHomeDir(), "mcp-oauth"),
	}
}

// tokenFileName generates a safe filename for a server URL.
func (s *OAuthTokenStore) tokenFileName(serverURL string) string {
	// Use URL-safe base64 encoding to create a safe filename
	encoded := base64.URLEncoding.EncodeToString([]byte(serverURL))
	return filepath.Join(s.dir, "token_"+encoded+".json")
}

// Store saves an OAuth token for a server URL.
func (s *OAuthTokenStore) Store(serverURL string, token *OAuthToken) error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}

	data, err := json.Marshal(token)
	if err != nil {
		return err
	}

	filename := s.tokenFileName(serverURL)
	return os.WriteFile(filename, data, 0600)
}

// Load retrieves an OAuth token for a server URL.
// Returns (token, nil) if found, or (nil, os.ErrNotExist) if not found.
// Callers should use errors.Is(err, os.ErrNotExist) to check for missing tokens.
func (s *OAuthTokenStore) Load(serverURL string) (*OAuthToken, error) {
	filename := s.tokenFileName(serverURL)

	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var token OAuthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}

	return &token, nil
}
