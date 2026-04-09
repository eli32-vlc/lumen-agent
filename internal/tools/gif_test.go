package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"element-orion/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestHandleSearchGIFs(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", req.Method)
		}
		if req.URL.Path != "/v1/gifs/search" {
			t.Fatalf("unexpected path %s", req.URL.Path)
		}
		query := req.URL.Query()
		if query.Get("api_key") != "giphy-key" {
			t.Fatalf("unexpected key %q", query.Get("api_key"))
		}
		if query.Get("q") != "happy cat" {
			t.Fatalf("unexpected query %q", query.Get("q"))
		}
		if query.Get("limit") != "2" {
			t.Fatalf("unexpected limit %q", query.Get("limit"))
		}
		if query.Get("rating") != "pg" {
			t.Fatalf("unexpected rating %q", query.Get("rating"))
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"gif-1","title":"happy cat","url":"https://giphy.example/gif-1","images":{"original":{"url":"https://cdn.example/gif-1.gif"},"fixed_width_small":{"url":"https://cdn.example/gif-1-small.gif"}}}]}`)),
		}, nil
	})}

	registry := &Registry{
		cfg: config.Config{
			GIFs: config.GIFConfig{
				Enabled:       true,
				Provider:      "giphy",
				APIKey:        "giphy-key",
				SearchLimit:   5,
				ContentFilter: "pg",
			},
		},
		gifAPIBase: "https://api.giphy.com/v1/gifs",
		gifClient:  client,
	}

	result, err := registry.handleSearchGIFs(context.Background(), json.RawMessage(`{"query":"happy cat","limit":2}`))
	if err != nil {
		t.Fatalf("handleSearchGIFs returned error: %v", err)
	}

	if !strings.Contains(result, `"gif_url": "https://cdn.example/gif-1.gif"`) {
		t.Fatalf("expected GIF URL in result, got %s", result)
	}
}
