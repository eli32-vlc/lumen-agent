package tools

import "testing"

func TestObjectSchemaUsesEmptyRequiredArray(t *testing.T) {
	schema := objectSchema(map[string]any{
		"status": stringSchema("optional"),
	})

	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("expected required to be []string, got %T", schema["required"])
	}
	if len(required) != 0 {
		t.Fatalf("expected empty required array, got %#v", required)
	}
}

func TestNormalizeSchemaConvertsNilRequiredToEmptyArray(t *testing.T) {
	schema := normalizeSchema(map[string]any{
		"type":     "object",
		"required": nil,
		"properties": map[string]any{
			"status": stringSchema("optional"),
		},
	})

	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("expected required to be []string, got %T", schema["required"])
	}
	if len(required) != 0 {
		t.Fatalf("expected empty required array, got %#v", required)
	}
}
