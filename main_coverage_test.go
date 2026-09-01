package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleMCPInitializeAndMethodHandling verifies initialization, preflight,
// and unsupported HTTP method behavior at the MCP endpoint.
func TestHandleMCPInitializeAndMethodHandling(t *testing.T) {
	t.Run("initialize", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handleMCP(rec, httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)))
		var response map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		result := response["result"].(map[string]any)
		if result["protocolVersion"] != "2025-03-26" {
			t.Fatalf("unexpected result: %#v", result)
		}
	})

	t.Run("options", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handleMCP(rec, httptest.NewRequest(http.MethodOptions, mcpPath, nil))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Fatal("missing CORS header")
		}
	})

	t.Run("non-post", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handleMCP(rec, httptest.NewRequest(http.MethodGet, mcpPath, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})
}

// TestHandleMCPRejectsOversizedBody verifies enforcement of maxBodySize.
func TestHandleMCPRejectsOversizedBody(t *testing.T) {
	rec := httptest.NewRecorder()
	body := strings.NewReader(strings.Repeat("x", maxBodySize+1))
	handleMCP(rec, httptest.NewRequest(http.MethodPost, mcpPath, body))
	var response JSONRPCResponse
	decodeResponse(t, rec, &response)
	if response.Error == nil || response.Error.Code != -32700 || response.Error.Message != "failed to read request body" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

// TestHandleToolCallArgumentValidation covers malformed and incomplete
// arguments for every dispatch branch.
func TestHandleToolCallArgumentValidation(t *testing.T) {
	tests := []struct{ name, payload, want string }{
		{"params", `{`, "invalid tools/call params"},
		{"text args", `{"name":"fetch_url_as_text","arguments":[]}`, "invalid fetch_url_as_text arguments"},
		{"rss args", `{"name":"fetch_rss_as_JSON","arguments":[]}`, "invalid fetch_rss_as_JSON arguments"},
		{"json args", `{"name":"fetch_url_as_JSON","arguments":[]}`, "invalid fetch_url_as_JSON arguments"},
		{"geocode args", `{"name":"geocode","arguments":[]}`, "invalid geocode arguments"},
		{"weather args", `{"name":"weather","arguments":[]}`, "invalid weather arguments"},
		{"weather missing", `{"name":"weather","arguments":{"latitude":1}}`, "latitude and longitude are required for weather"},
		{"browse args", `{"name":"browse","arguments":[]}`, "invalid browse arguments"},
		{"search args", `{"name":"search_site","arguments":[]}`, "invalid search_site arguments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := handleToolCall(json.RawMessage(tc.payload))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

// TestToolDefinitionsContract verifies tool-name uniqueness and read-only hints.
func TestToolDefinitionsContract(t *testing.T) {
	rec := httptest.NewRecorder()
	handleMCP(rec, httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	definitions := response["result"].(map[string]any)["tools"].([]any)
	if len(definitions) != 7 {
		t.Fatalf("expected 7 tools, got %d", len(definitions))
	}
	seen := map[string]bool{}
	for _, value := range definitions {
		definition := value.(map[string]any)
		name := definition["name"].(string)
		if seen[name] {
			t.Fatalf("duplicate tool name %q", name)
		}
		seen[name] = true
		annotations := definition["annotations"].(map[string]any)
		if annotations["readOnlyHint"] != true {
			t.Fatalf("tool %q is not read-only", name)
		}
	}
}
