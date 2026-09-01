package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mwiater/fexr/tools"
)

// TestHandleRoot verifies the root health response and content type.
func TestHandleRoot(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handleRoot(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if got := rec.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", got)
	}

	if got := rec.Body.String(); got != "fexr is running. MCP endpoint: /mcp\n" {
		t.Fatalf("unexpected body: %q", got)
	}
}

// TestHandleMCPToolsList verifies that tools/list advertises every current tool.
func TestHandleMCPToolsList(t *testing.T) {
	req := jsonRPCRequest(t, JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})
	rec := httptest.NewRecorder()

	handleMCP(rec, req)

	var resp JSONRPCResponse
	decodeResponse(t, rec, &resp)

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}

	toolsValue, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("unexpected tools type: %T", result["tools"])
	}

	if len(toolsValue) != 7 {
		t.Fatalf("expected 7 tools, got %d", len(toolsValue))
	}
}

// TestHandleMCPToolsCallFetchURLAsText exercises text fetching through JSON-RPC.
func TestHandleMCPToolsCallFetchURLAsText(t *testing.T) {
	restorePath := withStubCommands(t, map[string]string{
		"curl":      "#!/bin/sh\necho '<html>source</html>'\n",
		"html2text": "#!/bin/sh\necho 'converted text'\n",
	})
	defer restorePath()

	req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(`{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "fetch_url_as_text",
    "arguments": {
      "url": "https://example.com"
    }
  }
}`))
	rec := httptest.NewRecorder()

	handleMCP(rec, req)

	var resp JSONRPCResponse
	decodeResponse(t, rec, &resp)

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}

	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("unexpected content: %#v", result["content"])
	}

	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "converted text") {
		t.Fatalf("unexpected text: %q", text)
	}
}

// TestHandleMCPToolsCallFetchRSSAsJSON exercises feed parsing through JSON-RPC.
func TestHandleMCPToolsCallFetchRSSAsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Main Test Feed</title>
    <description>Main Test Description</description>
    <link>https://example.com/</link>
  </channel>
</rss>`))
	}))
	defer server.Close()

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "fetch_rss_as_JSON",
			"arguments": map[string]any{
				"url": server.URL,
			},
		},
	}
	encoded, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, mcpPath, bytes.NewReader(encoded))
	rec := httptest.NewRecorder()

	handleMCP(rec, req)

	var resp JSONRPCResponse
	decodeResponse(t, rec, &resp)

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}

	structured, ok := result["structuredContent"].(map[string]any)
	if !ok || structured["title"] != "Main Test Feed" {
		t.Fatalf("unexpected structuredContent: %#v", result["structuredContent"])
	}
}

// TestHandleMCPToolsCallFetchURLAsJSON exercises JSON retrieval through JSON-RPC.
func TestHandleMCPToolsCallFetchURLAsJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/rest.php/v1/page/Arthur_Schopenhauer" {
			t.Fatalf("unexpected path: %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"title":"Arthur Schopenhauer","type":"page","key":"Arthur_Schopenhauer"}`))
	}))
	defer server.Close()

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "fetch_url_as_JSON",
			"arguments": map[string]any{
				"url": server.URL + "/rest.php/v1/page/Arthur_Schopenhauer",
			},
		},
	}
	encoded, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, mcpPath, bytes.NewReader(encoded))
	rec := httptest.NewRecorder()

	handleMCP(rec, req)

	var resp JSONRPCResponse
	decodeResponse(t, rec, &resp)

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}

	structured, ok := result["structuredContent"].(map[string]any)
	if !ok || structured["title"] != "Arthur Schopenhauer" {
		t.Fatalf("unexpected structuredContent: %#v", result["structuredContent"])
	}
}

// TestHandleMCPToolsCallGeocode exercises geocoding through JSON-RPC.
func TestHandleMCPToolsCallGeocode(t *testing.T) {
	restoreBaseURL := withGeocodeBaseURL(t)
	defer restoreBaseURL()

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

	tools.GeocodeBaseURL = server.URL

	req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(`{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "tools/call",
  "params": {
    "name": "geocode",
    "arguments": {
      "locationname": "San Francisco"
    }
  }
}`))
	rec := httptest.NewRecorder()

	handleMCP(rec, req)

	var resp JSONRPCResponse
	decodeResponse(t, rec, &resp)

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}

	structured, ok := result["structuredContent"].(map[string]any)
	if !ok || structured["results"] == nil {
		t.Fatalf("unexpected structuredContent: %#v", result["structuredContent"])
	}
}

// TestHandleToolCallRejectsUnknownTool verifies unknown tool names are rejected.
func TestHandleToolCallRejectsUnknownTool(t *testing.T) {
	_, err := handleToolCall(json.RawMessage(`{"name":"bogus","arguments":{}}`))
	if err == nil || err.Error() != "unknown tool: bogus" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestHandleToolCallRejectsInvalidURL verifies URL validation during dispatch.
func TestHandleToolCallRejectsInvalidURL(t *testing.T) {
	_, err := handleToolCall(json.RawMessage(`{"name":"fetch_url_as_text","arguments":{"url":""}}`))
	if err == nil || err.Error() != "url is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestHandleToolCallRejectsMissingLocationName verifies geocode requirements.
func TestHandleToolCallRejectsMissingLocationName(t *testing.T) {
	_, err := handleToolCall(json.RawMessage(`{"name":"geocode","arguments":{"locationname":""}}`))
	if err == nil || err.Error() != "locationname is required for geocode" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestHandleToolCallRejectsInvalidSearchSiteURL verifies crawler URL schemes.
func TestHandleToolCallRejectsInvalidSearchSiteURL(t *testing.T) {
	_, err := handleToolCall(json.RawMessage(`{"name":"search_site","arguments":{"start_url":"file:///tmp/site","query":"auctions"}}`))
	if err == nil || err.Error() != "invalid start_url: only http and https URLs are allowed" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestValidateURL covers accepted and rejected URL forms.
func TestValidateURL(t *testing.T) {
	testCases := []struct {
		name    string
		rawURL  string
		wantErr string
	}{
		{name: "empty", rawURL: "", wantErr: "url is required"},
		{name: "invalid", rawURL: "://bad", wantErr: "invalid url"},
		{name: "unsupported scheme", rawURL: "file:///tmp/test", wantErr: "only http and https URLs are allowed"},
		{name: "missing host", rawURL: "https://", wantErr: "url must include a host"},
		{name: "valid http", rawURL: "http://example.com"},
		{name: "valid https", rawURL: "https://example.com/path"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateURL(tc.rawURL)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("expected error %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// jsonRPCRequest serializes req into an HTTP request for handler tests.
func jsonRPCRequest(t *testing.T, req JSONRPCRequest) *http.Request {
	t.Helper()

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	return httptest.NewRequest(http.MethodPost, mcpPath, bytes.NewReader(body))
}

// decodeResponse checks the response content type and decodes its JSON body.
func decodeResponse(t *testing.T, rec *httptest.ResponseRecorder, dst *JSONRPCResponse) {
	t.Helper()

	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", got)
	}

	if err := json.Unmarshal(rec.Body.Bytes(), dst); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// withStubCommands installs temporary executable fixtures on PATH and returns
// a cleanup function that restores the original environment.
func withStubCommands(t *testing.T, commands map[string]string) func() {
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

// withGeocodeBaseURL returns a cleanup function that restores GeocodeBaseURL.
func withGeocodeBaseURL(t *testing.T) func() {
	t.Helper()

	old := tools.GeocodeBaseURL
	return func() {
		tools.GeocodeBaseURL = old
	}
}

// TestHandleRootWithDifferentMethods documents the method-agnostic root route.
func TestHandleRootWithDifferentMethods(t *testing.T) {
	// handleRoot doesn't check method - it always returns 200
	// This test verifies the function's behavior regardless of method
	testCases := []struct {
		name       string
		method     string
		wantStatus int
	}{
		{name: "GET", method: http.MethodGet, wantStatus: http.StatusOK},
		{name: "POST", method: http.MethodPost, wantStatus: http.StatusOK},
		{name: "PUT", method: http.MethodPut, wantStatus: http.StatusOK},
		{name: "DELETE", method: http.MethodDelete, wantStatus: http.StatusOK},
		{name: "OPTIONS", method: http.MethodOptions, wantStatus: http.StatusOK},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/", nil)
			rec := httptest.NewRecorder()

			handleRoot(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, rec.Code)
			}

			// Verify CORS headers are set
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
				t.Fatalf("expected CORS header, got %q", got)
			}
		})
	}
}

// TestHandleMCPWithInvalidJSONRPC verifies protocol-level parsing errors.
func TestHandleMCPWithInvalidJSONRPC(t *testing.T) {
	testCases := []struct {
		name     string
		body     string
		wantCode int
		wantMsg  string
	}{
		{
			name:     "invalid JSON",
			body:     `{invalid json`,
			wantCode: -32700,
			wantMsg:  "invalid JSON-RPC request",
		},
		{
			name:     "wrong version",
			body:     `{"jsonrpc":"1.0","id":1,"method":"tools/list"}`,
			wantCode: -32600,
			wantMsg:  "invalid JSON-RPC version",
		},
		{
			name:     "missing method",
			body:     `{"jsonrpc":"2.0","id":1}`,
			wantCode: -32601,
			wantMsg:  "method not found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			handleMCP(rec, req)

			var resp JSONRPCResponse
			decodeResponse(t, rec, &resp)

			if resp.Error == nil {
				t.Fatalf("expected error response, got nil")
			}

			if resp.Error.Code != tc.wantCode {
				t.Fatalf("expected error code %d, got %d", tc.wantCode, resp.Error.Code)
			}

			if resp.Error.Message != tc.wantMsg {
				t.Fatalf("expected error message %q, got %q", tc.wantMsg, resp.Error.Message)
			}
		})
	}
}

// TestHandleMCPWithNotifications verifies one-way requests return Accepted.
func TestHandleMCPWithNotifications(t *testing.T) {
	testCases := []struct {
		name string
		body string
	}{
		{
			name: "notification with no ID",
			body: `{"jsonrpc":"2.0","method":"tools/list"}`,
		},
		{
			name: "tools/call notification",
			body: `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"fetch_url_as_text","arguments":{"url":"https://example.com"}}}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()

			handleMCP(rec, req)

			if rec.Code != http.StatusAccepted {
				t.Fatalf("expected status %d for notification, got %d", http.StatusAccepted, rec.Code)
			}
		})
	}
}

// TestIsNotification covers request ID and method combinations.
func TestIsNotification(t *testing.T) {
	testCases := []struct {
		name       string
		req        JSONRPCRequest
		wantNotify bool
	}{
		{
			name:       "notification with no ID",
			req:        JSONRPCRequest{JSONRPC: "2.0", Method: "tools/list"},
			wantNotify: true,
		},
		{
			name:       "notification with empty ID",
			req:        JSONRPCRequest{JSONRPC: "2.0", ID: nil, Method: "tools/list"},
			wantNotify: true,
		},
		{
			name:       "request with ID",
			req:        JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"},
			wantNotify: false,
		},
		{
			name:       "request with string ID",
			req:        JSONRPCRequest{JSONRPC: "2.0", ID: "request-123", Method: "tools/list"},
			wantNotify: false,
		},
		{
			name:       "no method",
			req:        JSONRPCRequest{JSONRPC: "2.0", ID: 1},
			wantNotify: false,
		},
		{
			name:       "empty method",
			req:        JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: ""},
			wantNotify: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := isNotification(tc.req)
			if got != tc.wantNotify {
				t.Fatalf("expected isNotification=%v, got %v", tc.wantNotify, got)
			}
		})
	}
}

// TestHandleToolCallWithAllTools exercises every tool dispatch case.
func TestHandleToolCallWithAllTools(t *testing.T) {
	// Test fetch_url_as_text
	t.Run("fetch_url_as_text", func(t *testing.T) {
		restorePath := withStubCommands(t, map[string]string{
			"curl":      "#!/bin/sh\necho '<html>test</html>'\n",
			"html2text": "#!/bin/sh\necho 'converted text'\n",
		})
		defer restorePath()

		result, err := handleToolCall(json.RawMessage(`{"name":"fetch_url_as_text","arguments":{"url":"https://example.com"}}`))
		if err != nil {
			t.Fatalf("handleToolCall returned error: %v", err)
		}

		if result == nil {
			t.Fatalf("expected result, got nil")
		}
	})

	// Test fetch_rss_as_JSON
	t.Run("fetch_rss_as_JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <link>https://example.com/</link>
  </channel>
</rss>`))
		}))
		defer server.Close()

		result, err := handleToolCall(json.RawMessage(`{"name":"fetch_rss_as_JSON","arguments":{"url":"` + server.URL + `"}}`))
		if err != nil {
			t.Fatalf("handleToolCall returned error: %v", err)
		}

		if result == nil {
			t.Fatalf("expected result, got nil")
		}
	})

	// Test fetch_url_as_JSON
	t.Run("fetch_url_as_JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"test":"data"}`))
		}))
		defer server.Close()

		result, err := handleToolCall(json.RawMessage(`{"name":"fetch_url_as_JSON","arguments":{"url":"` + server.URL + `"}}`))
		if err != nil {
			t.Fatalf("handleToolCall returned error: %v", err)
		}

		if result == nil {
			t.Fatalf("expected result, got nil")
		}
	})

	// Test geocode
	t.Run("geocode", func(t *testing.T) {
		oldBase := tools.GeocodeBaseURL
		defer func() { tools.GeocodeBaseURL = oldBase }()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":[]}`))
		}))
		defer server.Close()

		tools.GeocodeBaseURL = server.URL

		result, err := handleToolCall(json.RawMessage(`{"name":"geocode","arguments":{"locationname":"New York"}}`))
		if err != nil {
			t.Fatalf("handleToolCall returned error: %v", err)
		}

		if result == nil {
			t.Fatalf("expected result, got nil")
		}
	})

	// Test weather
	t.Run("weather", func(t *testing.T) {
		oldBase := tools.WeatherBaseURL
		defer func() { tools.WeatherBaseURL = oldBase }()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"latitude":0,"longitude":0}`))
		}))
		defer server.Close()

		tools.WeatherBaseURL = server.URL

		result, err := handleToolCall(json.RawMessage(`{"name":"weather","arguments":{"latitude":40.7128,"longitude":-74.0060}}`))
		if err != nil {
			t.Fatalf("handleToolCall returned error: %v", err)
		}

		if result == nil {
			t.Fatalf("expected result, got nil")
		}
	})

	// Test browse
	t.Run("browse", func(t *testing.T) {
		// Browse requires Playwright which may not be available in test environment
		// This test just validates the argument parsing and validation
		_, err := handleToolCall(json.RawMessage(`{"name":"browse","arguments":{"url":"https://example.com"}}`))
		// We expect this to fail if Playwright is not installed, which is fine
		if err != nil && !strings.Contains(err.Error(), "Playwright") && !strings.Contains(err.Error(), "chromium") {
			t.Fatalf("handleToolCall returned unexpected error: %v", err)
		}
	})
}

// TestWriteRPCResult verifies successful response serialization.
func TestWriteRPCResult(t *testing.T) {
	rec := httptest.NewRecorder()

	writeRPCResult(rec, 123, map[string]any{"test": "result"})

	var resp JSONRPCResponse
	decodeResponse(t, rec, &resp)

	// ID is unmarshaled as float64 from JSON
	var id int64
	switch v := resp.ID.(type) {
	case int:
		id = int64(v)
	case int64:
		id = v
	case float64:
		id = int64(v)
	default:
		t.Fatalf("unexpected ID type: %T", resp.ID)
	}

	if id != 123 {
		t.Fatalf("expected ID 123, got %v", id)
	}

	if resp.Error != nil {
		t.Fatalf("expected no error, got %v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok || result["test"] != "result" {
		t.Fatalf("unexpected result: %#v", resp.Result)
	}
}

// TestWriteRPCError verifies error response serialization.
func TestWriteRPCError(t *testing.T) {
	rec := httptest.NewRecorder()

	writeRPCError(rec, 456, -32000, "test error message")

	var resp JSONRPCResponse
	decodeResponse(t, rec, &resp)

	// ID is unmarshaled as float64 from JSON
	var id int64
	switch v := resp.ID.(type) {
	case int:
		id = int64(v)
	case int64:
		id = v
	case float64:
		id = int64(v)
	default:
		t.Fatalf("unexpected ID type: %T", resp.ID)
	}

	if id != 456 {
		t.Fatalf("expected ID 456, got %v", id)
	}

	if resp.Error == nil {
		t.Fatalf("expected error, got nil")
	}

	if resp.Error.Code != -32000 {
		t.Fatalf("expected error code -32000, got %d", resp.Error.Code)
	}

	if resp.Error.Message != "test error message" {
		t.Fatalf("expected error message %q, got %q", "test error message", resp.Error.Message)
	}
}

// TestHandleMCPToolsCallWeather verifies weather query construction via JSON-RPC.
func TestHandleMCPToolsCallWeather(t *testing.T) {
	oldBase := tools.WeatherBaseURL
	defer func() { tools.WeatherBaseURL = oldBase }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("latitude"); got != "45.5122" {
			t.Fatalf("unexpected latitude query: %q", got)
		}

		if got := r.URL.Query().Get("longitude"); got != "-122.6784" {
			t.Fatalf("unexpected longitude query: %q", got)
		}

		if got := r.URL.Query().Get("current"); got != "temperature_2m,relative_humidity_2m,wind_speed_10m,weather_code" {
			t.Fatalf("unexpected current query: %q", got)
		}

		if got := r.URL.Query().Get("daily"); got != "weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max" {
			t.Fatalf("unexpected daily query: %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"latitude":45.5122,"longitude":-122.6784,"current":{"temperature_2m":72.1}}`))
	}))
	defer server.Close()

	tools.WeatherBaseURL = server.URL

	req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(`{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "tools/call",
  "params": {
    "name": "weather",
    "arguments": {
      "latitude": 45.5122,
      "longitude": -122.6784
    }
  }
}`))
	rec := httptest.NewRecorder()

	handleMCP(rec, req)

	var resp JSONRPCResponse
	decodeResponse(t, rec, &resp)

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}

	structured, ok := result["structuredContent"].(map[string]any)
	if !ok || structured["latitude"] == nil {
		t.Fatalf("unexpected structuredContent: %#v", result["structuredContent"])
	}
}
