package api

import "testing"

func TestFinalizeToolInput_RepairMalformedJSON(t *testing.T) {
	acc := newStreamAccumulator()
	acc.setBlockType(0, BlockTypeToolUse)
	acc.blocks[0].ToolName = "edit"
	acc.appendToolInputJSON(0, `{file_path: "a.go", old_string: "x", new_string: "y",}`)

	acc.finalizeToolInput(0)

	if acc.blocks[0].ToolInputParseError != "" {
		t.Fatalf("unexpected parse error: %s", acc.blocks[0].ToolInputParseError)
	}
	if acc.blocks[0].ToolInput["file_path"] != "a.go" {
		t.Fatalf("file_path=%v", acc.blocks[0].ToolInput["file_path"])
	}
}

func TestFinalizeToolInput_RepairFailureDiagnostic(t *testing.T) {
	acc := newStreamAccumulator()
	acc.setBlockType(0, BlockTypeToolUse)
	acc.blocks[0].ToolName = "edit"
	acc.appendToolInputJSON(0, `{totally broken`)

	acc.finalizeToolInput(0)

	if acc.blocks[0].ToolInputParseError == "" {
		t.Fatal("expected parse error")
	}
	if len(acc.blocks[0].ToolInput) != 0 {
		t.Fatalf("expected empty input, got %#v", acc.blocks[0].ToolInput)
	}
}

func TestFinalizeToolInput_ValidJSONPassthrough(t *testing.T) {
	acc := newStreamAccumulator()
	acc.setBlockType(0, BlockTypeToolUse)
	acc.blocks[0].ToolName = "bash"
	acc.appendToolInputJSON(0, `{"command":"echo hi"}`)

	acc.finalizeToolInput(0)

	if acc.blocks[0].ToolInputParseError != "" {
		t.Fatalf("unexpected parse error: %s", acc.blocks[0].ToolInputParseError)
	}
	if acc.blocks[0].ToolInput["command"] != "echo hi" {
		t.Fatalf("command=%v", acc.blocks[0].ToolInput["command"])
	}
}
