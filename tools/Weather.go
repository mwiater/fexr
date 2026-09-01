package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// WeatherToolName is the identifier for the weather tool.
const WeatherToolName = "weather"

// WeatherBaseURL is the Open-Meteo forecast API endpoint used for weather data retrieval.
var WeatherBaseURL = "https://api.open-meteo.com/v1/forecast"

// WeatherDefinition returns the tool definition for weather.
// The tool fetches weather forecast data for a given latitude and longitude
// using the Open-Meteo API, including current conditions and 5-day forecasts.
func WeatherDefinition() map[string]any {
	return map[string]any{
		"name":        WeatherToolName,
		"description": "Fetches an Open-Meteo forecast for a latitude and longitude and returns the JSON response.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"latitude": map[string]any{
					"type":        "number",
					"description": "Latitude coordinate.",
				},
				"longitude": map[string]any{
					"type":        "number",
					"description": "Longitude coordinate.",
				},
			},
			"required": []string{"latitude", "longitude"},
		},
		"annotations": map[string]any{
			"readOnlyHint": true,
		},
	}
}

// Weather fetches weather forecast data for the given latitude and longitude.
// It returns current conditions and a 5-day forecast with temperature, humidity,
// wind speed, and precipitation data. The maxOutput parameter limits the size
// of the formatted text output.
func Weather(latitude, longitude float64, maxOutput int) (map[string]any, error) {
	endpoint, err := url.Parse(WeatherBaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to build weather request: %w", err)
	}

	query := endpoint.Query()
	query.Set("latitude", strconv.FormatFloat(latitude, 'f', -1, 64))
	query.Set("longitude", strconv.FormatFloat(longitude, 'f', -1, 64))
	query.Set("current", "temperature_2m,relative_humidity_2m,wind_speed_10m,weather_code")
	query.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max")
	query.Set("forecast_days", "5")
	query.Set("temperature_unit", "fahrenheit")
	query.Set("wind_speed_unit", "mph")
	query.Set("precipitation_unit", "inch")
	query.Set("timezone", "America/Los_Angeles")
	endpoint.RawQuery = query.Encode()

	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create weather request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("weather request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("weather request failed: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read weather response: %w", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("response body is not a JSON object: %w", err)
	}

	prettyJSON, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode weather response: %w", err)
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
