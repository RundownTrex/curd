package megaplay

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wraient/curd/internal/providers"
)

func searchAnime(query, mode string) ([]providers.SelectionOption, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty search query")
	}
	_ = mode

	// Strip provider label artifacts that other providers append to their titles.
	// Examples from other providers:
	//   "Title (25 episodes) [mkissa]"  → "Title"
	//   "Title · 25 eps"               → "Title"
	//   "Title — TV"                   → "Title"
	//   "Title [anipub]"               → "Title"
	query = sanitizeSearchQuery(query)
	if query == "" {
		return nil, fmt.Errorf("empty search query after sanitization")
	}

	result, err := searchAniList(query)
	if err != nil {
		return nil, err
	}
	if len(result.Data.Page.Media) == 0 {
		return nil, fmt.Errorf("no results for %q", query)
	}

	options := make([]providers.SelectionOption, 0, len(result.Data.Page.Media))
	for _, media := range result.Data.Page.Media {
		if media.IDMal == nil || *media.IDMal <= 0 {
			continue
		}
		malID := *media.IDMal

		title := strings.TrimSpace(media.Title.English)
		if title == "" {
			title = strings.TrimSpace(media.Title.Romaji)
		}
		if title == "" {
			continue
		}

		episodes := 0
		if media.Episodes != nil {
			episodes = *media.Episodes
		}

		label := formatSearchLabel(title, episodes)
		options = append(options, providers.SelectionOption{
			Key:       strconv.Itoa(malID),
			Label:     label,
			Title:     title,
			Thumbnail: fmt.Sprintf("%d", media.ID),
			ExtraData: SearchItem{
				MalID:    malID,
				Title:    title,
				Episodes: episodes,
			},
		})
	}

	if len(options) == 0 {
		return nil, fmt.Errorf("no results with MAL IDs for %q", query)
	}
	return options, nil
}

func formatSearchLabel(title string, episodes int) string {
	if episodes > 0 {
		return fmt.Sprintf("%s · %d eps", title, episodes)
	}
	return title
}

// sanitizeSearchQuery strips label artifacts that other providers append to their
// anime titles before using the title as an AniList search query. Without this,
// auto-search after provider switching fails because AniList can't match decorated
// labels like "That Time I Got Reincarnated as a Slime (25 episodes) [mkissa]".
func sanitizeSearchQuery(query string) string {
	// Strip " [provider]" style suffixes (e.g. "[mkissa]", "[anipub]").
	if idx := strings.LastIndex(query, " ["); idx != -1 {
		if close := strings.Index(query[idx:], "]"); close != -1 {
			query = strings.TrimSpace(query[:idx])
		}
	}
	// Strip " (N episodes)" or " (N eps)" suffixes where N is a number or "?".
	// Only strip if the content inside parens starts with a digit or "?".
	if idx := strings.LastIndex(query, " ("); idx != -1 {
		rest := query[idx+2:]
		if len(rest) > 0 && (rest[0] >= '0' && rest[0] <= '9' || rest[0] == '?') {
			if strings.Contains(rest, "episode") || strings.HasPrefix(rest, "? eps") ||
				(len(rest) > 2 && strings.Contains(rest, " eps")) {
				query = strings.TrimSpace(query[:idx])
			}
		}
	}
	// Strip " · N eps" suffixes (our own label format).
	if idx := strings.LastIndex(query, " · "); idx != -1 {
		query = strings.TrimSpace(query[:idx])
	}
	// Strip " — suffix" patterns (e.g. " — TV").
	if idx := strings.Index(query, " — "); idx != -1 {
		query = strings.TrimSpace(query[:idx])
	}
	return strings.TrimSpace(query)
}
