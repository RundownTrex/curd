package anipub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/wraient/curd/internal/providers"
)

// seasonSuffixRE matches common season/part/cour suffixes appended by AniList/MAL
// to the English title, e.g. "Season 2", "2nd Season", "Part II", "Cour 2".
var seasonSuffixRE = regexp.MustCompile(
	`(?i)\s+(?:` +
		`season\s+\d+` + // "Season 2"
		`|\d+(?:st|nd|rd|th)\s+season` + // "2nd Season"
		`|part\s+(?:\d+|[ivxlcdm]+)` + // "Part 2", "Part II"
		`|cour\s+\d+` + // "Cour 2"
		`|[ivxlcdm]{2,}` + // standalone roman numerals (e.g. trailing "II")
		`)$`,
)

// normalizeAnipubQuery sanitizes the raw query for anipub's search API:
//   - strips apostrophes (anipub API crashes on them)
//   - collapses extra whitespace
func normalizeAnipubQuery(q string) string {
	q = strings.TrimSpace(q)
	q = strings.ReplaceAll(q, "'", "")
	q = strings.Join(strings.Fields(q), " ")
	return q
}

// queryFallbacks builds a sequence of progressively-shorter search queries to
// try when the full title returns no results from anipub.  The sequence is:
//  1. The query with all trailing season/part/cour suffixes stripped.
//  2. Progressively drop one word at a time from the right, down to 2 words.
func queryFallbacks(query string) []string {
	seen := map[string]struct{}{}
	var out []string

	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || s == query {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	// Step 1: strip season/part/cour suffixes (may strip multiple in a loop)
	stripped := query
	for {
		next := seasonSuffixRE.ReplaceAllString(stripped, "")
		next = strings.TrimSpace(next)
		if next == stripped || next == "" {
			break
		}
		add(next)
		stripped = next
	}

	// Step 2: progressively drop trailing words (minimum 2 words)
	base := stripped
	words := strings.Fields(base)
	for len(words) > 2 {
		words = words[:len(words)-1]
		add(strings.Join(words, " "))
	}

	return out
}

func searchAnime(query, mode string) ([]providers.SelectionOption, error) {
	query = normalizeAnipubQuery(query)
	if query == "" {
		return nil, fmt.Errorf("empty search query")
	}
	_ = mode

	// Try the full (normalized) query first, then fall back to shorter variants.
	queries := append([]string{query}, queryFallbacks(query)...)

	var lastErr error
	for _, q := range queries {
		results, err := fetchSearchResults(q)
		if err != nil {
			lastErr = err
			continue
		}
		if len(results) == 0 {
			lastErr = fmt.Errorf("no results for %q", q)
			continue
		}

		infos := fetchSearchInfos(results)
		options := make([]providers.SelectionOption, 0, len(results))
		for i, item := range results {
			if item.ID <= 0 {
				continue
			}
			info := infos[i]
			label := formatSearchLabel(item, info)
			extra := toSearchItem(item, info)
			options = append(options, providers.SelectionOption{
				Key:       strconv.Itoa(item.ID),
				Label:     label,
				Title:     strings.TrimSpace(item.Name),
				Thumbnail: thumbnailFromInfo(info, item.Image),
				ExtraData: extra,
			})
		}
		if len(options) > 0 {
			return options, nil
		}
		lastErr = fmt.Errorf("no results for %q", q)
	}

	if lastErr != nil {
		return nil, fmt.Errorf("anipub lookup: %w", lastErr)
	}
	return nil, fmt.Errorf("anipub lookup: no results for %q", query)
}

func fetchSearchResults(query string) ([]searchResult, error) {
	raw, err := fetchString(searchURL(query), baseURL+"/")
	if err != nil {
		return nil, err
	}
	return decodeSearchResults([]byte(raw))
}

func decodeSearchResults(raw []byte) ([]searchResult, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty search response")
	}

	var notFound struct {
		Found bool `json:"found"`
	}
	if err := json.Unmarshal(raw, &notFound); err == nil && strings.Contains(string(raw), `"found"`) && !notFound.Found {
		return nil, nil
	}

	var results []searchResult
	if err := json.Unmarshal(raw, &results); err == nil {
		return results, nil
	}

	var single searchResult
	if err := json.Unmarshal(raw, &single); err == nil && single.ID > 0 {
		return []searchResult{single}, nil
	}

	return nil, fmt.Errorf("parse anipub search response")
}

func fetchSearchInfos(results []searchResult) []infoResponse {
	infos := make([]infoResponse, len(results))
	var wg sync.WaitGroup
	wg.Add(len(results))
	for i, item := range results {
		i, item := i, item
		go func() {
			defer wg.Done()
			if item.ID <= 0 {
				return
			}
			var info infoResponse
			if err := fetchJSON(infoURL(strconv.Itoa(item.ID)), baseURL+"/", &info); err == nil {
				infos[i] = info
			}
		}()
	}
	wg.Wait()
	return infos
}

func formatSearchLabel(item searchResult, info infoResponse) string {
	parts := []string{strings.TrimSpace(item.Name)}
	if info.EpCount > 0 {
		parts = append(parts, fmt.Sprintf("%d eps", info.EpCount))
	}
	return strings.Join(parts, " · ")
}

func toSearchItem(item searchResult, info infoResponse) SearchItem {
	malID, _ := strconv.Atoi(strings.TrimSpace(info.MALID))
	episodes := info.EpCount
	if episodes <= 0 && info.ID > 0 {
		episodes = info.EpCount
	}
	return SearchItem{
		ID:       item.ID,
		MalID:    malID,
		Name:     strings.TrimSpace(item.Name),
		Finder:   strings.TrimSpace(item.Finder),
		Episodes: episodes,
	}
}
