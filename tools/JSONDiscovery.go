package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// DiscoverJSONToolName is the identifier for the discover_json tool.
const DiscoverJSONToolName = "discover_json"

const discoverJSONMaxOutput = 50000

// ExtractedJSON is one JSON value found in a page's embedded data or network
// responses.
type ExtractedJSON struct {
	SourceType string `json:"source_type"`
	SourceURL  string `json:"source_url,omitempty"`
	Data       any    `json:"data"`
}

// CallToolResult represents the required MCP object structure for tool responses.
type CallToolResult struct {
	Content []CallToolContent `json:"content"`
	IsError bool              `json:"isError,omitempty"`
}

type CallToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// DiscoverJSONDefinition returns the tool definition for discover_json.
func DiscoverJSONDefinition() map[string]any {
	return map[string]any{
		"name":        DiscoverJSONToolName,
		"description": "Scans a URL for direct JSON responses, background XHR/Fetch JSON requests, or embedded application/json data blocks in the HTML source.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The full HTTP or HTTPS URL to examine.",
				},
			},
			"required": []string{"url"},
		},
		"annotations": map[string]any{"readOnlyHint": true},
	}
}

// DiscoverAllJSON renders targetURL and extracts parseable JSON embedded in
// recognized script elements and returned by JSON network responses.
func DiscoverAllJSON(targetURL string) ([]ExtractedJSON, error) {
	pw, browser, browserContext, err := startBrowser()
	if err != nil {
		return nil, err
	}
	defer pw.Stop()
	defer browser.Close()
	defer browserContext.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	page, err := browserContext.NewPage()
	if err != nil {
		return nil, fmt.Errorf("failed to create browser page: %w", err)
	}

	var mu sync.Mutex
	networkResults := make([]ExtractedJSON, 0)
	page.OnResponse(func(response playwright.Response) {
		if !responseHasJSONContentType(response.Headers()) {
			return
		}
		body, bodyErr := response.Body()
		if bodyErr != nil {
			return
		}
		var data any
		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.UseNumber()
		if decoder.Decode(&data) != nil {
			return
		}
		mu.Lock()
		networkResults = append(networkResults, ExtractedJSON{SourceType: "Network: XHR/Fetch", SourceURL: response.URL(), Data: data})
		mu.Unlock()
	})

	if _, err := page.Goto(targetURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateLoad, Timeout: playwright.Float(20000)}); err != nil {
		return nil, fmt.Errorf("browser execution failed: %w", err)
	}
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("browser execution failed: %w", ctx.Err())
	case <-time.After(4 * time.Second):
	}

	const jsExtract = `(() => {
    const scripts = document.querySelectorAll('script[type="application/json"], script[type="application/ld+json"], script#__NEXT_DATA__, script#svelte-announcer');
    return Array.from(scripts).map(s => {
      let source = s.type || 'unknown type';
      if (s.id) source = "id: " + s.id;
      return {source_type: "DOM: " + source, raw_data: s.textContent.trim()};
    }).filter(item => item.raw_data.length > 0);
  })()`

	var domItems []struct {
		SourceType string `json:"source_type"`
		RawData    string `json:"raw_data"`
	}

	if _, err := page.Evaluate(jsExtract, &domItems); err != nil {
		return nil, fmt.Errorf("failed to extract embedded JSON: %w", err)
	}

	for _, item := range domItems {
		var data any
		decoder := json.NewDecoder(strings.NewReader(item.RawData))
		decoder.UseNumber()

		if err := decoder.Decode(&data); err == nil {
			networkResults = append(networkResults, ExtractedJSON{
				SourceType: item.SourceType,
				SourceURL:  targetURL,
				Data:       data,
			})
		} else {
			// Fallback: If it's malformed JSON, pass the raw string instead of dropping it entirely
			networkResults = append(networkResults, ExtractedJSON{
				SourceType: item.SourceType + " (Malformed JSON)",
				SourceURL:  targetURL,
				Data:       item.RawData, // Pass the raw text so the LLM can still read it
			})
		}
	}

	return networkResults, nil
}

func responseHasJSONContentType(headers map[string]string) bool {
	for name, value := range headers {
		if strings.EqualFold(name, "content-type") && strings.Contains(strings.ToLower(value), "json") {
			return true
		}
	}
	return false
}

// FormatForMCP serializes discovery results, caps the payload, and wraps it
// in the required Model Context Protocol object structure.
func FormatForMCP(results []ExtractedJSON) CallToolResult {
	if len(results) == 0 {
		return CallToolResult{
			Content: []CallToolContent{{Type: "text", Text: "No JSON objects found via DOM or Network."}},
		}
	}

	// 1. Truncate individual massive items so we don't lose the tail end of the array
	for i, res := range results {
		itemBytes, err := json.Marshal(res.Data)
		if err == nil && len(itemBytes) > 10000 {
			// Replace the huge data object with a truncated string preview
			results[i].Data = fmt.Sprintf("[TRUNCATED: Original size %d bytes] %s...", len(itemBytes), string(itemBytes[:10000]))
		}
	}

	// 2. Marshal the safely capped array
	out, err := json.MarshalIndent(results, "", "  ")

	var textOutput string
	if err != nil {
		textOutput = fmt.Sprintf("Error formatting results: %v", err)
	} else if len(out) > discoverJSONMaxOutput {
		// 3. Fallback guardrail just in case the array has hundreds of small items
		textOutput = fmt.Sprintf("Warning: The overall payload is still too large (%d bytes).\n\nShowing truncated preview:\n\n%s\n\n... [TRUNCATED]", len(out), out[:discoverJSONMaxOutput])
	} else {
		textOutput = string(out)
	}

	return CallToolResult{
		Content: []CallToolContent{{Type: "text", Text: textOutput}},
	}
}
