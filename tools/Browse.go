package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

// BrowseToolName is the identifier for the browse tool.
const BrowseToolName = "browse"

// Browser timeout defaults and limits are expressed in seconds.
const (
	// defaultBrowseTimeout is used when callers omit a navigation timeout.
	defaultBrowseTimeout = 30
	// maxBrowseTimeout caps caller-controlled browser waits.
	maxBrowseTimeout = 60
)

// PageLink is a visible hyperlink discovered on a rendered page.
type PageLink struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

// browsedPage is the normalized representation shared by browsing and crawling.
type browsedPage struct {
	URL    string
	Title  string
	Status int
	Text   string
	Links  []PageLink
}

// BrowseDefinition returns the tool definition for browse.
// The tool loads a web page in headless Chromium, waits for rendered content,
// and returns visible text. It supports waiting for specific CSS selectors
// and configurable timeouts for page rendering.
func BrowseDefinition() map[string]any {
	return map[string]any{
		"name":        BrowseToolName,
		"description": "Browse a public website in a JavaScript-enabled browser and return its current visible content and links. Use automatically when a user asks about current information on a specific website, supplies a URL or domain, or needs dynamically rendered content; the user does not need to name this tool.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "HTTP or HTTPS URL to browse.",
				},
				"wait_for_selector": map[string]any{
					"type":        "string",
					"description": "Optional CSS selector to wait for and return text from instead of the whole page body.",
				},
				"timeout_seconds": map[string]any{
					"type":        "integer",
					"description": "Navigation and rendering timeout in seconds (default 30, maximum 60).",
					"minimum":     1,
					"maximum":     maxBrowseTimeout,
				},
			},
			"required": []string{"url"},
		},
		"annotations": map[string]any{
			"readOnlyHint": true,
		},
	}
}

// Browse loads a web page in headless Chromium and returns the visible text content.
// It waits for the page to render and optionally waits for a specific CSS selector.
// The timeoutSeconds parameter controls navigation and rendering timeout (default 30, max 60).
// The maxOutput parameter limits the size of the returned text.
func Browse(rawURL, waitForSelector string, timeoutSeconds, maxOutput int) (map[string]any, error) {
	if timeoutSeconds == 0 {
		timeoutSeconds = defaultBrowseTimeout
	}
	if timeoutSeconds < 1 || timeoutSeconds > maxBrowseTimeout {
		return nil, fmt.Errorf("timeout_seconds must be between 1 and %d", maxBrowseTimeout)
	}

	pw, browser, context, err := startBrowser()
	if err != nil {
		return nil, err
	}
	defer pw.Stop()
	defer browser.Close()
	defer context.Close()

	pageResult, err := browsePage(context, rawURL, waitForSelector, timeoutSeconds)
	if err != nil {
		return nil, err
	}

	return browseToolResult(pageResult, maxOutput), nil
}

// startBrowser starts Playwright and Chromium and creates an isolated,
// consistently configured browser context.
func startBrowser() (*playwright.Playwright, playwright.Browser, playwright.BrowserContext, error) {
	pw, err := playwright.Run()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to start Playwright: %w", err)
	}

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		pw.Stop()
		return nil, nil, nil, fmt.Errorf("failed to launch headless Chromium (install it with the command in README.md): %w", err)
	}

	userAgent := fmt.Sprintf("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s Safari/537.36", browser.Version())
	context, err := browser.NewContext(playwright.BrowserNewContextOptions{
		AcceptDownloads:  playwright.Bool(false),
		Locale:           playwright.String("en-US"),
		UserAgent:        playwright.String(userAgent),
		Viewport:         &playwright.Size{Width: 1365, Height: 768},
		ExtraHttpHeaders: map[string]string{"Accept-Language": "en-US,en;q=0.9"},
	})
	if err != nil {
		browser.Close()
		pw.Stop()
		return nil, nil, nil, fmt.Errorf("failed to create browser context: %w", err)
	}
	return pw, browser, context, nil
}

// browsePage renders one URL and extracts its final URL, title, status, visible
// text, and visible links.
func browsePage(context playwright.BrowserContext, rawURL, waitForSelector string, timeoutSeconds int) (browsedPage, error) {
	page, err := context.NewPage()
	if err != nil {
		return browsedPage{}, fmt.Errorf("failed to create browser page: %w", err)
	}
	defer page.Close()

	timeoutMS := float64(timeoutSeconds * 1000)
	response, err := page.Goto(rawURL, playwright.PageGotoOptions{
		Timeout:   playwright.Float(timeoutMS),
		WaitUntil: playwright.WaitUntilStateLoad,
	})
	if err != nil {
		return browsedPage{}, fmt.Errorf("page navigation failed: %w", err)
	}

	selector := "body"
	if strings.TrimSpace(waitForSelector) != "" {
		selector = waitForSelector
		if err := page.Locator(selector).WaitFor(playwright.LocatorWaitForOptions{
			State:   playwright.WaitForSelectorStateVisible,
			Timeout: playwright.Float(timeoutMS),
		}); err != nil {
			return browsedPage{}, fmt.Errorf("wait_for_selector failed: %w", err)
		}
	} else {
		// Some pages never become idle because of analytics, streaming, or polling.
		// Treat network-idle as a best-effort rendering grace period after load.
		idleTimeout := min(timeoutMS, 5000)
		_ = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
			State:   playwright.LoadStateNetworkidle,
			Timeout: playwright.Float(idleTimeout),
		})
	}

	text, err := page.Locator(selector).InnerText()
	if err != nil {
		return browsedPage{}, fmt.Errorf("failed to read rendered page text: %w", err)
	}
	title, err := page.Title()
	if err != nil {
		return browsedPage{}, fmt.Errorf("failed to read page title: %w", err)
	}
	links, err := extractVisibleLinks(page)
	if err != nil {
		return browsedPage{}, fmt.Errorf("failed to read page links: %w", err)
	}
	status := 0
	if response != nil {
		status = response.Status()
	}

	return browsedPage{URL: page.URL(), Title: title, Status: status, Text: text, Links: links}, nil
}

// extractVisibleLinks returns labeled HTTP and HTTPS anchors that participate
// in the rendered layout.
func extractVisibleLinks(page playwright.Page) ([]PageLink, error) {
	value, err := page.Locator("a[href]").EvaluateAll(`elements => elements
        .filter(a => a.offsetParent !== null)
        .map(a => ({ text: (a.innerText || a.getAttribute('aria-label') || '').trim(), url: a.href }))
        .filter(link => link.text && /^https?:\/\//i.test(link.url))`)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var links []PageLink
	if err := json.Unmarshal(encoded, &links); err != nil {
		return nil, err
	}
	return links, nil
}

// browseToolResult converts page into MCP structured and text content.
func browseToolResult(page browsedPage, maxOutput int) map[string]any {
	text, truncated := truncateText(page.Text, maxOutput)
	structured := map[string]any{
		"url": page.URL, "title": page.Title, "status": page.Status,
		"text": text, "links": page.Links, "truncated": truncated,
	}
	return map[string]any{
		"structuredContent": structured,
		"content": []map[string]any{{
			"type": "text",
			"text": text,
		}},
	}
}

// truncateText limits value to maxOutput bytes and reports whether it changed.
func truncateText(value string, maxOutput int) (string, bool) {
	if len(value) <= maxOutput {
		return value, false
	}
	return value[:maxOutput] + "\n\n[output truncated]", true
}
