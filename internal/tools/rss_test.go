package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"element-orion/internal/config"
)

func TestParseRSSFeedDocumentSupportsRSS2(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example RSS</title>
    <link>https://example.com</link>
    <item>
      <title>First item</title>
      <link>https://example.com/posts/1</link>
      <description><![CDATA[<p>Hello <strong>world</strong></p>]]></description>
      <pubDate>Tue, 07 Apr 2026 09:15:00 GMT</pubDate>
    </item>
  </channel>
</rss>`

	parsed, err := parseRSSFeedDocument([]byte(xml), "https://example.com/feed.xml")
	if err != nil {
		t.Fatalf("parseRSSFeedDocument returned error: %v", err)
	}
	if parsed.Title != "Example RSS" {
		t.Fatalf("expected title %q, got %q", "Example RSS", parsed.Title)
	}
	if len(parsed.Items) != 1 {
		t.Fatalf("expected one item, got %d", len(parsed.Items))
	}
	if parsed.Items[0].Summary != "Hello world" {
		t.Fatalf("expected stripped summary, got %q", parsed.Items[0].Summary)
	}
}

func TestParseRSSFeedDocumentSupportsAtom(t *testing.T) {
	xml := `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Example Atom</title>
  <link href="https://example.com" rel="alternate"></link>
  <entry>
    <title>Entry one</title>
    <link href="https://example.com/entry-1"></link>
    <summary>Short summary</summary>
    <updated>2026-04-07T09:15:00Z</updated>
  </entry>
</feed>`

	parsed, err := parseRSSFeedDocument([]byte(xml), "https://example.com/atom.xml")
	if err != nil {
		t.Fatalf("parseRSSFeedDocument returned error: %v", err)
	}
	if parsed.Title != "Example Atom" {
		t.Fatalf("expected title %q, got %q", "Example Atom", parsed.Title)
	}
	if len(parsed.Items) != 1 || parsed.Items[0].URL != "https://example.com/entry-1" {
		t.Fatalf("expected atom entry URL to be parsed, got %#v", parsed.Items)
	}
}

func TestRSSFeedToolLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example Feed</title>
    <link>https://example.com</link>
    <item>
      <title>Latest post</title>
      <link>https://example.com/posts/latest</link>
      <description><![CDATA[<p>Testing latest post</p>]]></description>
      <pubDate>Tue, 07 Apr 2026 09:15:00 GMT</pubDate>
    </item>
  </channel>
</rss>`))
	}))
	defer server.Close()

	cfg := config.Config{
		App: config.AppConfig{
			SessionDir: t.TempDir(),
		},
	}

	registry, err := NewRegistry(cfg)
	if err != nil {
		t.Fatalf("NewRegistry returned error: %v", err)
	}
	defer registry.Close()
	registry.rssClient = server.Client()

	addPayload, err := json.Marshal(map[string]any{"url": server.URL, "title": "My Feed"})
	if err != nil {
		t.Fatalf("marshal add payload: %v", err)
	}

	addResult, err := registry.handleAddRSSFeed(context.Background(), addPayload)
	if err != nil {
		t.Fatalf("handleAddRSSFeed returned error: %v", err)
	}
	if !strings.Contains(addResult, "My Feed") {
		t.Fatalf("expected add result to include custom title, got %q", addResult)
	}

	listResult, err := registry.handleListRSSFeeds(context.Background(), nil)
	if err != nil {
		t.Fatalf("handleListRSSFeeds returned error: %v", err)
	}
	if !strings.Contains(listResult, "My Feed") {
		t.Fatalf("expected list result to include saved feed, got %q", listResult)
	}

	store, err := loadRSSFeedStore(filepath.Join(cfg.App.SessionDir, "rss-feeds.json"))
	if err != nil {
		t.Fatalf("loadRSSFeedStore returned error: %v", err)
	}
	if len(store.Feeds) != 1 {
		t.Fatalf("expected one saved feed, got %d", len(store.Feeds))
	}

	readPayload, err := json.Marshal(map[string]any{"id": store.Feeds[0].ID, "limit": 1})
	if err != nil {
		t.Fatalf("marshal read payload: %v", err)
	}
	readResult, err := registry.handleReadRSSFeed(context.Background(), readPayload)
	if err != nil {
		t.Fatalf("handleReadRSSFeed returned error: %v", err)
	}
	if !strings.Contains(readResult, "Latest post") {
		t.Fatalf("expected read result to include latest item, got %q", readResult)
	}

	removePayload, err := json.Marshal(map[string]any{"id": store.Feeds[0].ID})
	if err != nil {
		t.Fatalf("marshal remove payload: %v", err)
	}
	removeResult, err := registry.handleRemoveRSSFeed(context.Background(), removePayload)
	if err != nil {
		t.Fatalf("handleRemoveRSSFeed returned error: %v", err)
	}
	if !strings.Contains(removeResult, "My Feed") {
		t.Fatalf("expected remove result to include removed feed, got %q", removeResult)
	}
}
