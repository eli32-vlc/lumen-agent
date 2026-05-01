package secrets

import (
	"testing"
)

func TestStore_Substitute(t *testing.T) {
	s := &Store{
		data: map[string]string{
			"GITHUB_PASS": "actual-password",
			"OPENAI_KEY":  "sk-12345",
		},
	}

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "login with {{secret:GITHUB_PASS}}",
			expected: "login with actual-password",
		},
		{
			input:    "key is {{secret:OPENAI_KEY}}",
			expected: "key is sk-12345",
		},
		{
			input:    "no secrets here",
			expected: "no secrets here",
		},
		{
			input:    "missing {{secret:NON_EXISTENT}}",
			expected: "missing {{secret:NON_EXISTENT}}",
		},
		{
			input:    "multiple {{secret:GITHUB_PASS}} and {{secret:OPENAI_KEY}}",
			expected: "multiple actual-password and sk-12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := s.Substitute(tt.input); got != tt.expected {
				t.Errorf("Substitute() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestStore_Redact(t *testing.T) {
	s := &Store{
		data: map[string]string{
			"GITHUB_PASS": "actual-password",
			"OPENAI_KEY":  "sk-12345",
			"SHORT":       "abc",
		},
	}

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "error: actual-password failed",
			expected: "error: [REDACTED:GITHUB_PASS] failed",
		},
		{
			input:    "response: sk-12345 received",
			expected: "response: [REDACTED:OPENAI_KEY] received",
		},
		{
			input:    "data: abcdef",
			expected: "data: [REDACTED:SHORT]def",
		},
		{
			input:    "mixed: actual-password and sk-12345",
			expected: "mixed: [REDACTED:GITHUB_PASS] and [REDACTED:OPENAI_KEY]",
		},
		{
			input:    "no secrets here",
			expected: "no secrets here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := s.Redact(tt.input); got != tt.expected {
				t.Errorf("Redact() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestStore_Redact_Overlapping(t *testing.T) {
	s := &Store{
		data: map[string]string{
			"LONG":  "password123",
			"SHORT": "password",
		},
	}

	input := "my password123 is secret"
	expected := "my [REDACTED:LONG] is secret"

	if got := s.Redact(input); got != expected {
		t.Errorf("Redact() with overlapping secrets = %v, want %v", got, expected)
	}
}

func TestStore_AddDeleteSave(t *testing.T) {
	tempFile := t.TempDir() + "/secrets.json"
	
	// Create an empty store and add a secret
	s, err := Load(tempFile)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	
	if err := s.Add("NEW_SECRET", "12345"); err != nil {
		t.Fatalf("Add() failed: %v", err)
	}
	
	names := s.Names()
	if len(names) != 1 || names[0] != "NEW_SECRET" {
		t.Errorf("Names() = %v, want [NEW_SECRET]", names)
	}
	
	// Load again to verify Save worked
	s2, err := Load(tempFile)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	
	names2 := s2.Names()
	if len(names2) != 1 || names2[0] != "NEW_SECRET" {
		t.Errorf("Names() = %v, want [NEW_SECRET]", names2)
	}
	
	// Delete the secret
	if err := s2.Delete("NEW_SECRET"); err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}
	
	names3 := s2.Names()
	if len(names3) != 0 {
		t.Errorf("Names() = %v, want empty", names3)
	}
	
	// Load again to verify Save after Delete worked
	s3, err := Load(tempFile)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	
	names4 := s3.Names()
	if len(names4) != 0 {
		t.Errorf("Names() = %v, want empty", names4)
	}
}
