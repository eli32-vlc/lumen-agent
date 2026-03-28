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

func (r *Registry) registerWebInfoTools() {
	r.register(
		"get_weather",
		"Look up current weather and a short forecast for a city or place using a free weather API.",
		objectSchema(map[string]any{
			"location": stringSchema("City or place name to look up."),
			"days":     integerSchema("Optional number of forecast days to return, up to 5.", 1),
		}, "location"),
		r.handleGetWeather,
	)

	r.register(
		"search_web",
		"Search the web using a lightweight free search API and return short summaries plus related links.",
		objectSchema(map[string]any{
			"query": stringSchema("Search query."),
		}, "query"),
		r.handleSearchWeb,
	)

	r.register(
		"search_news",
		"Search recent tech and startup news headlines using the free Hacker News Algolia API.",
		objectSchema(map[string]any{
			"query": stringSchema("News topic or keyword."),
			"limit": integerSchema("Optional number of results to return, up to 10.", 1),
		}, "query"),
		r.handleSearchNews,
	)
}

func (r *Registry) handleGetWeather(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Location string `json:"location"`
		Days     int    `json:"days"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	location := strings.TrimSpace(input.Location)
	if location == "" {
		return "", fmt.Errorf("location must not be empty")
	}

	forecastDays := input.Days
	if forecastDays <= 0 {
		forecastDays = 3
	}
	if forecastDays > 5 {
		forecastDays = 5
	}

	geoURL := "https://geocoding-api.open-meteo.com/v1/search?count=1&language=en&format=json&name=" + url.QueryEscape(location)
	geoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, geoURL, nil)
	if err != nil {
		return "", fmt.Errorf("build weather geocoding request: %w", err)
	}

	geoResp, err := http.DefaultClient.Do(geoReq)
	if err != nil {
		return "", fmt.Errorf("geocode weather location: %w", err)
	}
	defer geoResp.Body.Close()

	if geoResp.StatusCode < 200 || geoResp.StatusCode >= 300 {
		return "", fmt.Errorf("geocode weather location: API error (%d)", geoResp.StatusCode)
	}

	var geocoded struct {
		Results []struct {
			Name      string  `json:"name"`
			Country   string  `json:"country"`
			Admin1    string  `json:"admin1"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Timezone  string  `json:"timezone"`
		} `json:"results"`
	}
	if err := json.NewDecoder(geoResp.Body).Decode(&geocoded); err != nil {
		return "", fmt.Errorf("decode weather geocoding response: %w", err)
	}
	if len(geocoded.Results) == 0 {
		return "", fmt.Errorf("no weather match found for %q", location)
	}

	match := geocoded.Results[0]
	forecastURL := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s&current=temperature_2m,apparent_temperature,weather_code,wind_speed_10m&daily=weather_code,temperature_2m_max,temperature_2m_min&forecast_days=%d&timezone=auto",
		strconv.FormatFloat(match.Latitude, 'f', 4, 64),
		strconv.FormatFloat(match.Longitude, 'f', 4, 64),
		forecastDays,
	)
	forecastReq, err := http.NewRequestWithContext(ctx, http.MethodGet, forecastURL, nil)
	if err != nil {
		return "", fmt.Errorf("build weather forecast request: %w", err)
	}

	forecastResp, err := http.DefaultClient.Do(forecastReq)
	if err != nil {
		return "", fmt.Errorf("fetch weather forecast: %w", err)
	}
	defer forecastResp.Body.Close()

	if forecastResp.StatusCode < 200 || forecastResp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch weather forecast: API error (%d)", forecastResp.StatusCode)
	}

	var forecast struct {
		Current struct {
			Temperature2M       float64 `json:"temperature_2m"`
			ApparentTemperature float64 `json:"apparent_temperature"`
			WeatherCode         int     `json:"weather_code"`
			WindSpeed10M        float64 `json:"wind_speed_10m"`
		} `json:"current"`
		Daily struct {
			Time             []string  `json:"time"`
			WeatherCode      []int     `json:"weather_code"`
			TemperatureMax2M []float64 `json:"temperature_2m_max"`
			TemperatureMin2M []float64 `json:"temperature_2m_min"`
		} `json:"daily"`
	}
	if err := json.NewDecoder(forecastResp.Body).Decode(&forecast); err != nil {
		return "", fmt.Errorf("decode weather forecast response: %w", err)
	}

	days := make([]map[string]any, 0, len(forecast.Daily.Time))
	for i := range forecast.Daily.Time {
		item := map[string]any{
			"date":        forecast.Daily.Time[i],
			"description": weatherCodeDescription(valueAtInt(forecast.Daily.WeatherCode, i)),
			"temp_max_c":  valueAtFloat(forecast.Daily.TemperatureMax2M, i),
			"temp_min_c":  valueAtFloat(forecast.Daily.TemperatureMin2M, i),
		}
		days = append(days, item)
	}

	label := strings.TrimSpace(match.Name)
	if strings.TrimSpace(match.Admin1) != "" {
		label += ", " + strings.TrimSpace(match.Admin1)
	}
	if strings.TrimSpace(match.Country) != "" {
		label += ", " + strings.TrimSpace(match.Country)
	}

	return jsonResult(map[string]any{
		"location": label,
		"timezone": strings.TrimSpace(match.Timezone),
		"current": map[string]any{
			"temperature_c":  forecast.Current.Temperature2M,
			"feels_like_c":   forecast.Current.ApparentTemperature,
			"wind_speed_kmh": forecast.Current.WindSpeed10M,
			"description":    weatherCodeDescription(forecast.Current.WeatherCode),
			"weather_code":   forecast.Current.WeatherCode,
		},
		"forecast": days,
	})
}

func (r *Registry) handleSearchWeb(ctx context.Context, payload json.RawMessage) (string, error) {
	type args struct {
		Query string `json:"query"`
	}
	var input args
	if err := decodeArgs(payload, &input); err != nil {
		return "", err
	}

	query := strings.TrimSpace(input.Query)
	if query == "" {
		return "", fmt.Errorf("query must not be empty")
	}

	searchURL := "https://api.duckduckgo.com/?format=json&no_html=1&no_redirect=1&q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("build web search request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search web: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("search web: API error (%d)", resp.StatusCode)
	}

	var parsed struct {
		Heading       string `json:"Heading"`
		AbstractText  string `json:"AbstractText"`
		AbstractURL   string `json:"AbstractURL"`
		Answer        string `json:"Answer"`
		Definition    string `json:"Definition"`
		RelatedTopics []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
			Topics   []struct {
				Text     string `json:"Text"`
				FirstURL string `json:"FirstURL"`
			} `json:"Topics"`
		} `json:"RelatedTopics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode web search response: %w", err)
	}

	results := make([]map[string]any, 0, 8)
	appendTopic := func(text string, firstURL string) {
		text = strings.TrimSpace(text)
		firstURL = strings.TrimSpace(firstURL)
		if text == "" && firstURL == "" {
			return
		}
		results = append(results, map[string]any{
			"text": text,
			"url":  firstURL,
		})
	}

	for _, topic := range parsed.RelatedTopics {
		appendTopic(topic.Text, topic.FirstURL)
		for _, nested := range topic.Topics {
			appendTopic(nested.Text, nested.FirstURL)
			if len(results) >= 8 {
				break
			}
		}
		if len(results) >= 8 {
			break
		}
	}

	return jsonResult(map[string]any{
		"query": query,
		"summary": map[string]any{
			"heading":      strings.TrimSpace(parsed.Heading),
			"abstract":     strings.TrimSpace(parsed.AbstractText),
			"abstract_url": strings.TrimSpace(parsed.AbstractURL),
			"answer":       strings.TrimSpace(parsed.Answer),
			"definition":   strings.TrimSpace(parsed.Definition),
		},
		"results": results,
	})
}

func (r *Registry) handleSearchNews(ctx context.Context, payload json.RawMessage) (string, error) {
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

	limit := input.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}

	searchURL := "https://hn.algolia.com/api/v1/search_by_date?tags=story&hitsPerPage=" + strconv.Itoa(limit) + "&query=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("build news search request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search news: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("search news: API error (%d)", resp.StatusCode)
	}

	var parsed struct {
		Hits []struct {
			Title     string `json:"title"`
			URL       string `json:"url"`
			Author    string `json:"author"`
			CreatedAt string `json:"created_at"`
			Points    int    `json:"points"`
			StoryText string `json:"story_text"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode news search response: %w", err)
	}

	results := make([]map[string]any, 0, len(parsed.Hits))
	for _, hit := range parsed.Hits {
		results = append(results, map[string]any{
			"title":      strings.TrimSpace(hit.Title),
			"url":        strings.TrimSpace(hit.URL),
			"author":     strings.TrimSpace(hit.Author),
			"created_at": strings.TrimSpace(hit.CreatedAt),
			"points":     hit.Points,
			"summary":    strings.TrimSpace(hit.StoryText),
		})
	}

	return jsonResult(map[string]any{
		"query":   query,
		"results": results,
	})
}

func valueAtFloat(values []float64, index int) float64 {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}

func valueAtInt(values []int, index int) int {
	if index < 0 || index >= len(values) {
		return 0
	}
	return values[index]
}

func weatherCodeDescription(code int) string {
	switch code {
	case 0:
		return "clear sky"
	case 1, 2, 3:
		return "partly cloudy"
	case 45, 48:
		return "fog"
	case 51, 53, 55:
		return "drizzle"
	case 56, 57:
		return "freezing drizzle"
	case 61, 63, 65:
		return "rain"
	case 66, 67:
		return "freezing rain"
	case 71, 73, 75, 77:
		return "snow"
	case 80, 81, 82:
		return "rain showers"
	case 85, 86:
		return "snow showers"
	case 95:
		return "thunderstorm"
	case 96, 99:
		return "thunderstorm with hail"
	default:
		return "unknown"
	}
}
