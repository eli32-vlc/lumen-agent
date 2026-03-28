package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHandleSearchWeb(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", req.Method)
		}
		if req.URL.Host != "api.duckduckgo.com" {
			t.Fatalf("unexpected host %s", req.URL.Host)
		}
		if req.URL.Query().Get("q") != "golang" {
			t.Fatalf("unexpected query %q", req.URL.Query().Get("q"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"Heading":"Go",
				"AbstractText":"Go is a programming language.",
				"AbstractURL":"https://go.dev",
				"Answer":"",
				"Definition":"",
				"RelatedTopics":[{"Text":"Go documentation","FirstURL":"https://pkg.go.dev"}]
			}`)),
		}, nil
	})}

	prev := http.DefaultClient
	http.DefaultClient = client
	defer func() { http.DefaultClient = prev }()

	registry := &Registry{}
	result, err := registry.handleSearchWeb(context.Background(), json.RawMessage(`{"query":"golang"}`))
	if err != nil {
		t.Fatalf("handleSearchWeb returned error: %v", err)
	}
	if !strings.Contains(result, `"heading": "Go"`) {
		t.Fatalf("expected heading in result, got %s", result)
	}
	if !strings.Contains(result, `"url": "https://pkg.go.dev"`) {
		t.Fatalf("expected related URL in result, got %s", result)
	}
}

func TestHandleSearchNews(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "hn.algolia.com" {
			t.Fatalf("unexpected host %s", req.URL.Host)
		}
		if req.URL.Query().Get("query") != "openai" {
			t.Fatalf("unexpected query %q", req.URL.Query().Get("query"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"hits":[{"title":"OpenAI launch","url":"https://example.com/story","author":"sam","created_at":"2026-03-27T00:00:00Z","points":42,"story_text":"launch details"}]
			}`)),
		}, nil
	})}

	prev := http.DefaultClient
	http.DefaultClient = client
	defer func() { http.DefaultClient = prev }()

	registry := &Registry{}
	result, err := registry.handleSearchNews(context.Background(), json.RawMessage(`{"query":"openai","limit":3}`))
	if err != nil {
		t.Fatalf("handleSearchNews returned error: %v", err)
	}
	if !strings.Contains(result, `"title": "OpenAI launch"`) {
		t.Fatalf("expected title in result, got %s", result)
	}
}

func TestHandleGetWeather(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "geocoding-api.open-meteo.com":
			if req.URL.Query().Get("name") != "brisbane" {
				t.Fatalf("unexpected location query %q", req.URL.Query().Get("name"))
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{
					"results":[{"name":"Brisbane","country":"Australia","admin1":"Queensland","latitude":-27.4679,"longitude":153.0281,"timezone":"Australia/Brisbane"}]
				}`)),
			}, nil
		case "api.open-meteo.com":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{
					"current":{"temperature_2m":28.1,"apparent_temperature":30.0,"weather_code":1,"wind_speed_10m":12.4},
					"daily":{"time":["2026-03-27"],"weather_code":[1],"temperature_2m_max":[30.1],"temperature_2m_min":[22.0]}
				}`)),
			}, nil
		default:
			t.Fatalf("unexpected host %s", req.URL.Host)
			return nil, nil
		}
	})}

	prev := http.DefaultClient
	http.DefaultClient = client
	defer func() { http.DefaultClient = prev }()

	registry := &Registry{}
	result, err := registry.handleGetWeather(context.Background(), json.RawMessage(`{"location":"brisbane","days":1}`))
	if err != nil {
		t.Fatalf("handleGetWeather returned error: %v", err)
	}
	if !strings.Contains(result, `"location": "Brisbane, Queensland, Australia"`) {
		t.Fatalf("expected location in result, got %s", result)
	}
	if !strings.Contains(result, `"description": "partly cloudy"`) {
		t.Fatalf("expected decoded weather description in result, got %s", result)
	}
}
