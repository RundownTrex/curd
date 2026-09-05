package anineko

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/wraient/curd/internal/providers"
)

type searchResponse struct {
	Success bool `json:"success"`
	Results []struct {
		Title string `json:"title"`
		URL   string `json:"url"`
		Image string `json:"image"`
		Meta  string `json:"meta"`
	} `json:"results"`
}

var (
	slugFromPathRE      = regexp.MustCompile(`/watch/([^/?#]+)`)
	searchPunctuationRE = regexp.MustCompile(`[:;?!/&()#"@\-_]+`)
)

func cleanSearchQuery(query string) string {
	query = strings.ReplaceAll(query, "'", "")
	query = searchPunctuationRE.ReplaceAllString(query, " ")
	return strings.Join(strings.Fields(query), " ")
}

func doSearchAnime(query string) ([]providers.SelectionOption, error) {
	rawURL := fmt.Sprintf("%s/ajax/search?q=%s", baseURL, url.QueryEscape(query))
	body, err := fetchString(rawURL, baseURL+"/")
	if err != nil {
		return nil, err
	}

	var payload searchResponse
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, fmt.Errorf("parse anineko search response: %w", err)
	}
	if !payload.Success {
		return nil, fmt.Errorf("anineko search failed")
	}

	options := make([]providers.SelectionOption, 0, len(payload.Results))
	for _, result := range payload.Results {
		slug := slugFromWatchURL(result.URL)
		if slug == "" {
			continue
		}
		label := strings.TrimSpace(result.Title)
		if meta := strings.TrimSpace(result.Meta); meta != "" {
			label = label + " — " + meta
		}
		options = append(options, providers.SelectionOption{
			Key:       slug,
			Label:     label,
			Title:     strings.TrimSpace(result.Title),
			Thumbnail: absoluteURL(result.Image),
		})
	}
	return options, nil
}

func searchAnime(query, mode string) ([]providers.SelectionOption, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty search query")
	}

	cleanQuery := cleanSearchQuery(query)
	if cleanQuery == "" {
		cleanQuery = query
	}

	options, err := doSearchAnime(cleanQuery)
	if err == nil && len(options) > 0 {
		return options, nil
	}

	// If cleaned query differed from raw query, try original query as fallback
	if cleanQuery != query {
		if rawOptions, rawErr := doSearchAnime(query); rawErr == nil && len(rawOptions) > 0 {
			return rawOptions, nil
		}
	}

	// Try prefix before colon or dash if available (e.g. "Title: Subtitle" -> "Title")
	if idx := strings.IndexAny(query, ":-"); idx > 0 {
		prefix := cleanSearchQuery(query[:idx])
		if prefix != "" && prefix != cleanQuery {
			if prefixOptions, prefixErr := doSearchAnime(prefix); prefixErr == nil && len(prefixOptions) > 0 {
				return prefixOptions, nil
			}
		}
	}

	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no results for %q", query)
}

func slugFromWatchURL(watchPath string) string {
	match := slugFromPathRE.FindStringSubmatch(watchPath)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}
