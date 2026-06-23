package tool_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ipy/jenny/internal/tool"
)

// ============================================================================
// Schema validation
// ============================================================================

func TestValidSchema_ParsesSuccessfully(t *testing.T) {
	validSchema := `{"type": "object", "properties": {"name": {"type": "string"}}}`
	schema, err := tool.ValidateStructuredSchema(validSchema)
	if err != nil {
		t.Fatalf("expected valid schema to parse, got error: %v", err)
	}
	if schema == nil {
		t.Fatal("expected non-nil schema")
	}
	if schema["type"] != "object" {
		t.Errorf("expected type 'object', got %v", schema["type"])
	}
}

func TestInvalidJSON_FailsParsing(t *testing.T) {
	invalidJSON := `not json at all`
	_, err := tool.ValidateStructuredSchema(invalidJSON)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("expected 'invalid JSON' error, got: %v", err)
	}
}

func TestTypeNotString_Fails(t *testing.T) {
	invalidSchema := `{"type": 123}`
	_, err := tool.ValidateStructuredSchema(invalidSchema)
	if err == nil {
		t.Fatal("expected error for non-string type, got nil")
	}
	if !strings.Contains(err.Error(), "type must be a string") {
		t.Errorf("expected 'type must be a string' error, got: %v", err)
	}
}

func TestTypeNotObject_Fails(t *testing.T) {
	invalidSchema := `{"type": "string"}`
	_, err := tool.ValidateStructuredSchema(invalidSchema)
	if err == nil {
		t.Fatal("expected error for non-object type, got nil")
	}
	if !strings.Contains(err.Error(), "type must be 'object'") {
		t.Errorf("expected 'type must be object' error, got: %v", err)
	}
}

func TestPropertiesNotObject_Fails(t *testing.T) {
	invalidSchema := `{"type": "object", "properties": "not an object"}`
	_, err := tool.ValidateStructuredSchema(invalidSchema)
	if err == nil {
		t.Fatal("expected error for non-object properties, got nil")
	}
	if !strings.Contains(err.Error(), "properties must be an object") {
		t.Errorf("expected 'properties must be an object' error, got: %v", err)
	}
}

func TestMissingType_Accepted(t *testing.T) {
	// Schema without type is technically valid (any JSON value allowed)
	schema := `{"properties": {"name": {"type": "string"}}}`
	parsed, err := tool.ValidateStructuredSchema(schema)
	if err != nil {
		t.Fatalf("schema without type should be accepted, got error: %v", err)
	}
	if parsed == nil {
		t.Fatal("expected non-nil schema")
	}
}

// ============================================================================
// StructuredOutputTool creation and interface
// ============================================================================

func TestStructuredOutputTool_Name(t *testing.T) {
	schema := map[string]any{"type": "object"}
	outputTool := tool.NewStructuredOutputTool(schema)
	if outputTool.Name() != "StructuredOutput" {
		t.Errorf("expected name 'StructuredOutput', got %q", outputTool.Name())
	}
}

func TestStructuredOutputTool_Description(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}}
	outputTool := tool.NewStructuredOutputTool(schema)
	desc := outputTool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
	if !strings.Contains(desc, "structured") {
		t.Errorf("description should mention 'structured', got: %s", desc)
	}
}

func TestStructuredOutputTool_InputSchema(t *testing.T) {
	schema := map[string]any{"type": "object"}
	outputTool := tool.NewStructuredOutputTool(schema)
	inputSchema := outputTool.InputSchema()
	if inputSchema == nil {
		t.Fatal("expected non-nil input schema")
	}
	props, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in input schema")
	}
	if _, ok := props["value"]; !ok {
		t.Error("expected 'value' property in input schema")
	}
	if _, ok := props["format"]; !ok {
		t.Error("expected 'format' property in input schema")
	}
}

func TestInputSchema_DynamicUserSchema(t *testing.T) {
	// InputSchema should dynamically incorporate user schema properties
	userSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "number"},
		},
		"required": []any{"name"},
	}
	outputTool := tool.NewStructuredOutputTool(userSchema)
	inputSchema := outputTool.InputSchema()

	props, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in input schema")
	}

	valueProp, ok := props["value"].(map[string]any)
	if !ok {
		t.Fatal("expected 'value' property to be a map")
	}

	// The value property should have the user schema's properties
	// Check that 'properties' from user schema is in the value property
	valueProps, ok := valueProp["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected 'properties' in value property")
	}

	if _, ok := valueProps["name"]; !ok {
		t.Error("FAIL: 'value.properties' should include 'name' from user schema")
	} else {
		t.Log("PASS: 'value.properties' includes 'name' from user schema")
	}

	if _, ok := valueProps["age"]; !ok {
		t.Error("FAIL: 'value.properties' should include 'age' from user schema")
	} else {
		t.Log("PASS: 'value.properties' includes 'age' from user schema")
	}

	// The value property should have type "object" (inherited from user schema)
	if valueProp["type"] != "object" {
		t.Errorf("FAIL: 'value' property should have type 'object', got %v", valueProp["type"])
	} else {
		t.Log("PASS: 'value' property has type 'object'")
	}

	// The value property should have required: ["value"]
	if req, ok := valueProp["required"].([]any); ok {
		if len(req) != 1 || req[0] != "value" {
			t.Errorf("FAIL: 'value.required' should be [\"value\"], got %v", req)
		} else {
			t.Log("PASS: 'value.required' is [\"value\"]")
		}
	} else {
		t.Error("FAIL: 'value.required' should exist and be [\"value\"]")
	}
}

func TestStructuredOutputTool_Reset(t *testing.T) {
	schema := map[string]any{"type": "object"}
	outputTool := tool.NewStructuredOutputTool(schema)

	if outputTool.IsEmitted() {
		t.Error("expected IsEmitted() to be false initially")
	}

	// Simulate a call
	_, _ = outputTool.Execute(context.Background(), map[string]any{
		"value": map[string]any{"result": "test"},
	}, t.TempDir())

	if !outputTool.IsEmitted() {
		t.Error("expected IsEmitted() to be true after Execute")
	}

	// Reset
	outputTool.Reset()
	if outputTool.IsEmitted() {
		t.Error("expected IsEmitted() to be false after Reset")
	}
}

// ============================================================================
// Execute enforces single emission per turn
// ============================================================================

func TestSecondExecute_ReturnsError(t *testing.T) {
	schema := map[string]any{"type": "object"}
	outputTool := tool.NewStructuredOutputTool(schema)

	// First call succeeds
	result1, err1 := outputTool.Execute(context.Background(), map[string]any{
		"value": map[string]any{"result": "first"},
	}, t.TempDir())
	if err1 != nil {
		t.Fatalf("first Execute failed: %v", err1)
	}
	if result1.IsError {
		t.Error("first Execute should not be error")
	}

	// Second call fails
	result2, err2 := outputTool.Execute(context.Background(), map[string]any{
		"value": map[string]any{"result": "second"},
	}, t.TempDir())
	if err2 != nil {
		t.Fatalf("second Execute returned error: %v", err2)
	}
	if result2 == nil {
		t.Fatal("expected non-nil result")
	}
	if !result2.IsError {
		t.Error("second Execute should be error")
	}
	if !strings.Contains(result2.Content, "already emitted") {
		t.Errorf("expected 'already emitted' error, got: %s", result2.Content)
	}
}

func TestExecuteWithMissingValue_ReturnsError(t *testing.T) {
	schema := map[string]any{"type": "object"}
	outputTool := tool.NewStructuredOutputTool(schema)

	result, err := outputTool.Execute(context.Background(), map[string]any{}, t.TempDir())
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.IsError {
		t.Error("Execute with missing value should be error")
	}
	if !strings.Contains(result.Content, "value field is required") {
		t.Errorf("expected 'value field is required' error, got: %s", result.Content)
	}
}

func TestExecuteWithValidValue_ReturnsValidatedJSON(t *testing.T) {
	schema := map[string]any{"type": "object"}
	outputTool := tool.NewStructuredOutputTool(schema)

	result, err := outputTool.Execute(context.Background(), map[string]any{
		"value": map[string]any{
			"name": "test",
			"age":  42,
		},
	}, t.TempDir())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Errorf("Execute should not be error, got: %s", result.Content)
	}

	// Verify output is valid JSON
	var output any
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Errorf("output should be valid JSON: %v", err)
	}
}

func TestExecuteWithRequiredFields_ValidatesPresence(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"name"},
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	outputTool := tool.NewStructuredOutputTool(schema)

	// Missing required field
	result, err := outputTool.Execute(context.Background(), map[string]any{
		"value": map[string]any{"age": 42},
	}, t.TempDir())
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.IsError {
		t.Error("Execute with missing required field should be error")
	}
	if !strings.Contains(result.Content, "required field") {
		t.Errorf("expected 'required field' error, got: %s", result.Content)
	}

	// Present required field
	result2, err2 := outputTool.Execute(context.Background(), map[string]any{
		"value": map[string]any{"name": "test"},
	}, t.TempDir())
	if err2 != nil {
		t.Fatalf("Execute returned error: %v", err2)
	}
	if result2 == nil {
		t.Fatal("expected non-nil result")
	}
	if result2.IsError {
		t.Errorf("Execute with required field present should succeed, got: %s", result2.Content)
	}
}
