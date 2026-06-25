package jsonrepair_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/jsonrepair"
)

func TestRepair_ValidJSONPassthrough(t *testing.T) {
	input := `{"file_path": "main.go", "old_string": "foo", "new_string": "bar"}`
	got, err := jsonrepair.Repair(input, "edit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{
		"file_path":  "main.go",
		"old_string": "foo",
		"new_string": "bar",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	// Input bytes must be unchanged for valid JSON
	if _, err := jsonrepair.Repair(input, ""); err != nil {
		t.Fatalf("valid JSON should pass through: %v", err)
	}
}

func TestRepair_ValidJSONWindowsPathPassthrough(t *testing.T) {
	input := `{"file_path": "D:\\test\\file.py"}`
	got, err := jsonrepair.Repair(input, "write")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `D:\test\file.py`
	if got["file_path"] != want {
		t.Fatalf("file_path=%q, want %q", got["file_path"], want)
	}
}

func TestRepair_MarkdownFenceStripping(t *testing.T) {
	input := "```json\n{\"command\": \"ls\"}\n```"
	got, err := jsonrepair.Repair(input, "bash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["command"] != "ls" {
		t.Fatalf("got command=%v, want ls", got["command"])
	}
}

func TestRepair_TrailingCommas(t *testing.T) {
	input := `{"a": 1, "b": 2,}`
	got, err := jsonrepair.Repair(input, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["a"].(float64) != 1 || got["b"].(float64) != 2 {
		t.Fatalf("unexpected values: %#v", got)
	}
}

func TestRepair_UnquotedKeys(t *testing.T) {
	input := `{file_path: "test.go", old_string: "x", new_string: "y"}`
	got, err := jsonrepair.Repair(input, "edit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["file_path"] != "test.go" {
		t.Fatalf("file_path=%v", got["file_path"])
	}
}

func TestRepair_SingleQuotedStrings(t *testing.T) {
	input := `{'key': 'value', 'num': 1}`
	got, err := jsonrepair.Repair(input, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["key"] != "value" {
		t.Fatalf("key=%v", got["key"])
	}
}

func TestRepair_WindowsPaths(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "tab in path",
			input: `{"file_path": "D:\test\file.py"}`,
			want:  `D:\test\file.py`,
		},
		{
			name:  "formfeed in path",
			input: `{"file_path": "C:\foo\bar.txt"}`,
			want:  `C:\foo\bar.txt`,
		},
		{
			name:  "users documents",
			input: `{"file_path": "C:\Users\name\Documents\file.go"}`,
			want:  `C:\Users\name\Documents\file.go`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := jsonrepair.Repair(tt.input, "write")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			fp, _ := got["file_path"].(string)
			if fp != tt.want {
				t.Fatalf("file_path=%q, want %q", fp, tt.want)
			}
		})
	}
}

func TestRepair_TruncatedJSON(t *testing.T) {
	input := `{"file_path": "a.go", "content": "package main`
	got, err := jsonrepair.Repair(input, "write")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["file_path"] != "a.go" {
		t.Fatalf("file_path=%v", got["file_path"])
	}
	content, _ := got["content"].(string)
	if !strings.Contains(content, "package main") {
		t.Fatalf("content=%q", content)
	}
}

func TestRepair_EditToolExtraction(t *testing.T) {
	// Simulates broken JSON where old_string contains unescaped quotes
	input := `{"file_path": "main.go", "old_string": "func foo() {", "new_string": "func bar() {`
	got, err := jsonrepair.Repair(input, "edit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["file_path"] != "main.go" {
		t.Fatalf("file_path=%v", got["file_path"])
	}
	oldStr, _ := got["old_string"].(string)
	if !strings.Contains(oldStr, "func foo") {
		t.Fatalf("old_string=%q", oldStr)
	}
}

func TestRepair_EditSemiStructuredExtraction(t *testing.T) {
	input := `{file_path: "x.go", old_string: package x

func main() {}, new_string: package x

func main() { fmt.Println("hi") }`
	got, err := jsonrepair.Repair(input, "edit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["file_path"] != "x.go" {
		t.Fatalf("file_path=%v", got["file_path"])
	}
	if _, ok := got["old_string"]; !ok {
		t.Fatal("missing old_string")
	}
	if _, ok := got["new_string"]; !ok {
		t.Fatal("missing new_string")
	}
}

func TestRepair_DiagnosticError(t *testing.T) {
	_, err := jsonrepair.Repair(`{broken`, "edit")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "file_path") {
		t.Fatalf("expected required key in error, got: %s", msg)
	}
	if strings.Contains(msg, "column") {
		t.Fatalf("should not contain raw unmarshal position error: %s", msg)
	}
}

func TestExtractJSON_Basic(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{"a": 1}`, `{"a": 1}`},
		{`Here: {"a": 1} done`, `{"a": 1}`},
		{`no json`, `no json`},
	}
	for _, tt := range tests {
		got := jsonrepair.ExtractJSON(tt.input)
		if got != tt.expected {
			t.Errorf("ExtractJSON(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestExtractJSON_IgnoresBracesInStrings(t *testing.T) {
	input := `prefix {"key": "value with { brace"} suffix {"ignored": true}`
	got := jsonrepair.ExtractJSON(input)
	want := `{"key": "value with { brace"}`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractJSON_NestedObjects(t *testing.T) {
	input := `{"outer": {"inner": 1}}`
	got := jsonrepair.ExtractJSON(input)
	if got != input {
		t.Fatalf("got %q", got)
	}
}

func TestRepair_Deterministic(t *testing.T) {
	input := `{file_path: "a.go", content: "hello",}`
	for i := 0; i < 3; i++ {
		a, errA := jsonrepair.Repair(input, "write")
		b, errB := jsonrepair.Repair(input, "write")
		if (errA == nil) != (errB == nil) {
			t.Fatal("inconsistent error presence")
		}
		if errA != nil {
			if errA.Error() != errB.Error() {
				t.Fatal("inconsistent error messages")
			}
			continue
		}
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		if string(ja) != string(jb) {
			t.Fatalf("non-deterministic output: %s vs %s", ja, jb)
		}
	}
}

func TestRepair_WriteToolRequiredKeys(t *testing.T) {
	got, err := jsonrepair.Repair(`{"file_path": "f.txt"}`, "write")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["file_path"] != "f.txt" {
		t.Fatalf("file_path=%v", got["file_path"])
	}
}

func TestRepair_ArrayTrailingComma(t *testing.T) {
	input := `{"items": [1, 2, 3,]}`
	got, err := jsonrepair.Repair(input, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items, ok := got["items"].([]any)
	if !ok || len(items) != 3 {
		t.Fatalf("items=%#v", got["items"])
	}
}
