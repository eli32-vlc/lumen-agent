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

func TestRegistry_IsProtectedPath(t *testing.T) {
	r := &Registry{
		secretsPath: "/home/user/workspace/.lumen/secrets.json",
	}

	tests := []struct {
		path     string
		expected bool
	}{
		{"/home/user/workspace/.lumen/secrets.json", true},
		{"/home/user/workspace/.lumen/other.json", false},
		{"/home/user/workspace/secrets.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := r.isProtectedPath(tt.path); got != tt.expected {
				t.Errorf("isProtectedPath() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRegistry_PathTouchesProtectedPath(t *testing.T) {
	r := &Registry{
		secretsPath: "/home/user/workspace/.lumen/secrets.json",
	}

	tests := []struct {
		path     string
		expected bool
	}{
		{"/home/user/workspace/.lumen/secrets.json", true},
		{"/home/user/workspace/.lumen", true},
		{"/home/user/workspace", true},
		{"/home/user/workspace/src", false},
		{"/home/user/workspace/.lumen/other.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := r.pathTouchesProtectedPath(tt.path); got != tt.expected {
				t.Errorf("pathTouchesProtectedPath() = %v, want %v", got, tt.expected)
			}
		})
	}
}
