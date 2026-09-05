package kickassanime

import (
	"encoding/json"
	"fmt"
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

func searchAnime(query, mode string) ([]providers.SelectionOption, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil, fmt.Errorf("empty search query")
	}

	body, err := postJSON(baseURL+"/api/search", map[string]string{"query": trimmed}, map[string]string{
		"Referer": baseURL + "/",
	})
	if err != nil {
		return nil, fmt.Errorf("kickassanime search: %w", err)
	}

	var results []searchItem
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("parse kickassanime search results: %w", err)
	}

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

	if len(options) == 0 {
		return nil, fmt.Errorf("no results for %q", query)
	}

	return options, nil
}
