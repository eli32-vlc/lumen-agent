package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const maxRSSFeedBytes = 2 << 20

var htmlTagPattern = regexp.MustCompile(`(?s)<[^>]*>`)

type rssFeedSubscription struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

type rssFeedStore struct {
	Feeds []rssFeedSubscription `json:"feeds"`
}

type rssParsedFeed struct {
	Title       string
	HomePageURL string
	Items       []rssParsedFeedItem
}

type rssParsedFeedItem struct {
	Title       string
	URL         string
	Summary     string
	PublishedAt time.Time
}

type rss2Document struct {
	Channel rss2Channel `xml:"channel"`
}

type rss2Channel struct {
	Title string     `xml:"title"`
	Link  string     `xml:"link"`
	Items []rss2Item `xml:"item"`
}

type rss2Item struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Published   string `xml:"published"`
	Updated     string `xml:"updated"`
}

type atomDocument struct {
	Title   string      `xml:"title"`
	Links   []atomLink  `xml:"link"`
	Entries []atomEntry `xml:"entry"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}

type atomEntry struct {
	Title     string     `xml:"title"`
	Summary   string     `xml:"summary"`
	Content   string     `xml:"content"`
	ID        string     `xml:"id"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
	Links     []atomLink `xml:"link"`
}

func (r *Registry) registerRSSTools() {
	r.register(
		"add_rss_feed",
		"Add and persist an RSS or Atom feed subscription managed by the app runtime.",
		objectSchema(map[string]any{
			"url":   stringSchema("RSS or Atom feed URL."),
			"title": stringSchema("Optional custom title to use instead of the feed title."),
		}, "url"),
		r.handleAddRSSFeed,
	)

	r.register(
		"list_rss_feeds",
		"List saved RSS and Atom feed subscriptions managed by the app runtime.",
		objectSchema(map[string]any{}),
		r.handleListRSSFeeds,
	)

	r.register(
		"read_rss_feed",
		"Fetch the latest items from one saved RSS or Atom feed subscription.",
		objectSchema(map[string]any{
			"id":    stringSchema("Saved feed ID."),
			"limit": integerSchema("Optional number of items to return, up to 25.", 1),
		}, "id"),
		r.handleReadRSSFeed,
	)

	r.register(
		"remove_rss_feed",
		"Delete one saved RSS or Atom feed subscription from the app runtime.",
		objectSchema(map[string]any{
			"id": stringSchema("Saved feed ID."),
		}, "id"),
		r.handleRemoveRSSFeed,
	)
}

func (r *Registry) handleAddRSSFeed(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	feedURL, err := normalizeRSSFeedURL(input.URL)
	if err != nil {
		return "", err
	}

	release, err := r.ensureLockManager().Acquire(ctx, "rss-feeds")
	if err != nil {
		return "", err
	}
	defer release()

	storePath, err := r.rssFeedsPath()
	if err != nil {
		return "", err
	}

	store, err := loadRSSFeedStore(storePath)
	if err != nil {
		return "", err
	}

	for _, existing := range store.Feeds {
		if existing.URL == feedURL {
			return jsonResult(map[string]any{
				"feed":           existing,
				"already_exists": true,
			})
		}
	}

	parsedFeed, err := r.fetchRSSFeed(ctx, feedURL)
	if err != nil {
		return "", err
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = strings.TrimSpace(parsedFeed.Title)
	}
	if title == "" {
		title = fallbackRSSFeedTitle(feedURL)
	}

	subscription := rssFeedSubscription{
		ID:        rssFeedID(),
		Title:     title,
		URL:       feedURL,
		CreatedAt: time.Now().UTC(),
	}

	store.Feeds = append(store.Feeds, subscription)
	slices.SortFunc(store.Feeds, func(a, b rssFeedSubscription) int {
		return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
	})

	if err := saveRSSFeedStore(storePath, store); err != nil {
		return "", err
	}

	return jsonResult(map[string]any{
		"feed": subscription,
		"source": map[string]any{
			"title":         strings.TrimSpace(parsedFeed.Title),
			"home_page_url": strings.TrimSpace(parsedFeed.HomePageURL),
		},
		"latest_items": rssFeedItemsToResult(parsedFeed.Items, 3),
	})
}

func (r *Registry) handleListRSSFeeds(ctx context.Context, _ json.RawMessage) (string, error) {
	release, err := r.ensureLockManager().Acquire(ctx, "rss-feeds")
	if err != nil {
		return "", err
	}
	defer release()

	storePath, err := r.rssFeedsPath()
	if err != nil {
		return "", err
	}

	store, err := loadRSSFeedStore(storePath)
	if err != nil {
		return "", err
	}
	return jsonResult(map[string]any{
		"feeds": store.Feeds,
		"count": len(store.Feeds),
	})
}

func (r *Registry) handleReadRSSFeed(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	release, err := r.ensureLockManager().Acquire(ctx, "rss-feeds")
	if err != nil {
		return "", err
	}
	defer release()

	storePath, err := r.rssFeedsPath()
	if err != nil {
		return "", err
	}

	store, err := loadRSSFeedStore(storePath)
	if err != nil {
		return "", err
	}

	subscription, err := findRSSFeedByID(store, input.ID)
	if err != nil {
		return "", err
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 25 {
		limit = 25
	}

	parsedFeed, err := r.fetchRSSFeed(ctx, subscription.URL)
	if err != nil {
		return "", err
	}

	return jsonResult(map[string]any{
		"feed": subscription,
		"source": map[string]any{
			"title":         strings.TrimSpace(parsedFeed.Title),
			"home_page_url": strings.TrimSpace(parsedFeed.HomePageURL),
		},
		"items": rssFeedItemsToResult(parsedFeed.Items, limit),
	})
}

func (r *Registry) handleRemoveRSSFeed(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		ID string `json:"id"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	release, err := r.ensureLockManager().Acquire(ctx, "rss-feeds")
	if err != nil {
		return "", err
	}
	defer release()

	storePath, err := r.rssFeedsPath()
	if err != nil {
		return "", err
	}

	store, err := loadRSSFeedStore(storePath)
	if err != nil {
		return "", err
	}

	removed := rssFeedSubscription{}
	nextFeeds := make([]rssFeedSubscription, 0, len(store.Feeds))
	for _, feed := range store.Feeds {
		if feed.ID == strings.TrimSpace(input.ID) {
			removed = feed
			continue
		}
		nextFeeds = append(nextFeeds, feed)
	}
	if removed.ID == "" {
		return "", fmt.Errorf("rss feed %q was not found", strings.TrimSpace(input.ID))
	}

	store.Feeds = nextFeeds
	if err := saveRSSFeedStore(storePath, store); err != nil {
		return "", err
	}

	return jsonResult(map[string]any{
		"removed": removed,
		"count":   len(store.Feeds),
	})
}

func (r *Registry) rssFeedsPath() (string, error) {
	sessionDir := strings.TrimSpace(r.cfg.App.SessionDir)
	if sessionDir == "" {
		return "", fmt.Errorf("rss feed tools require app.session_dir to be configured")
	}
	return filepath.Join(sessionDir, "rss-feeds.json"), nil
}

func loadRSSFeedStore(path string) (rssFeedStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return rssFeedStore{Feeds: []rssFeedSubscription{}}, nil
		}
		return rssFeedStore{}, fmt.Errorf("read rss feed store: %w", err)
	}

	var store rssFeedStore
	if err := json.Unmarshal(data, &store); err != nil {
		return rssFeedStore{}, fmt.Errorf("decode rss feed store: %w", err)
	}
	if store.Feeds == nil {
		store.Feeds = []rssFeedSubscription{}
	}
	return store, nil
}

func saveRSSFeedStore(path string, store rssFeedStore) error {
	if store.Feeds == nil {
		store.Feeds = []rssFeedSubscription{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create rss feed store directory: %w", err)
	}

	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encode rss feed store: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write rss feed store: %w", err)
	}
	return nil
}

func findRSSFeedByID(store rssFeedStore, id string) (rssFeedSubscription, error) {
	id = strings.TrimSpace(id)
	for _, feed := range store.Feeds {
		if feed.ID == id {
			return feed, nil
		}
	}
	return rssFeedSubscription{}, fmt.Errorf("rss feed %q was not found", id)
}

func normalizeRSSFeedURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("url must not be empty")
	}

	parsed, err := neturl.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse rss feed url: %w", err)
	}
	if !parsed.IsAbs() {
		return "", fmt.Errorf("rss feed url must be absolute")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("rss feed url must use http or https")
	}
	parsed.Fragment = ""
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String(), nil
}

func fallbackRSSFeedTitle(feedURL string) string {
	parsed, err := neturl.Parse(feedURL)
	if err != nil {
		return "RSS Feed"
	}
	if host := strings.TrimSpace(parsed.Hostname()); host != "" {
		return host
	}
	return "RSS Feed"
}

func (r *Registry) fetchRSSFeed(ctx context.Context, feedURL string) (rssParsedFeed, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return rssParsedFeed{}, fmt.Errorf("build rss request: %w", err)
	}
	req.Header.Set("User-Agent", "Element-Orion/1.0 (+rss)")
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/xml, text/xml;q=0.9, */*;q=0.1")

	resp, err := r.rssClient.Do(req)
	if err != nil {
		return rssParsedFeed{}, fmt.Errorf("fetch rss feed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return rssParsedFeed{}, fmt.Errorf("fetch rss feed: HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRSSFeedBytes))
	if err != nil {
		return rssParsedFeed{}, fmt.Errorf("read rss feed: %w", err)
	}
	if len(data) == 0 {
		return rssParsedFeed{}, fmt.Errorf("rss feed response was empty")
	}

	parsed, err := parseRSSFeedDocument(data, feedURL)
	if err != nil {
		return rssParsedFeed{}, err
	}
	if strings.TrimSpace(parsed.Title) == "" {
		parsed.Title = fallbackRSSFeedTitle(feedURL)
	}
	return parsed, nil
}

func parseRSSFeedDocument(data []byte, sourceURL string) (rssParsedFeed, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return rssParsedFeed{}, fmt.Errorf("rss feed response was empty")
	}

	var rssDoc rss2Document
	if err := xml.Unmarshal(trimmed, &rssDoc); err == nil && (strings.TrimSpace(rssDoc.Channel.Title) != "" || len(rssDoc.Channel.Items) > 0) {
		items := make([]rssParsedFeedItem, 0, len(rssDoc.Channel.Items))
		for _, item := range rssDoc.Channel.Items {
			itemURL := strings.TrimSpace(item.Link)
			if itemURL == "" {
				itemURL = strings.TrimSpace(item.GUID)
			}
			items = append(items, rssParsedFeedItem{
				Title:       firstNonEmpty(item.Title, itemURL, "Untitled item"),
				URL:         itemURL,
				Summary:     summarizeFeedText(item.Description),
				PublishedAt: parseRSSPublishedAt(item.PubDate, item.Published, item.Updated),
			})
		}
		return rssParsedFeed{
			Title:       strings.TrimSpace(rssDoc.Channel.Title),
			HomePageURL: strings.TrimSpace(rssDoc.Channel.Link),
			Items:       items,
		}, nil
	}

	var atomDoc atomDocument
	if err := xml.Unmarshal(trimmed, &atomDoc); err == nil && (strings.TrimSpace(atomDoc.Title) != "" || len(atomDoc.Entries) > 0) {
		items := make([]rssParsedFeedItem, 0, len(atomDoc.Entries))
		for _, entry := range atomDoc.Entries {
			itemURL := atomLinkURL(entry.Links)
			if itemURL == "" {
				itemURL = strings.TrimSpace(entry.ID)
			}
			summary := entry.Summary
			if strings.TrimSpace(summary) == "" {
				summary = entry.Content
			}
			items = append(items, rssParsedFeedItem{
				Title:       firstNonEmpty(entry.Title, itemURL, "Untitled item"),
				URL:         itemURL,
				Summary:     summarizeFeedText(summary),
				PublishedAt: parseRSSPublishedAt(entry.Published, entry.Updated),
			})
		}
		return rssParsedFeed{
			Title:       strings.TrimSpace(atomDoc.Title),
			HomePageURL: atomLinkURL(atomDoc.Links),
			Items:       items,
		}, nil
	}

	return rssParsedFeed{}, fmt.Errorf("response did not contain a supported RSS or Atom feed")
}

func atomLinkURL(links []atomLink) string {
	for _, link := range links {
		if strings.TrimSpace(link.Href) == "" {
			continue
		}
		if rel := strings.TrimSpace(strings.ToLower(link.Rel)); rel == "" || rel == "alternate" {
			return strings.TrimSpace(link.Href)
		}
	}
	for _, link := range links {
		if href := strings.TrimSpace(link.Href); href != "" {
			return href
		}
	}
	return ""
}

func parseRSSPublishedAt(values ...string) time.Time {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		time.RFC850,
		"Mon, 02 Jan 2006 15:04:05 MST",
		"Mon, 02 Jan 2006 15:04 MST",
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, layout := range layouts {
			if parsed, err := time.Parse(layout, value); err == nil {
				return parsed.UTC()
			}
		}
	}
	return time.Time{}
}

func summarizeFeedText(value string) string {
	value = html.UnescapeString(strings.TrimSpace(value))
	value = htmlTagPattern.ReplaceAllString(value, " ")
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 320 {
		return strings.TrimSpace(value[:317]) + "..."
	}
	return value
}

func rssFeedItemsToResult(items []rssParsedFeedItem, limit int) []map[string]any {
	if limit <= 0 || limit > len(items) {
		limit = len(items)
	}
	result := make([]map[string]any, 0, limit)
	for _, item := range items[:limit] {
		result = append(result, map[string]any{
			"title":        item.Title,
			"url":          item.URL,
			"summary":      item.Summary,
			"published_at": item.PublishedAt,
		})
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func rssFeedID() string {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Sprintf("rss-%d", time.Now().UTC().UnixNano())
	}
	return fmt.Sprintf("rss-%s-%s", time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(suffix[:]))
}
