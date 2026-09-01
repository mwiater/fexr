// Package main implements the fexr HTTP server and its JSON-RPC 2.0 MCP
// endpoint. It exposes read-only tools for static and rendered web content,
// feeds, JSON APIs, geocoding, weather, and bounded site discovery.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/mwiater/fexr/tools"
)

// addr is the network address the server listens on for incoming connections.
const addr = ":4002"

// mcpPath is the URL path for the MCP endpoint where JSON-RPC requests are processed.
const mcpPath = "/mcp"

// serverName is the identifier for this MCP server, used in serverInfo responses.
const serverName = "fexr"

// maxBodySize is the maximum size in bytes allowed for incoming request bodies.
const maxBodySize = 1024 * 1024 * 4

// maxOutput is the maximum size in bytes for tool output responses.
const maxOutput = 1024 * 200

// JSONRPCRequest represents a JSON-RPC 2.0 request structure.
// It contains the protocol version, optional ID, method name, and parameters.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response structure.
// It contains the protocol version, optional ID, result, and error fields.
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents an error response in JSON-RPC 2.0 format.
// It contains an error code and a descriptive message.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolCallParams represents the parameters for a tools/call request.
// It specifies which tool to invoke and its arguments.
type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// FetchURLArgs represents the arguments for the fetch_url_as_text tool.
type FetchURLArgs struct {
	URL string `json:"url"`
}

// GeocodeArgs represents the arguments for the geocode tool.
type GeocodeArgs struct {
	LocationName string `json:"locationname"`
}

// ParseRSSFeedArgs represents the arguments for the fetch_rss_as_JSON tool.
type ParseRSSFeedArgs struct {
	URL string `json:"url"`
}

// WeatherArgs represents the arguments for the weather tool.
type WeatherArgs struct {
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
}

// BrowseArgs represents the arguments for the browse tool.
type BrowseArgs struct {
	URL             string `json:"url"`
	WaitForSelector string `json:"wait_for_selector"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
}

// SearchSiteArgs represents bounded crawler arguments supplied to search_site.
type SearchSiteArgs struct {
	StartURL       string `json:"start_url"`
	Query          string `json:"query"`
	MaxPages       int    `json:"max_pages"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// main is the application entry point. It sets up the HTTP server with routes
// for the root endpoint and MCP endpoint, then starts listening for connections.
func main() {
	mux := http.NewServeMux()
	mux.HandleFunc(mcpPath, handleMCP)
	mux.HandleFunc("/", handleRoot)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("fexr listening on http://127.0.0.1%s%s", addr, mcpPath)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// handleRoot handles requests to the root endpoint, returning a simple status message.
// It sets CORS headers and responds with plain text indicating the server is running
// and where the MCP endpoint is located.
func handleRoot(w http.ResponseWriter, r *http.Request) {
	writeCORS(w)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("fexr is running. MCP endpoint: /mcp\n"))
}

// handleMCP handles all JSON-RPC 2.0 requests to the MCP endpoint.
// It processes requests for initialize, tools/list, and tools/call methods,
// returning appropriate responses or errors.
func handleMCP(w http.ResponseWriter, r *http.Request) {
	writeCORS(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "MCP endpoint requires POST", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeRPCError(w, nil, -32700, "failed to read request body")
		return
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(w, nil, -32700, "invalid JSON-RPC request")
		return
	}

	if req.JSONRPC != "2.0" {
		writeRPCError(w, req.ID, -32600, "invalid JSON-RPC version")
		return
	}

	if isNotification(req) {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	switch req.Method {
	case "initialize":
		writeRPCResult(w, req.ID, map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    serverName,
				"version": "0.1.0",
			},
		})

	case "tools/list":
		writeRPCResult(w, req.ID, map[string]any{
			"tools": []map[string]any{
				tools.FetchURLAsTextDefinition(),
				tools.FetchRSSAsJSONDefinition(),
				tools.FetchURLAsJSONDefinition(),
				tools.GeocodeDefinition(),
				tools.WeatherDefinition(),
				tools.BrowseDefinition(),
				tools.SearchSiteDefinition(),
			},
		})

	case "tools/call":
		result, err := handleToolCall(req.Params)
		if err != nil {
			writeRPCError(w, req.ID, -32000, err.Error())
			return
		}
		writeRPCResult(w, req.ID, result)

	default:
		writeRPCError(w, req.ID, -32601, "method not found")
	}
}

// handleToolCall dispatches tool calls to the appropriate tool implementation.
// It parses the tool name and arguments, validates inputs, and returns the
// result or an error.
func handleToolCall(raw json.RawMessage) (any, error) {
	var params ToolCallParams
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("invalid tools/call params: %w", err)
	}

	switch params.Name {
	case tools.FetchURLAsTextToolName:
		var args FetchURLArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid %s arguments: %w", tools.FetchURLAsTextToolName, err)
		}

		if err := validateURL(args.URL); err != nil {
			return nil, err
		}

		return tools.FetchURLAsText(args.URL, maxOutput)
	case tools.FetchRSSAsJSONToolName:
		var args ParseRSSFeedArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid %s arguments: %w", tools.FetchRSSAsJSONToolName, err)
		}

		if err := validateURL(args.URL); err != nil {
			return nil, err
		}

		return tools.FetchRSSAsJSON(args.URL, maxOutput)
	case tools.FetchURLAsJSONToolName:
		var args FetchURLArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid %s arguments: %w", tools.FetchURLAsJSONToolName, err)
		}

		if err := validateURL(args.URL); err != nil {
			return nil, err
		}

		return tools.FetchURLAsJSON(args.URL, maxOutput)
	case tools.GeocodeToolName:
		var args GeocodeArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid %s arguments: %w", tools.GeocodeToolName, err)
		}

		if args.LocationName == "" {
			return nil, fmt.Errorf("locationname is required for %s", tools.GeocodeToolName)
		}

		return tools.Geocode(args.LocationName, maxOutput)
	case tools.WeatherToolName:
		var args WeatherArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid %s arguments: %w", tools.WeatherToolName, err)
		}

		if args.Latitude == nil || args.Longitude == nil {
			return nil, fmt.Errorf("latitude and longitude are required for %s", tools.WeatherToolName)
		}

		return tools.Weather(*args.Latitude, *args.Longitude, maxOutput)
	case tools.BrowseToolName:
		var args BrowseArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid %s arguments: %w", tools.BrowseToolName, err)
		}

		if err := validateURL(args.URL); err != nil {
			return nil, err
		}

		return tools.Browse(args.URL, args.WaitForSelector, args.TimeoutSeconds, maxOutput)
	case tools.SearchSiteToolName:
		var args SearchSiteArgs
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid %s arguments: %w", tools.SearchSiteToolName, err)
		}
		if err := validateURL(args.StartURL); err != nil {
			return nil, fmt.Errorf("invalid start_url: %w", err)
		}
		return tools.SearchSite(args.StartURL, args.Query, args.MaxPages, args.TimeoutSeconds, maxOutput)
	default:
		return nil, fmt.Errorf("unknown tool: %s", params.Name)
	}
}

// validateURL checks that a URL string is valid and uses HTTP or HTTPS scheme.
// It returns an error if the URL is empty, malformed, lacks a scheme, or lacks a host.
func validateURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("url is required")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url")
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("only http and https URLs are allowed")
	}

	if parsed.Host == "" {
		return fmt.Errorf("url must include a host")
	}

	return nil
}

// isNotification determines if a JSON-RPC request is a notification (has no ID).
// Notifications are one-way requests that do not expect a response.
func isNotification(req JSONRPCRequest) bool {
	return req.ID == nil && len(req.Method) > 0
}

// writeRPCResult writes a successful JSON-RPC response to the HTTP response writer.
// It includes the protocol version, request ID, and result data.
func writeRPCResult(w http.ResponseWriter, id any, result any) {
	writeJSON(w, JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

// writeRPCError writes an error JSON-RPC response to the HTTP response writer.
// It includes the protocol version, request ID (if provided), and error details.
func writeRPCError(w http.ResponseWriter, id any, code int, msg string) {
	writeJSON(w, JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: msg,
		},
	})
}

// writeJSON writes any value as JSON to the HTTP response writer with proper headers.
// It sets CORS headers and Content-Type, then encodes the value with indentation.
func writeJSON(w http.ResponseWriter, v any) {
	writeCORS(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// writeCORS sets the CORS headers on the HTTP response writer.
// It allows cross-origin requests from any origin with POST and OPTIONS methods.
func writeCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept, Mcp-Session-Id")
}
