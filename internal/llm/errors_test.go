package llm

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestIsTimeoutError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "context deadline", err: context.DeadlineExceeded, want: true},
		{name: "wrapped deadline", err: fmt.Errorf("send request: %w", context.DeadlineExceeded), want: true},
		{name: "http client timeout", err: errors.New("send request: Post https://example.invalid: context deadline exceeded (Client.Timeout exceeded while awaiting headers)"), want: true},
		{name: "generic timeout wording", err: errors.New("request timed out while waiting for upstream"), want: true},
		{name: "non timeout", err: errors.New("decode response: unexpected end of JSON input"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTimeoutError(tt.err); got != tt.want {
				t.Fatalf("IsTimeoutError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsRetriableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "timeout", err: context.DeadlineExceeded, want: true},
		{name: "api 429", err: errors.New("API error (429): rate limit"), want: true},
		{name: "api 503", err: errors.New("API error (503): overloaded"), want: true},
		{name: "api 522", err: errors.New("API error (522): "), want: true},
		{name: "api 400", err: errors.New("API error (400): bad request"), want: false},
		{name: "connection reset", err: errors.New("read: connection reset by peer"), want: true},
		{name: "decode error", err: errors.New("decode response: invalid character"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetriableError(tt.err); got != tt.want {
				t.Fatalf("IsRetriableError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
