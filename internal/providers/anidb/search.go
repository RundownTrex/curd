package anidb

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/wraient/curd/internal/curdhost"
	"github.com/wraient/curd/internal/providers"
)

const (
	anidbUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	anidbBaseURL   = "https://anidb.app"
)

var (
	anidbCardLinkPattern = regexp.MustCompile(`(?i)<a\s+href="(?:https://anidb\.app)?/anime/([a-z0-9-]+-[0-9]+)"[^>]*title="([^"]+)"`)
	anidbCardAltPattern  = regexp.MustCompile(`(?i)<a\s+href="(?:https://anidb\.app)?/anime/([a-z0-9-]+-[0-9]+)".*?alt="([^"]+)"`)
	defaultHTTPClient    = &http.Client{Timeout: 15 * time.Second}
)

func searchAniDB(query, mode string) ([]providers.SelectionOption, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty search query")
	}

	searchURL := fmt.Sprintf("%s/browse?q=%s", anidbBaseURL, url.QueryEscape(query))
	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", anidbUserAgent)
	req.Header.Set("Referer", anidbBaseURL+"/")

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if !curdhost.HTTPStatusOK(resp.StatusCode) {
		return nil, curdhost.HTTPStatusError("anidb search", resp.StatusCode, body)
	}

	page := string(body)
	if strings.Contains(strings.ToLower(page), "just a moment") {
		return nil, fmt.Errorf("blocked by cloudflare on anidb")
	}

	options := parseAniDBSearchPage(page)
	if len(options) == 0 {
		return nil, fmt.Errorf("no search results found for query %q", query)
	}
	return options, nil
}

func parseAniDBSearchPage(page string) []providers.SelectionOption {
	var options []providers.SelectionOption
	seen := make(map[string]bool)

	matches := anidbCardLinkPattern.FindAllStringSubmatch(page, -1)
	if len(matches) == 0 {
		matches = anidbCardAltPattern.FindAllStringSubmatch(page, -1)
	}

	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		slug := strings.TrimSpace(m[1])
		title := html.UnescapeString(strings.TrimSpace(m[2]))

		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true

		options = append(options, providers.SelectionOption{
			Key:   slug,
			Label: title,
			Title: title,
		})
	}

	return options
}

func httpClient() *http.Client {
	if curdhost.HTTPClient != nil {
		if c := curdhost.HTTPClient(); c != nil {
			return c
		}
	}
	return defaultHTTPClient
}
