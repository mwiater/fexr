package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// FetchURLAsTextToolName is the identifier for the fetch_url_as_text tool.
const FetchURLAsTextToolName = "fetch_url_as_text"

// FetchURLAsTextDefinition returns the tool definition for fetch_url_as_text.
// The tool fetches HTML content from a URL and converts it to plain text using
// curl and html2text command-line utilities.
func FetchURLAsTextDefinition() map[string]any {
	return map[string]any{
		"name":        FetchURLAsTextToolName,
		"description": "Fetches and converts HTML content from a URL to plain text.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "HTTP or HTTPS URL to fetch.",
				},
			},
			"required": []string{"url"},
		},
		"annotations": map[string]any{
			"readOnlyHint": true,
		},
	}
}

// FetchURLAsText fetches HTML content from the given URL and converts it to plain text.
// It uses curl to retrieve the content and html2text to convert HTML to text.
// The maxOutput parameter limits the size of the returned text.
func FetchURLAsText(rawURL string, maxOutput int) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	curlCmd := exec.CommandContext(ctx, "curl", "-s", "--ssl-no-revoke", rawURL)

	curlOut, err := curlCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("curl failed: %w", err)
	}

	html2textCmd := exec.CommandContext(ctx, "html2text")
	html2textCmd.Stdin = bytes.NewReader(curlOut)

	out, err := html2textCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("html2text failed: %w", err)
	}

	text := string(out)
	if len(text) > maxOutput {
		text = text[:maxOutput] + "\n\n[output truncated]"
	}

	return map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": text,
			},
		},
	}, nil
}
