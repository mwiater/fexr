package tools

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// TestCrawlSiteFollowsRankedLinksWithinBounds verifies bounded, ranked traversal.
func TestCrawlSiteFollowsRankedLinksWithinBounds(t *testing.T) {
	origin, _ := url.Parse("https://example.test/start")
	pages := map[string]browsedPage{
		"https://example.test/start": {
			URL: "https://example.test/start", Title: "Start", Status: 200, Text: "start page",
			Links: []PageLink{
				{Text: "About", URL: "https://example.test/about"},
				{Text: "Reserved domains", URL: "https://example.test/domains/reserved#examples"},
				{Text: "Duplicate", URL: "https://example.test/domains/reserved#other"},
				{Text: "External", URL: "https://elsewhere.test/domains"},
				{Text: "Sign in", URL: "https://example.test/login"},
			},
		},
		"https://example.test/domains/reserved": {
			URL: "https://example.test/domains/reserved", Title: "Reserved", Status: 200, Text: "reserved domains",
			Links: []PageLink{{Text: "Protocol registries", URL: "https://example.test/protocols"}},
		},
		"https://example.test/protocols": {URL: "https://example.test/protocols", Title: "Protocols", Status: 200, Text: "protocol registries"},
		"https://example.test/about":     {URL: "https://example.test/about", Title: "About", Status: 200, Text: "about"},
	}
	var loaded []string
	results := crawlSite(origin, "reserved domains protocol registries", 4, 3000, func(rawURL string) (browsedPage, error) {
		loaded = append(loaded, rawURL)
		page, ok := pages[rawURL]
		if !ok {
			return browsedPage{}, errors.New("missing fixture")
		}
		return page, nil
	})

	want := []string{"https://example.test/start", "https://example.test/domains/reserved", "https://example.test/about", "https://example.test/protocols"}
	if strings.Join(loaded, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected crawl order: %#v", loaded)
	}
	if len(results) != 4 || results[3].Title != "Protocols" {
		t.Fatalf("unexpected results: %#v", results)
	}
}

// TestCrawlSiteContinuesAfterPageFailure verifies best-effort crawl recovery.
func TestCrawlSiteContinuesAfterPageFailure(t *testing.T) {
	origin, _ := url.Parse("https://example.test/")
	results := crawlSite(origin, "docs", 3, 1024, func(rawURL string) (browsedPage, error) {
		switch rawURL {
		case "https://example.test/":
			return browsedPage{URL: rawURL, Title: "Home", Text: "home", Links: []PageLink{
				{Text: "Docs one", URL: "https://example.test/docs-one"},
				{Text: "Docs two", URL: "https://example.test/docs-two"},
			}}, nil
		case "https://example.test/docs-one":
			return browsedPage{}, errors.New("navigation failed")
		default:
			return browsedPage{URL: rawURL, Title: "Docs Two", Text: "working page"}, nil
		}
	})
	if len(results) != 2 || results[1].Title != "Docs Two" {
		t.Fatalf("unexpected results after failure: %#v", results)
	}
}

// TestAppendUniqueLinks verifies stable queue deduplication.
func TestAppendUniqueLinks(t *testing.T) {
	queue := []PageLink{{Text: "A", URL: "https://example.test/a"}}
	got := appendUniqueLinks(queue, []PageLink{
		{Text: "A again", URL: "https://example.test/a"},
		{Text: "B", URL: "https://example.test/b"},
		{Text: "B again", URL: "https://example.test/b"},
	})
	if len(got) != 2 || got[1].URL != "https://example.test/b" {
		t.Fatalf("unexpected queue: %#v", got)
	}
}

// TestSiteSearchToolResultAndTruncation verifies MCP result shaping and limits.
func TestSiteSearchToolResultAndTruncation(t *testing.T) {
	result, err := siteSearchToolResult("https://example.test", "docs", []SiteSearchResult{{Title: "Docs", URL: "https://example.test/docs", Text: strings.Repeat("x", 100)}}, 60)
	if err != nil {
		t.Fatalf("siteSearchToolResult returned error: %v", err)
	}
	structured := result["structuredContent"].(map[string]any)
	if structured["pages_visited"] != 1 || structured["truncated"] != true {
		t.Fatalf("unexpected structured result: %#v", structured)
	}
	if !strings.Contains(extractContentText(t, result), "[output truncated]") {
		t.Fatal("expected truncated text marker")
	}
}

// TestURLAndSiteHelpersEdgeCases covers normalization, schemes, and skipped paths.
func TestURLAndSiteHelpersEdgeCases(t *testing.T) {
	if normalizeCrawlURL(nil) != "" {
		t.Fatal("nil URL should normalize to empty string")
	}
	origin, _ := url.Parse("https://EXAMPLE.test/start")
	httpCandidate, _ := url.Parse("http://example.test/page")
	ftpCandidate, _ := url.Parse("ftp://example.test/file")
	if !sameSite(origin, httpCandidate) || sameSite(origin, ftpCandidate) {
		t.Fatal("unexpected sameSite result")
	}
	for _, rawURL := range []string{"https://example.test/file.pdf", "https://example.test/account/settings", "https://example.test/checkout"} {
		parsed, _ := url.Parse(rawURL)
		if !shouldSkipCrawlURL(parsed) {
			t.Fatalf("expected URL to be skipped: %s", rawURL)
		}
	}
}

// TestFetchURLAsTextTruncates verifies the text tool's output limit marker.
func TestFetchURLAsTextTruncates(t *testing.T) {
	restorePath := withCommandDir(t, map[string]string{
		"curl":      "#!/bin/sh\necho '<html>source</html>'\n",
		"html2text": "#!/bin/sh\nprintf '0123456789'\n",
	})
	defer restorePath()
	result, err := FetchURLAsText("https://example.test", 5)
	if err != nil {
		t.Fatalf("FetchURLAsText returned error: %v", err)
	}
	if got := extractContentText(t, result); got != "01234\n\n[output truncated]" {
		t.Fatalf("unexpected truncated text: %q", got)
	}
}

// TestFetchURLAsJSONRetryExhaustionAndMalformedJSON covers terminal JSON errors.
func TestFetchURLAsJSONRetryExhaustionAndMalformedJSON(t *testing.T) {
	t.Run("retry exhaustion", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()
		_, err := FetchURLAsJSON(server.URL, 1024)
		if err == nil || !strings.Contains(err.Error(), "after retries") || calls.Load() != 3 {
			t.Fatalf("unexpected retry result: calls=%d err=%v", calls.Load(), err)
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"broken":`))
		}))
		defer server.Close()
		_, err := FetchURLAsJSON(server.URL, 1024)
		if err == nil || !strings.Contains(err.Error(), "failed to decode JSON") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// TestGeocodeAndWeatherHTTPAndJSONErrors verifies upstream failure handling.
func TestGeocodeAndWeatherHTTPAndJSONErrors(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status int
		call   func(string) error
	}{
		{name: "geocode status", status: 500, call: func(endpoint string) error {
			old := GeocodeBaseURL
			defer func() { GeocodeBaseURL = old }()
			GeocodeBaseURL = endpoint
			_, err := Geocode("Portland", 1024)
			return err
		}},
		{name: "geocode malformed", body: `{`, call: func(endpoint string) error {
			old := GeocodeBaseURL
			defer func() { GeocodeBaseURL = old }()
			GeocodeBaseURL = endpoint
			_, err := Geocode("Portland", 1024)
			return err
		}},
		{name: "weather status", status: 502, call: func(endpoint string) error {
			old := WeatherBaseURL
			defer func() { WeatherBaseURL = old }()
			WeatherBaseURL = endpoint
			_, err := Weather(1, 2, 1024)
			return err
		}},
		{name: "weather malformed", body: `[]`, call: func(endpoint string) error {
			old := WeatherBaseURL
			defer func() { WeatherBaseURL = old }()
			WeatherBaseURL = endpoint
			_, err := Weather(1, 2, 1024)
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.status != 0 {
					w.WriteHeader(tc.status)
					return
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			if err := tc.call(server.URL); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
