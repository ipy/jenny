package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/ipy/jenny/internal/constants"
)

// OAuthToken represents an OAuth 2.1 token response.
type OAuthToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	TokenType    string `json:"token_type,omitempty"` // typically "Bearer"
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
	// Use base64 encoding to create a safe filename
	encoded := make([]byte, len(serverURL)*2)
	for i := 0; i < len(serverURL); i++ {
		encoded[i*2] = byte(serverURL[i] >> 8)
		encoded[i*2+1] = byte(serverURL[i] & 0xFF)
	}
	return filepath.Join(s.dir, "token_"+base64Encode(encoded)+".json")
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
// Returns (token, true) if found, or (nil, false) if not found.
// Returns an error only on actual file read errors (not on missing files).
func (s *OAuthTokenStore) Load(serverURL string) (*OAuthToken, bool, error) {
	filename := s.tokenFileName(serverURL)

	data, err := os.ReadFile(filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}

	var token OAuthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, false, err
	}

	return &token, true, nil
}

// base64Encode encodes bytes to URL-safe base64 without padding.
func base64Encode(data []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

	result := make([]byte, (len(data)+2)/3*4)
	for i := 0; i < len(data); i += 3 {
		var val uint32
		var n int
		for j := i; j < len(data) && j < i+3; j++ {
			val = val<<8 | uint32(data[j])
			n++
		}
		// Pad with zeros to get 24 bits
		val <<= (3 - n) * 8

		// Extract 6-bit groups
		for j := 0; j < n+1; j++ {
			idx := (val >> uint((3-j)*6)) & 0x3F
			result[i/3*4+j] = alphabet[idx]
		}
	}

	// Remove any zero bytes at the end (padding artifacts)
	result = result[:len(result):len(result)]
	for len(result) > 0 && result[len(result)-1] == 0 {
		result = result[:len(result)-1]
	}

	return string(result)
}