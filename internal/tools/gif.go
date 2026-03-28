package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (r *Registry) registerGIFTool() {
	if !r.cfg.GIFs.Enabled {
		return
	}

	r.register(
		"search_gifs",
		"Search GIPHY for GIFs that match a query and return direct GIF URLs.",
		objectSchema(map[string]any{
			"query": stringSchema("What kind of GIF to search for."),
			"limit": integerSchema("Optional number of GIFs to return.", 1),
		}, "query"),
		r.handleSearchGIFs,
	)
}

func (r *Registry) handleSearchGIFs(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}

	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	query := strings.TrimSpace(input.Query)
	if query == "" {
		return "", fmt.Errorf("query must not be empty")
	}

	apiKey, err := r.cfg.ResolveGIFAPIKey()
	if err != nil {
		return "", err
	}

	limit := input.Limit
	if limit <= 0 {
		limit = r.cfg.GIFs.SearchLimit
	}
	if cfgLimit := r.cfg.GIFs.SearchLimit; cfgLimit > 0 && limit > cfgLimit {
		limit = cfgLimit
	}

	params := url.Values{}
	params.Set("api_key", apiKey)
	params.Set("q", query)
	params.Set("limit", strconv.Itoa(limit))
	params.Set("rating", r.cfg.GIFs.ContentFilter)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.gifAPIBase, "/")+"/search?"+params.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("build GIF search request: %w", err)
	}

	resp, err := r.gifClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search GIFs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("search GIFs: API error (%d)", resp.StatusCode)
	}

	var parsed struct {
		Data []struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			URL    string `json:"url"`
			Images struct {
				Original struct {
					URL string `json:"url"`
				} `json:"original"`
				FixedWidthSmall struct {
					URL string `json:"url"`
				} `json:"fixed_width_small"`
			} `json:"images"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode GIF search response: %w", err)
	}

	results := make([]map[string]any, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		gifURL := strings.TrimSpace(item.Images.Original.URL)
		previewURL := strings.TrimSpace(item.Images.FixedWidthSmall.URL)
		results = append(results, map[string]any{
			"id":          strings.TrimSpace(item.ID),
			"title":       strings.TrimSpace(item.Title),
			"page_url":    strings.TrimSpace(item.URL),
			"gif_url":     gifURL,
			"preview_url": previewURL,
		})
	}

	return jsonResult(map[string]any{
		"query":   query,
		"results": results,
	})
}
