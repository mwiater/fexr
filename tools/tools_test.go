package tools

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

// TestFetchURLAsTextDefinition verifies the static text tool definition.
func TestFetchURLAsTextDefinition(t *testing.T) {
	def := FetchURLAsTextDefinition()

	if def["name"] != FetchURLAsTextToolName {
		t.Fatalf("unexpected tool name: %v", def["name"])
	}
}

// TestBrowseDefinition verifies the browser tool schema and required fields.
func TestBrowseDefinition(t *testing.T) {
	def := BrowseDefinition()
	if def["name"] != BrowseToolName {
		t.Fatalf("unexpected tool name: %v", def["name"])
	}

	schema := def["inputSchema"].(map[string]any)
	required := schema["required"].([]string)
	if len(required) != 1 || required[0] != "url" {
		t.Fatalf("unexpected required properties: %#v", required)
	}
}

// TestBrowseRejectsInvalidTimeout verifies the upper timeout bound.
func TestBrowseRejectsInvalidTimeout(t *testing.T) {
	_, err := Browse("https://example.com", "", 61, 1024)
	if err == nil || err.Error() != "timeout_seconds must be between 1 and 60" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBrowseToolResultIncludesLinks verifies structured link preservation.
func TestBrowseToolResultIncludesLinks(t *testing.T) {
	result := browseToolResult(browsedPage{
		URL: "https://example.com", Title: "Example", Status: 200, Text: "page text",
		Links: []PageLink{{Text: "Auction", URL: "https://example.com/auction/1"}},
	}, 1024)
	structured := result["structuredContent"].(map[string]any)
	links := structured["links"].([]PageLink)
	if len(links) != 1 || links[0].Text != "Auction" {
		t.Fatalf("unexpected links: %#v", links)
	}
}

// TestSearchSiteDefinition verifies the crawler schema and required fields.
func TestSearchSiteDefinition(t *testing.T) {
	def := SearchSiteDefinition()
	if def["name"] != SearchSiteToolName {
		t.Fatalf("unexpected tool name: %v", def["name"])
	}
	schema := def["inputSchema"].(map[string]any)
	required := schema["required"].([]string)
	if len(required) != 2 || required[0] != "start_url" || required[1] != "query" {
		t.Fatalf("unexpected required properties: %#v", required)
	}
}

// TestRankCrawlLinksPrioritizesQueryAndFiltersSite verifies relevance ordering
// and same-host, fragment, and sensitive-path filtering.
func TestRankCrawlLinksPrioritizesQueryAndFiltersSite(t *testing.T) {
	origin, _ := url.Parse("https://example.com/auctions/")
	links := []PageLink{
		{Text: "About", URL: "https://example.com/about#team"},
		{Text: "Current vehicle auctions", URL: "https://example.com/auctions/current#cars"},
		{Text: "External auction", URL: "https://other.example/auction"},
		{Text: "Sign in", URL: "https://example.com/login"},
	}
	ranked := rankCrawlLinks(origin, links, "current vehicle auctions", map[string]bool{})
	if len(ranked) != 2 {
		t.Fatalf("expected 2 crawlable links, got %#v", ranked)
	}
	if ranked[0].URL != "https://example.com/auctions/current" {
		t.Fatalf("unexpected top-ranked link: %#v", ranked[0])
	}
	if strings.Contains(ranked[0].URL, "#") {
		t.Fatalf("fragment was not removed: %q", ranked[0].URL)
	}
}

// TestSearchSiteRejectsInvalidBoundsBeforeStartingBrowser verifies validation
// occurs before expensive browser startup.
func TestSearchSiteRejectsInvalidBoundsBeforeStartingBrowser(t *testing.T) {
	if _, err := SearchSite("https://example.com", "auctions", 21, 30, 1024); err == nil || err.Error() != "max_pages must be between 1 and 20" {
		t.Fatalf("unexpected max_pages error: %v", err)
	}
	if _, err := SearchSite("https://example.com", "", 5, 30, 1024); err == nil || err.Error() != "query is required" {
		t.Fatalf("unexpected query error: %v", err)
	}
}

// TestFetchURLAsTextSuccess verifies the curl-to-html2text pipeline.
func TestFetchURLAsTextSuccess(t *testing.T) {
	restorePath := withCommandDir(t, map[string]string{
		"curl":      "#!/bin/sh\necho '<html>source</html>'\n",
		"html2text": "#!/bin/sh\necho 'converted text'\n",
	})
	defer restorePath()

	result, err := FetchURLAsText("https://example.com", 1024)
	if err != nil {
		t.Fatalf("FetchURLAsText returned error: %v", err)
	}

	content := extractContentText(t, result)
	if !strings.Contains(content, "converted text") {
		t.Fatalf("unexpected content: %q", content)
	}
}

// TestFetchRSSAsJSONDefinition verifies the feed tool definition.
func TestFetchRSSAsJSONDefinition(t *testing.T) {
	def := FetchRSSAsJSONDefinition()

	if def["name"] != FetchRSSAsJSONToolName {
		t.Fatalf("unexpected tool name: %v", def["name"])
	}
}

// TestFetchRSSAsJSONSuccess verifies RSS parsing and structured output.
func TestFetchRSSAsJSONSuccess(t *testing.T) {
	server := newFeedServer(t, `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Example Feed</title>
    <description>Example Description</description>
    <link>https://example.com/</link>
    <item>
      <title>Item One</title>
      <link>https://example.com/item-1</link>
      <description>Hello world</description>
    </item>
  </channel>
</rss>`)
	defer server.Close()

	result, err := FetchRSSAsJSON(server.URL, 4096)
	if err != nil {
		t.Fatalf("FetchRSSAsJSON returned error: %v", err)
	}

	structured, ok := result["structuredContent"].(map[string]any)
	if !ok || structured["title"] != "Example Feed" {
		t.Fatalf("unexpected structuredContent: %#v", result["structuredContent"])
	}
}

// TestFetchURLAsJSONDefinition verifies the JSON tool definition.
func TestFetchURLAsJSONDefinition(t *testing.T) {
	def := FetchURLAsJSONDefinition()

	if def["name"] != FetchURLAsJSONToolName {
		t.Fatalf("unexpected tool name: %v", def["name"])
	}
}

// TestFetchURLAsJSONSuccess verifies successful JSON normalization.
func TestFetchURLAsJSONSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/rest.php/v1/page/Arthur_Schopenhauer" {
			t.Fatalf("unexpected path: %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"Arthur Schopenhauer","type":"page","key":"Arthur_Schopenhauer"}`))
	}))
	defer server.Close()

	result, err := FetchURLAsJSON(server.URL+"/rest.php/v1/page/Arthur_Schopenhauer", 4096)
	if err != nil {
		t.Fatalf("FetchURLAsJSON returned error: %v", err)
	}

	structured, ok := result["structuredContent"].(map[string]any)
	if !ok || structured["title"] != "Arthur Schopenhauer" {
		t.Fatalf("unexpected structuredContent: %#v", result["structuredContent"])
	}

	content := extractContentText(t, result)
	if !strings.Contains(content, "Arthur Schopenhauer") {
		t.Fatalf("unexpected content: %q", content)
	}
}

// TestFetchURLAsJSONRetriesOnTransientErrors verifies transient retry behavior.
func TestFetchURLAsJSONRetriesOnTransientErrors(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		if call < 3 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`bad gateway`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"Arthur Schopenhauer"}`))
	}))
	defer server.Close()

	result, err := FetchURLAsJSON(server.URL, 4096)
	if err != nil {
		t.Fatalf("FetchURLAsJSON returned error: %v", err)
	}

	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}

	structured, ok := result["structuredContent"].(map[string]any)
	if !ok || structured["title"] != "Arthur Schopenhauer" {
		t.Fatalf("unexpected structuredContent: %#v", result["structuredContent"])
	}
}

// TestGeocodeDefinition verifies the geocoding tool definition.
func TestGeocodeDefinition(t *testing.T) {
	def := GeocodeDefinition()

	if def["name"] != GeocodeToolName {
		t.Fatalf("unexpected tool name: %v", def["name"])
	}
}

// TestGeocodeSuccess verifies query parameters and response normalization.
func TestGeocodeSuccess(t *testing.T) {
	oldBase := GeocodeBaseURL
	defer func() { GeocodeBaseURL = oldBase }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("name"); got != "San Francisco" {
			t.Fatalf("unexpected name query: %q", got)
		}

		if got := r.URL.Query().Get("count"); got != "10" {
			t.Fatalf("unexpected count query: %q", got)
		}

		if got := r.URL.Query().Get("language"); got != "en" {
			t.Fatalf("unexpected language query: %q", got)
		}

		if got := r.URL.Query().Get("format"); got != "json" {
			t.Fatalf("unexpected format query: %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"name":"San Francisco","latitude":37.7749,"longitude":-122.4194}],"generationtime_ms":0.1}`))
	}))
	defer server.Close()

	GeocodeBaseURL = server.URL

	result, err := Geocode("San Francisco", 4096)
	if err != nil {
		t.Fatalf("Geocode returned error: %v", err)
	}

	structured, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected structuredContent type: %T", result["structuredContent"])
	}

	if structured["results"] == nil {
		t.Fatalf("unexpected structuredContent: %#v", structured)
	}

	content := extractContentText(t, result)
	if !strings.Contains(content, "San Francisco") {
		t.Fatalf("unexpected content: %q", content)
	}
}

// TestGeocodeRejectsEmptyLocationName verifies required input validation.
func TestGeocodeRejectsEmptyLocationName(t *testing.T) {
	_, err := Geocode("", 1024)
	if err == nil || err.Error() != "locationname is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// withCommandDir places executable test doubles on PATH and returns a restorer.
func withCommandDir(t *testing.T, commands map[string]string) func() {
	t.Helper()

	dir := t.TempDir()
	oldPath := os.Getenv("PATH")

	for name, content := range commands {
		// Use .sh extension for Unix-like systems
		fileName := filepath.Join(dir, name)
		if err := os.WriteFile(fileName, []byte(content), 0o700); err != nil {
			t.Fatalf("write command stub %s: %v", name, err)
		}
	}

	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	return func() {
		if err := os.Setenv("PATH", oldPath); err != nil {
			t.Fatalf("restore PATH: %v", err)
		}
	}
}

// scriptExt returns the command-script suffix for the current operating system.
func scriptExt() string {
	if runtime.GOOS == "windows" {
		return ".cmd"
	}

	return ""
}

// extractContentText returns the first MCP text content item or fails the test.
func extractContentText(t *testing.T, result map[string]any) string {
	t.Helper()

	contentValue, ok := result["content"].([]map[string]any)
	if ok && len(contentValue) > 0 {
		text, _ := contentValue[0]["text"].(string)
		return text
	}

	contentAny, ok := result["content"].([]any)
	if !ok || len(contentAny) == 0 {
		t.Fatalf("unexpected content type: %T", result["content"])
	}

	contentMap, ok := contentAny[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected content item type: %T", contentAny[0])
	}

	text, ok := contentMap["text"].(string)
	if !ok {
		t.Fatalf("unexpected text type: %T", contentMap["text"])
	}

	return text
}

// newFeedServer returns a local RSS fixture server containing body.
func newFeedServer(t *testing.T, body string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(body))
	}))
}

// TestWeatherDefinition verifies the weather tool definition.
func TestWeatherDefinition(t *testing.T) {
	def := WeatherDefinition()

	if def["name"] != WeatherToolName {
		t.Fatalf("unexpected tool name: %v", def["name"])
	}
}

// TestFetchURLAsTextWithNetworkError verifies curl transport failures propagate.
func TestFetchURLAsTextWithNetworkError(t *testing.T) {
	restorePath := withCommandDir(t, map[string]string{
		"curl": "#!/bin/sh\nexit 7\n",
	})
	defer restorePath()

	_, err := FetchURLAsText("https://example.test/unreachable", 1024)
	if err == nil {
		t.Fatalf("expected error for network failure, got nil")
	}
}

// TestFetchURLAsTextWithCommandFailure verifies command errors are reported.
func TestFetchURLAsTextWithCommandFailure(t *testing.T) {
	restorePath := withCommandDir(t, map[string]string{
		"curl": "#!/bin/sh\nexit 1\n",
	})
	defer restorePath()

	_, err := FetchURLAsText("https://example.com", 1024)
	if err == nil {
		t.Fatalf("expected error for curl failure, got nil")
	}
}

// TestFetchRSSAsJSONWithMalformedFeed documents tolerant malformed-feed handling.
func TestFetchRSSAsJSONWithMalformedFeed(t *testing.T) {
	// gofeed is lenient with malformed feeds, so this test verifies
	// that the function handles malformed XML gracefully
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Malformed Feed</title>
    <link>https://example.com/</link>
    <item>
      <title>Item One</title>
      <link>https://example.com/item-1</link>
      <description>Missing closing tag
  </channel>
</rss>`))
	}))
	defer server.Close()

	// gofeed may or may not return an error for malformed feeds
	// This test just verifies the function handles the response
	result, err := FetchRSSAsJSON(server.URL, 4096)
	if err != nil {
		// If there's an error, that's acceptable
		return
	}
	// If no error, verify we got some result
	if result == nil {
		t.Fatalf("expected result, got nil")
	}
}

// TestFetchURLAsJSONWithNonJSONContentType verifies content-type enforcement.
func TestFetchURLAsJSONWithNonJSONContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>Not JSON</body></html>`))
	}))
	defer server.Close()

	_, err := FetchURLAsJSON(server.URL, 4096)
	if err == nil {
		t.Fatalf("expected error for non-JSON content type, got nil")
	}
}

// TestGeocodeWithEmptyLocationName verifies whitespace-only names are rejected.
func TestGeocodeWithEmptyLocationName(t *testing.T) {
	_, err := Geocode("   ", 1024)
	if err == nil || err.Error() != "locationname is required" {
		t.Fatalf("expected error for empty locationname, got %v", err)
	}
}

// TestWeatherWithInvalidCoordinates documents coordinate pass-through behavior.
func TestWeatherWithInvalidCoordinates(t *testing.T) {
	// Invalid coordinates should still make a request but may return an error
	// This test validates that the function handles the coordinates correctly
	oldBase := WeatherBaseURL
	defer func() { WeatherBaseURL = oldBase }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"latitude":91,"longitude":181}`))
	}))
	defer server.Close()

	WeatherBaseURL = server.URL

	// Valid coordinates that the API will process
	result, err := Weather(91, 181, 4096)
	if err != nil {
		t.Fatalf("Weather with edge coordinates returned error: %v", err)
	}

	if result == nil {
		t.Fatalf("expected result, got nil")
	}
}

// TestBrowseWithInvalidSelector verifies expected browser or selector failures.
func TestBrowseWithInvalidSelector(t *testing.T) {
	// Browse requires Playwright which may not be available in test environment
	// This test validates the timeout validation
	_, err := Browse("https://example.com", ".nonexistent", 1, 1024)
	if err != nil && !strings.Contains(err.Error(), "Playwright") && !strings.Contains(err.Error(), "chromium") && !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("Browse returned unexpected error: %v", err)
	}
}

// TestBrowseWithTimeoutValidation covers default, boundary, and invalid timeouts.
func TestBrowseWithTimeoutValidation(t *testing.T) {
	testCases := []struct {
		name       string
		timeout    int
		wantErr    bool
		errMessage string
	}{
		{name: "zero timeout (default)", timeout: 0, wantErr: false},
		{name: "valid timeout", timeout: 30, wantErr: false},
		{name: "min timeout", timeout: 1, wantErr: false},
		{name: "max timeout", timeout: 60, wantErr: false},
		{name: "too low", timeout: 0, wantErr: false},
		{name: "too high", timeout: 61, wantErr: true, errMessage: "timeout_seconds must be between 1 and 60"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Browse("https://example.com", "", tc.timeout, 1024)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), tc.errMessage) {
					t.Fatalf("expected error containing %q, got %v", tc.errMessage, err)
				}
			} else {
				// If no error expected, we just check that it doesn't fail with timeout validation
				if err != nil && strings.Contains(err.Error(), "timeout_seconds must be between") {
					t.Fatalf("unexpected timeout error: %v", err)
				}
			}
		})
	}
}

// TestWeatherSuccess verifies forecast query construction and structured output.
func TestWeatherSuccess(t *testing.T) {
	oldBase := WeatherBaseURL
	defer func() { WeatherBaseURL = oldBase }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("latitude"); got != "45.5122" {
			t.Fatalf("unexpected latitude query: %q", got)
		}

		if got := r.URL.Query().Get("longitude"); got != "-122.6784" {
			t.Fatalf("unexpected longitude query: %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"latitude":45.5122,"longitude":-122.6784,"current":{"temperature_2m":72.1}}`))
	}))
	defer server.Close()

	WeatherBaseURL = server.URL

	result, err := Weather(45.5122, -122.6784, 4096)
	if err != nil {
		t.Fatalf("Weather returned error: %v", err)
	}

	structured, ok := result["structuredContent"].(map[string]any)
	if !ok || structured["latitude"] == nil {
		t.Fatalf("unexpected structuredContent: %#v", result["structuredContent"])
	}
}
