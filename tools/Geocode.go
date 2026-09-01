package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GeocodeToolName is the identifier for the geocode tool.
const GeocodeToolName = "geocode"

// GeocodeBaseURL is the Open-Meteo geocoding API endpoint used for location lookups.
var GeocodeBaseURL = "https://geocoding-api.open-meteo.com/v1/search"

// GeocodeDefinition returns the tool definition for geocode.
// The tool geocodes a location name and returns the Open-Meteo JSON response
// containing latitude, longitude, and other location metadata.
func GeocodeDefinition() map[string]any {
	return map[string]any{
		"name":        GeocodeToolName,
		"description": "Geocodes a location name and returns the Open-Meteo JSON response.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"locationname": map[string]any{
					"type":        "string",
					"description": "Location name to geocode.",
				},
			},
			"required": []string{"locationname"},
		},
		"annotations": map[string]any{
			"readOnlyHint": true,
		},
	}
}

// Geocode geocodes the given location name using the Open-Meteo geocoding API.
// It returns the API response with location data including latitude and longitude.
// The maxOutput parameter limits the size of the formatted text output.
func Geocode(locationName string, maxOutput int) (map[string]any, error) {
	if strings.TrimSpace(locationName) == "" {
		return nil, fmt.Errorf("locationname is required")
	}

	endpoint, err := url.Parse(GeocodeBaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to build geocode request: %w", err)
	}

	query := endpoint.Query()
	query.Set("name", locationName)
	query.Set("count", "10")
	query.Set("language", "en")
	query.Set("format", "json")
	endpoint.RawQuery = query.Encode()

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create geocode request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geocode request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("geocode request failed: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read geocode response: %w", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("response body is not a JSON object: %w", err)
	}

	prettyJSON, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode geocode response: %w", err)
	}

	text := string(prettyJSON)
	if len(text) > maxOutput {
		text = text[:maxOutput] + "\n\n[output truncated]"
	}

	return map[string]any{
		"structuredContent": parsed,
		"content": []map[string]any{
			{
				"type": "text",
				"text": text,
			},
		},
	}, nil
}
