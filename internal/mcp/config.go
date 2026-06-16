// Package mcp provides MCP server configuration loading and management.
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// MCPServerDef defines an MCP server configuration.
type MCPServerDef struct {
	Command       string            // The command to execute
	Args          []string          // Command arguments
	Env           map[string]string // Environment variables
	URL           string            // Server URL (for HTTP transport)
	Headers       map[string]string // HTTP headers (for URL transport)
	TokenEndpoint string            // OAuth 2.1 token endpoint URL
	ClientID      string            // OAuth 2.1 client ID
	ClientSecret  string            // OAuth 2.1 client secret
	Scopes        []string          // OAuth 2.1 requested scopes
}

// LoadConfig loads MCP configuration from the given file paths.
// If strict is true, only servers defined in the given files are allowed.
// Multiple files are merged, with later files overriding earlier ones on name collision.
func LoadConfig(paths []string, strict bool) (map[string]MCPServerDef, error) {
	result := make(map[string]MCPServerDef)

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading config file %q: %w", path, err)
		}

		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parsing config file %q: %w", path, err)
		}

		serversRaw, ok := raw["mcpServers"]
		if !ok {
			return nil, fmt.Errorf("config file %q missing required key 'mcpServers'", path)
		}

		servers, ok := serversRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("config file %q: mcpServers must be a JSON object", path)
		}

		for name, defRaw := range servers {
			def, ok := defRaw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("config file %q: server %q must be a JSON object", path, name)
			}

			serverDef := MCPServerDef{}

			if cmd, ok := def["command"].(string); ok {
				serverDef.Command = expandEnv(cmd)
			}
			if argsRaw, ok := def["args"].([]any); ok {
				serverDef.Args = make([]string, 0, len(argsRaw))
				for _, a := range argsRaw {
					if s, ok := a.(string); ok {
						serverDef.Args = append(serverDef.Args, expandEnv(s))
					}
				}
			}
			if envRaw, ok := def["env"].(map[string]any); ok {
				serverDef.Env = make(map[string]string)
				for k, v := range envRaw {
					if s, ok := v.(string); ok {
						serverDef.Env[k] = expandEnv(s)
					}
				}
			}
			if url, ok := def["url"].(string); ok {
				serverDef.URL = expandEnv(url)
			}
			if headersRaw, ok := def["headers"].(map[string]any); ok {
				serverDef.Headers = make(map[string]string)
				for k, v := range headersRaw {
					if s, ok := v.(string); ok {
						serverDef.Headers[k] = expandEnv(s)
					}
				}
			}
			if tokenEndpoint, ok := def["tokenEndpoint"].(string); ok {
				serverDef.TokenEndpoint = expandEnv(tokenEndpoint)
			}
			if clientID, ok := def["clientId"].(string); ok {
				serverDef.ClientID = expandEnv(clientID)
			}
			if clientSecret, ok := def["clientSecret"].(string); ok {
				serverDef.ClientSecret = expandEnv(clientSecret)
			}
			if scopesRaw, ok := def["scopes"].([]any); ok {
				serverDef.Scopes = make([]string, 0, len(scopesRaw))
				for _, s := range scopesRaw {
					if scope, ok := s.(string); ok {
						serverDef.Scopes = append(serverDef.Scopes, scope)
					}
				}
			}

			result[name] = serverDef
		}
	}

	// AC3: strict mode only honours CLI-supplied config. The --strict-mcp-config
	// flag is also wired through the tool Registry to suppress built-in tools
	// (see internal/tool/registry.go WithStrictMCP). MCP config loading itself
	// never pulls from plugin sources, so the flag is a no-op at this layer.

	return result, nil
}

// expandEnv expands environment variable references in a string.
// Supports ${VAR} and ${VAR:-default} forms.
// Unset variables without defaults resolve to empty strings.
func expandEnv(s string) string {
	var result strings.Builder
	result.Grow(len(s))

	i := 0
	for i < len(s) {
		// Look for ${ pattern
		start := strings.Index(s[i:], "${")
		if start == -1 {
			// No more env vars, append rest of string
			result.WriteString(s[i:])
			break
		}

		// Write everything before ${...}
		result.WriteString(s[i : i+start])
		i += start

		// Find closing brace
		end := strings.Index(s[i+2:], "}")
		if end == -1 {
			// No closing brace, treat rest as literal
			result.WriteString(s[i:])
			break
		}
		end = i + 2 + end // absolute position

		// Extract var name and default
		inner := s[i+2 : end]
		varName, defaultVal, hasDefault := strings.Cut(inner, ":-")

		if hasDefault {
			if val := os.Getenv(varName); val != "" {
				result.WriteString(val)
			} else {
				result.WriteString(defaultVal)
			}
		} else {
			result.WriteString(os.Getenv(varName))
		}

		i = end + 1
	}

	return result.String()
}