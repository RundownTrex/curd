package kickassanime

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/wraient/curd/internal/curdhost"
	"github.com/wraient/curd/internal/providers"
)

type searchItem struct {
	Title   string `json:"title"`
	TitleEn string `json:"title_en"`
	Slug    string `json:"slug"`
	Type    string `json:"type"`
	Year    int    `json:"year"`
	Poster  struct {
		HQ string `json:"hq"`
		SM string `json:"sm"`
	} `json:"poster"`
}

var kickassSeasonSuffixRE = regexp.MustCompile(
	`(?i)\s+(?:` +
		`season\s+\d+` +
		`|\d+(?:st|nd|rd|th)\s+season` +
		`|part\s+(?:\d+|[ivxlcdm]+)` +
		`|cour\s+\d+` +
		`|[ivxlcdm]{2,}` +
		`)$`,
)

func kickassQueryFallbacks(query string) []string {
	seen := map[string]struct{}{strings.ToLower(strings.TrimSpace(query)): {}}
	var out []string

	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		k := strings.ToLower(s)
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		out = append(out, s)
	}

	// 1. Cross-lookup via AniList when available
	if curdhost.SearchAniListTitles != nil {
		if en, ro, err := curdhost.SearchAniListTitles(query); err == nil {
			add(en)
			add(ro)
		}
	}

	// 2. Strip common Japanese prefixes
	lower := strings.ToLower(query)
	for _, p := range []string{"kabushiki gaisha ", "kabushikigaisha ", "gekijouban ", "shin ", "eiga "} {
		if strings.HasPrefix(lower, p) {
			add(query[len(p):])
		}
	}

	// 3. Spaced vs Hyphenated variants (e.g. "Magi Lumiere" <-> "Magi-Lumiere")
	if strings.Contains(query, " ") {
		add(strings.ReplaceAll(query, " ", "-"))
	}
	if strings.Contains(query, "-") {
		add(strings.ReplaceAll(query, "-", " "))
	}

	// 4. Strip season suffixes
	stripped := query
	for {
		next := kickassSeasonSuffixRE.ReplaceAllString(stripped, "")
		next = strings.TrimSpace(next)
		if next == stripped || next == "" {
			break
		}
		add(next)
		stripped = next
	}

	// 5. Prefix before colon or dash
	if idx := strings.IndexAny(query, ":-"); idx > 0 {
		add(query[:idx])
	}

	// 6. Progressive word dropping from right down to 1 word
	words := strings.Fields(query)
	for len(words) > 1 {
		words = words[:len(words)-1]
		add(strings.Join(words, " "))
	}

	return out
}

func executeSearch(q string) ([]searchItem, error) {
	trimmed := strings.TrimSpace(q)
	if trimmed == "" {
		return nil, nil
	}
	body, err := postJSON(baseURL+"/api/search", map[string]string{"query": trimmed}, map[string]string{
		"Referer": baseURL + "/",
	})
	if err != nil {
		return nil, err
	}
	var results []searchItem
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func formatSearchOptions(results []searchItem) []providers.SelectionOption {
	preferEnglish := true
	if curdhost.AnimeNameLanguage != nil && strings.ToLower(curdhost.AnimeNameLanguage()) == "romaji" {
		preferEnglish = false
	}

	options := make([]providers.SelectionOption, 0, len(results))
	seen := make(map[string]struct{})

	for _, r := range results {
		slug := strings.TrimSpace(r.Slug)
		if slug == "" {
			continue
		}
		if _, exists := seen[slug]; exists {
			continue
		}
		seen[slug] = struct{}{}

		title := strings.TrimSpace(r.TitleEn)
		if !preferEnglish || title == "" {
			if r.Title != "" {
				title = strings.TrimSpace(r.Title)
			}
		}
		if title == "" {
			title = slug
		}

		label := title
		if r.Year > 0 {
			label = fmt.Sprintf("%s (%d)", title, r.Year)
		}
		if r.Type != "" {
			label = fmt.Sprintf("%s [%s]", label, strings.ToUpper(r.Type))
		}

		var thumb string
		if r.Poster.HQ != "" {
			thumb = fmt.Sprintf("%s/image/poster/%s.webp", baseURL, r.Poster.HQ)
		} else if r.Poster.SM != "" {
			thumb = fmt.Sprintf("%s/image/poster/%s.webp", baseURL, r.Poster.SM)
		}

		options = append(options, providers.SelectionOption{
			Key:       slug,
			Label:     label,
			Title:     title,
			Thumbnail: thumb,
		})
	}
	return options
}

func searchAnime(query, mode string) ([]providers.SelectionOption, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil, fmt.Errorf("empty search query")
	}

	results, err := executeSearch(trimmed)
	if err == nil && len(results) > 0 {
		return formatSearchOptions(results), nil
	}

	// Try query fallbacks
	for _, fallback := range kickassQueryFallbacks(trimmed) {
		fallbackResults, fallbackErr := executeSearch(fallback)
		if fallbackErr == nil && len(fallbackResults) > 0 {
			return formatSearchOptions(fallbackResults), nil
		}
	}

	if err != nil {
		return nil, fmt.Errorf("kickassanime search: %w", err)
	}
	return nil, fmt.Errorf("no results for %q", query)
}
