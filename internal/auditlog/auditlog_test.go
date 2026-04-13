package auditlog

import "testing"

func TestSanitizeAuditDataRedactsMessageFields(t *testing.T) {
	input := map[string]any{
		"message": "hello",
		"content": "secret",
		"nested": map[string]any{
			"raw_response": map[string]any{"choices": []any{"kept out"}},
			"usage":        map[string]any{"tokens": 12},
		},
		"safe": "keep",
	}

	got := sanitizeAuditData(input)

	if got["message"] != redactedValue {
		t.Fatalf("expected message to be redacted, got %#v", got["message"])
	}
	if got["content"] != redactedValue {
		t.Fatalf("expected content to be redacted, got %#v", got["content"])
	}
	nested, ok := got["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %#v", got["nested"])
	}
	if nested["raw_response"] != redactedValue {
		t.Fatalf("expected raw_response to be redacted, got %#v", nested["raw_response"])
	}
	if nested["usage"] == redactedValue {
		t.Fatalf("expected usage to remain visible, got %#v", nested["usage"])
	}
	if got["safe"] != "keep" {
		t.Fatalf("expected safe field to remain visible, got %#v", got["safe"])
	}
}
