package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/mmcdole/gofeed"
)

// FetchRSSAsJSONToolName is the identifier for the fetch_rss_as_JSON tool.
const FetchRSSAsJSONToolName = "fetch_rss_as_JSON"

// FetchRSSAsJSONDefinition returns the tool definition for fetch_rss_as_JSON.
// The tool parses RSS or Atom feeds and returns the data as JSON using the
// gofeed library for feed parsing.
func FetchRSSAsJSONDefinition() map[string]any {
	return map[string]any{
		"name":        FetchRSSAsJSONToolName,
		"description": "Parses RSS or Atom feeds and returns data as JSON.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "HTTP or HTTPS RSS or Atom feed URL to fetch.",
				},
			},
			"required": []string{"url"},
		},
		"annotations": map[string]any{
			"readOnlyHint": true,
		},
	}
}

// FetchRSSAsJSON fetches and parses an RSS or Atom feed from the given URL.
// It returns the feed data as JSON with both structuredContent and formatted text.
// The maxOutput parameter limits the size of the formatted text output.
func FetchRSSAsJSON(rawURL string, maxOutput int) (map[string]any, error) {
	parser := gofeed.NewParser()
	parser.Client = &http.Client{
		Timeout: 30 * time.Second,
	}

	feed, err := parser.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("feed parse failed: %w", err)
	}

	feedBytes, err := json.Marshal(feed)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal parsed feed: %w", err)
	}

	var feedJSON map[string]any
	if err := json.Unmarshal(feedBytes, &feedJSON); err != nil {
		return nil, fmt.Errorf("failed to normalize parsed feed JSON: %w", err)
	}

	prettyJSON, err := json.MarshalIndent(feedJSON, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode feed JSON: %w", err)
	}

	text := string(prettyJSON)
	if len(text) > maxOutput {
		text = text[:maxOutput] + "\n\n[output truncated]"
	}

	return map[string]any{
		"structuredContent": feedJSON,
		"content": []map[string]any{
			{
				"type": "text",
				"text": text,
			},
		},
	}, nil
}
