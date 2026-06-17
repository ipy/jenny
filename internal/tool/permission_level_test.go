package tool

import (
	"testing"
)

// TestPermissionLevel_String verifies the string representation of each level.
func TestPermissionLevel_String(t *testing.T) {
	tests := []struct {
		level PermissionLevel
		want  string
	}{
		{PermissionRead, "read"},
		{PermissionAnalyze, "analyze"},
		{PermissionEdit, "edit"},
		{PermissionExecute, "execute"},
		{PermissionUnrestricted, "unrestricted"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.level.String(); got != tt.want {
				t.Errorf("PermissionLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
			}
		})
	}
}

// TestParsePermissionLevel verifies parsing from string values.
// Covers AC8: valid levels from CLI/env/config, and error on invalid values.
func TestParsePermissionLevel(t *testing.T) {
	tests := []struct {
		input   string
		want    PermissionLevel
		wantErr bool
	}{
		{"read", PermissionRead, false},
		{"analyze", PermissionAnalyze, false},
		{"edit", PermissionEdit, false},
		{"execute", PermissionExecute, false},
		{"unrestricted", PermissionUnrestricted, false},
		// Case sensitivity: must be exact lowercase
		{"READ", PermissionRead, true},
		{"Edit", PermissionEdit, true},
		// Invalid values
		{"", 0, true},
		{"admin", 0, true},
		{"write", 0, true},
		{"skip", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParsePermissionLevel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParsePermissionLevel(%q) expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("ParsePermissionLevel(%q) unexpected error: %v", tt.input, err)
				}
				if got != tt.want {
					t.Errorf("ParsePermissionLevel(%q) = %d, want %d", tt.input, got, tt.want)
				}
			}
		})
	}
}

// TestPermissionLevel_CapabilityQueries tests the capability query methods
// that drive CommandGate and tool-level behavior.
// AC1: read blocks Bash and file writes
// AC2: analyze allows read-only Bash, blocks writes
// AC3: edit allows read-only Bash + file writes (current default)
// AC4: execute allows Bash mutation, pattern blocks enforced
// AC5: unrestricted skips all gates
func TestPermissionLevel_CapabilityQueries(t *testing.T) {
	tests := []struct {
		name                 string
		level                PermissionLevel
		wantBashAllowed      bool
		wantPipelineEnforced bool
		wantCommandChecked   bool
		wantWriteAllowed     bool
		wantPathConstrained  bool
		wantReadBeforeWrite  bool
	}{
		{
			name: "read", level: PermissionRead,
			wantBashAllowed: false, wantPipelineEnforced: false,
			wantCommandChecked: false, wantWriteAllowed: false,
			wantPathConstrained: true, wantReadBeforeWrite: true,
		},
		{
			name: "analyze", level: PermissionAnalyze,
			wantBashAllowed: true, wantPipelineEnforced: true,
			wantCommandChecked: true, wantWriteAllowed: false,
			wantPathConstrained: true, wantReadBeforeWrite: true,
		},
		{
			name: "edit", level: PermissionEdit,
			wantBashAllowed: true, wantPipelineEnforced: true,
			wantCommandChecked: true, wantWriteAllowed: true,
			wantPathConstrained: true, wantReadBeforeWrite: true,
		},
		{
			name: "execute", level: PermissionExecute,
			wantBashAllowed: true, wantPipelineEnforced: false,
			wantCommandChecked: true, wantWriteAllowed: true,
			wantPathConstrained: true, wantReadBeforeWrite: true,
		},
		{
			name: "unrestricted", level: PermissionUnrestricted,
			wantBashAllowed: true, wantPipelineEnforced: false,
			wantCommandChecked: false, wantWriteAllowed: true,
			wantPathConstrained: false, wantReadBeforeWrite: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.level.BashAllowed(); got != tt.wantBashAllowed {
				t.Errorf("BashAllowed() = %v, want %v", got, tt.wantBashAllowed)
			}
			if got := tt.level.PipelineEnforced(); got != tt.wantPipelineEnforced {
				t.Errorf("PipelineEnforced() = %v, want %v", got, tt.wantPipelineEnforced)
			}
			if got := tt.level.CommandChecked(); got != tt.wantCommandChecked {
				t.Errorf("CommandChecked() = %v, want %v", got, tt.wantCommandChecked)
			}
			if got := tt.level.WriteAllowed(); got != tt.wantWriteAllowed {
				t.Errorf("WriteAllowed() = %v, want %v", got, tt.wantWriteAllowed)
			}
			if got := tt.level.PathConstrained(); got != tt.wantPathConstrained {
				t.Errorf("PathConstrained() = %v, want %v", got, tt.wantPathConstrained)
			}
			if got := tt.level.ReadBeforeWrite(); got != tt.wantReadBeforeWrite {
				t.Errorf("ReadBeforeWrite() = %v, want %v", got, tt.wantReadBeforeWrite)
			}
		})
	}
}

// TestPermissionLevel_DefaultIsEdit verifies that the default permission level
// matches current behavior (AC3: edit is the default).
func TestPermissionLevel_DefaultIsEdit(t *testing.T) {
	if DefaultPermissionLevel != PermissionEdit {
		t.Errorf("DefaultPermissionLevel = %v, want PermissionEdit", DefaultPermissionLevel)
	}
}

// TestResolvePermissionLevel tests the resolution logic when both
// --dangerously-skip-permissions and --permission-level are specified.
// AC5: --dangerously-skip-permissions maps to unrestricted
// AC6: both specified → unrestricted + warning
func TestResolvePermissionLevel(t *testing.T) {
	tests := []struct {
		name             string
		skipPerms        bool
		permLevelStr     string
		wantLevel        PermissionLevel
		wantWarnContains string // empty = no warning expected
	}{
		{
			name: "neither set → default edit",
			skipPerms: false, permLevelStr: "",
			wantLevel: PermissionEdit, wantWarnContains: "",
		},
		{
			name: "only skip-perms → unrestricted (AC5)",
			skipPerms: true, permLevelStr: "",
			wantLevel: PermissionUnrestricted, wantWarnContains: "",
		},
		{
			name: "only permission-level → that level",
			skipPerms: false, permLevelStr: "execute",
			wantLevel: PermissionExecute, wantWarnContains: "",
		},
		{
			name: "both specified → unrestricted + warning (AC6)",
			skipPerms: true, permLevelStr: "edit",
			wantLevel: PermissionUnrestricted, wantWarnContains: "dangerously-skip-permissions",
		},
		{
			name: "skip-perms + permission-level=unrestricted → unrestricted, no warning",
			skipPerms: true, permLevelStr: "unrestricted",
			wantLevel: PermissionUnrestricted, wantWarnContains: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLevel, gotWarn := ResolvePermissionLevel(tt.skipPerms, tt.permLevelStr)
			if gotLevel != tt.wantLevel {
				t.Errorf("ResolvePermissionLevel() level = %v, want %v", gotLevel, tt.wantLevel)
			}
			if tt.wantWarnContains != "" {
				if gotWarn == "" {
					t.Errorf("ResolvePermissionLevel() expected warning containing %q, got empty", tt.wantWarnContains)
				}
			} else if gotWarn != "" {
				t.Errorf("ResolvePermissionLevel() unexpected warning: %q", gotWarn)
			}
		})
	}
}