package tools

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"
)

// SearchSiteToolName is the MCP identifier for bounded website discovery.
const SearchSiteToolName = "search_site"

// Site-search page limits bound the amount of browser work per tool call.
const (
	// defaultSearchPages is used when callers omit max_pages.
	defaultSearchPages = 5
	// maxSearchPages prevents unbounded crawler work.
	maxSearchPages = 20
)

// SiteSearchResult describes one successfully rendered page in a site search.
type SiteSearchResult struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Status int    `json:"status"`
	Text   string `json:"text"`
}

// sitePageLoader abstracts page retrieval for deterministic traversal tests.
type sitePageLoader func(string) (browsedPage, error)

// SearchSiteDefinition returns the search_site MCP schema and model guidance.
func SearchSiteDefinition() map[string]any {
	return map[string]any{
		"name":        SearchSiteToolName,
		"description": "Search and browse multiple pages on one public website. Use automatically when a user asks what a site currently contains, including current auctions, listings, products, events, or other information that may require following links. The user does not need to name this tool.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"start_url":       map[string]any{"type": "string", "description": "Public HTTP or HTTPS page where discovery should begin."},
				"query":           map[string]any{"type": "string", "description": "Natural-language description of the information to find."},
				"max_pages":       map[string]any{"type": "integer", "description": "Maximum pages to visit (default 5, maximum 20).", "minimum": 1, "maximum": maxSearchPages},
				"timeout_seconds": map[string]any{"type": "integer", "description": "Timeout for each page in seconds (default 30, maximum 60).", "minimum": 1, "maximum": maxBrowseTimeout},
			},
			"required": []string{"start_url", "query"},
		},
		"annotations": map[string]any{"readOnlyHint": true},
	}
}

// SearchSite renders startURL and follows query-relevant links on the same
// hostname within caller-controlled page, timeout, and output limits.
func SearchSite(startURL, query string, maxPages, timeoutSeconds, maxOutput int) (map[string]any, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if maxPages == 0 {
		maxPages = defaultSearchPages
	}
	if maxPages < 1 || maxPages > maxSearchPages {
		return nil, fmt.Errorf("max_pages must be between 1 and %d", maxSearchPages)
	}
	if timeoutSeconds == 0 {
		timeoutSeconds = defaultBrowseTimeout
	}
	if timeoutSeconds < 1 || timeoutSeconds > maxBrowseTimeout {
		return nil, fmt.Errorf("timeout_seconds must be between 1 and %d", maxBrowseTimeout)
	}

	origin, err := url.Parse(startURL)
	if err != nil {
		return nil, fmt.Errorf("invalid start_url")
	}
	pw, browser, context, err := startBrowser()
	if err != nil {
		return nil, err
	}
	defer pw.Stop()
	defer browser.Close()
	defer context.Close()

	results := crawlSite(origin, query, maxPages, maxOutput, func(rawURL string) (browsedPage, error) {
		return browsePage(context, rawURL, "", timeoutSeconds)
	})
	return siteSearchToolResult(startURL, query, results, maxOutput)
}

// crawlSite performs bounded breadth-first traversal, ranking each page's new
// links before appending them to the crawl queue.
func crawlSite(origin *url.URL, query string, maxPages, maxOutput int, load sitePageLoader) []SiteSearchResult {
	queue := []PageLink{{URL: normalizeCrawlURL(origin), Text: query}}
	visited := make(map[string]bool)
	results := make([]SiteSearchResult, 0, maxPages)
	for len(queue) > 0 && len(visited) < maxPages {
		current := queue[0]
		queue = queue[1:]
		if visited[current.URL] {
			continue
		}
		visited[current.URL] = true

		page, pageErr := load(current.URL)
		if pageErr != nil {
			continue
		}
		pageText, _ := truncateText(page.Text, max(1024, maxOutput/maxPages))
		results = append(results, SiteSearchResult{Title: page.Title, URL: page.URL, Status: page.Status, Text: pageText})

		links := rankCrawlLinks(origin, page.Links, query, visited)
		queue = appendUniqueLinks(queue, links)
	}
	return results
}

// siteSearchToolResult serializes crawl results as MCP structured and text content.
func siteSearchToolResult(startURL, query string, results []SiteSearchResult, maxOutput int) (map[string]any, error) {
	structured := map[string]any{"query": query, "start_url": startURL, "pages_visited": len(results), "results": results}
	pretty, err := json.MarshalIndent(structured, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode site search results: %w", err)
	}
	text, truncated := truncateText(string(pretty), maxOutput)
	structured["truncated"] = truncated
	return map[string]any{
		"structuredContent": structured,
		"content":           []map[string]any{{"type": "text", "text": text}},
	}, nil
}

// rankCrawlLinks filters, normalizes, deduplicates, and relevance-sorts links.
func rankCrawlLinks(origin *url.URL, links []PageLink, query string, visited map[string]bool) []PageLink {
	type scoredLink struct {
		link  PageLink
		score int
	}
	best := make(map[string]scoredLink)
	terms := queryTerms(query)
	for _, link := range links {
		parsed, err := url.Parse(link.URL)
		if err != nil || !sameSite(origin, parsed) {
			continue
		}
		normalized := normalizeCrawlURL(parsed)
		if normalized == "" || visited[normalized] || shouldSkipCrawlURL(parsed) {
			continue
		}
		haystack := strings.ToLower(link.Text + " " + parsed.Path)
		score := 0
		for _, term := range terms {
			if strings.Contains(haystack, term) {
				score += 3
			}
		}
		for _, hint := range []string{"auction", "listing", "current", "inventory", "product", "event", "search"} {
			if strings.Contains(haystack, hint) {
				score++
			}
		}
		candidate := scoredLink{PageLink{Text: strings.TrimSpace(link.Text), URL: normalized}, score}
		if old, ok := best[normalized]; !ok || candidate.score > old.score {
			best[normalized] = candidate
		}
	}
	scored := make([]scoredLink, 0, len(best))
	for _, item := range best {
		scored = append(scored, item)
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	result := make([]PageLink, len(scored))
	for i := range scored {
		result[i] = scored[i].link
	}
	return result
}

// sameSite reports whether candidate uses HTTP or HTTPS on origin's hostname.
func sameSite(origin, candidate *url.URL) bool {
	return (candidate.Scheme == "http" || candidate.Scheme == "https") && strings.EqualFold(origin.Hostname(), candidate.Hostname())
}

// normalizeCrawlURL removes fragments so one document has one visited-set key.
func normalizeCrawlURL(value *url.URL) string {
	if value == nil {
		return ""
	}
	copy := *value
	copy.Fragment = ""
	return copy.String()
}

// shouldSkipCrawlURL identifies non-HTML assets and sensitive workflow paths.
func shouldSkipCrawlURL(value *url.URL) bool {
	path := strings.ToLower(value.Path)
	for _, suffix := range []string{".pdf", ".zip", ".jpg", ".jpeg", ".png", ".gif", ".svg", ".mp4", ".mp3"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	for _, part := range []string{"/logout", "/signout", "/login", "/signin", "/account", "/cart", "/checkout"} {
		if strings.Contains(path, part) {
			return true
		}
	}
	return false
}

// queryTerms converts a query into lowercase Unicode word tokens.
func queryTerms(query string) []string {
	return strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}

// appendUniqueLinks appends previously unqueued URLs while preserving order.
func appendUniqueLinks(queue, additions []PageLink) []PageLink {
	seen := make(map[string]bool, len(queue))
	for _, link := range queue {
		seen[link.URL] = true
	}
	for _, link := range additions {
		if !seen[link.URL] {
			queue = append(queue, link)
			seen[link.URL] = true
		}
	}
	return queue
}
