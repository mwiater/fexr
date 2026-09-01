package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// FetchURLAsJSONToolName is the identifier for the fetch_url_as_JSON tool.
const FetchURLAsJSONToolName = "fetch_url_as_JSON"

// fetchURLAsJSONUserAgent is the custom User-Agent header sent with JSON API requests.
const fetchURLAsJSONUserAgent = "fexr/0.1.0 (+https://github.com/mwiater/fexr)"

// FetchURLAsJSONDefinition returns the tool definition for fetch_url_as_JSON.
// The tool fetches data from an application/json URL and returns the JSON response.
func FetchURLAsJSONDefinition() map[string]any {
	return map[string]any{
		"name":        FetchURLAsJSONToolName,
		"description": "Fetches data from an application/json URL and returns the JSON response.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "HTTP or HTTPS URL returning application/json.",
				},
			},
			"required": []string{"url"},
		},
		"annotations": map[string]any{
			"readOnlyHint": true,
		},
	}
}

// FetchURLAsJSON fetches JSON data from the given URL with retry logic for transient errors.
// It retries up to 3 times on 502, 503, and 504 status codes. The maxOutput parameter
// limits the size of the formatted text output.
func FetchURLAsJSON(rawURL string, maxOutput int) (map[string]any, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	var lastStatus string
	for attempt := 0; attempt < 3; attempt++ {
		body, resp, err := doJSONFetch(client, rawURL)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout {
			lastStatus = resp.Status
			resp.Body.Close()
			if attempt < 2 {
				time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
				continue
			}
			return nil, fmt.Errorf("unexpected status code after retries: %s", lastStatus)
		}

		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, fmt.Errorf("unexpected status code: %s", resp.Status)
		}

		var parsed any
		decoder := json.NewDecoder(strings.NewReader(string(body)))
		decoder.UseNumber()
		if err := decoder.Decode(&parsed); err != nil {
			return nil, fmt.Errorf("failed to decode JSON: %w", err)
		}

		prettyJSON, err := json.MarshalIndent(parsed, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("failed to encode JSON: %w", err)
		}

		text := string(prettyJSON)
		if len(text) > maxOutput {
			text = text[:maxOutput] + "\n\n[output truncated]"
		}

		return map[string]any{
			"structuredContent": parsed,
			"content": []map[string]any{{
				"type": "text",
				"text": text,
			}},
		}, nil
	}

	if lastStatus != "" {
		return nil, fmt.Errorf("unexpected status code after retries: %s", lastStatus)
	}

	return nil, fmt.Errorf("request failed after retries")
}

// doJSONFetch performs a single HTTP GET request to fetch JSON data.
// It sets the Accept and User-Agent headers and validates the response content type.
// Returns the response body, response object, and any error encountered.
func doJSONFetch(client *http.Client, rawURL string) ([]byte, *http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to build request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", fetchURLAsJSONUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 && contentType != "" && !strings.Contains(contentType, "json") {
		defer resp.Body.Close()
		return nil, nil, fmt.Errorf("response content-type is not JSON: %s", contentType)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		return nil, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return body, resp, nil
}
