package allanime

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/wraient/curd/internal/curdhost"
	"github.com/wraient/curd/internal/providers"
)

const (
	anidbUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	anidbBaseURL   = "https://anidb.app"
)

var (
	anidbSearchCardPattern = regexp.MustCompile(`(?s)<a\s+href="https://anidb\.app/anime/([^"]+)".*?alt="([^"]+)"`)
)

func searchAllAnime(query, mode string) ([]providers.SelectionOption, error) {
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
	matches := anidbSearchCardPattern.FindAllStringSubmatch(page, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no search results for %q", query)
	}

	options := make([]providers.SelectionOption, 0, len(matches))
	seen := make(map[string]struct{})

	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		slugID := match[1]
		title := html.UnescapeString(strings.TrimSpace(match[2]))
		if _, exists := seen[slugID]; exists {
			continue
		}
		seen[slugID] = struct{}{}

		options = append(options, providers.SelectionOption{
			Title: title,
			Key:   slugID,
			Label: title,
		})
	}

	if len(options) == 0 {
		return nil, fmt.Errorf("no search results found for %q", query)
	}
	return options, nil
}

func logAllanime(msg string) {
	curdhost.Log("allanime: " + msg)
}
