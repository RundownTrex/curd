package hianime

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	epItemRE     = regexp.MustCompile(`(?s)<a\b([^>]*)class=["'][^"']*ep-item[^"']*["']([^>]*)>(.*?)</a>`)
	epItemAltRE  = regexp.MustCompile(`(?s)<a\b([^>]*)>(.*?)</a>`)
	dataNumberRE = regexp.MustCompile(`data-number=["']([^"']+)["']`)
	dataIDRE     = regexp.MustCompile(`data-id=["']([0-9]+)["']`)
	epTitleRE    = regexp.MustCompile(`title=["']([^"']+)["']`)
)

// hianimeEpisodesCacheEntry stores a parsed episode list for one show.
type hianimeEpisodesCacheEntry struct {
	entries   []episodeEntry
	fetchedAt time.Time
}

var (
	hianimeEpisodesCache   = map[string]*hianimeEpisodesCacheEntry{}
	hianimeEpisodesCacheMu sync.Mutex
	hianimeEpisodesTTL     = 15 * time.Minute
)

func extractAnimeID(showID string) string {
	showID = strings.TrimSpace(showID)
	if idx := strings.LastIndex(showID, "-"); idx >= 0 && idx < len(showID)-1 {
		candidate := showID[idx+1:]
		if _, err := strconv.Atoi(candidate); err == nil {
			return candidate
		}
	}
	return showID
}

// fetchEpisodeEntries returns the episode list for a show, using an in-memory
// cache so the list endpoint is only hit once per session per show.
func fetchEpisodeEntries(showID string) ([]episodeEntry, error) {
	showID = strings.TrimSpace(showID)
	if showID == "" {
		return nil, fmt.Errorf("empty show id")
	}

	hianimeEpisodesCacheMu.Lock()
	if e, ok := hianimeEpisodesCache[showID]; ok && time.Since(e.fetchedAt) < hianimeEpisodesTTL {
		entries := e.entries
		hianimeEpisodesCacheMu.Unlock()
		return entries, nil
	}
	hianimeEpisodesCacheMu.Unlock()

	animeID := extractAnimeID(showID)
	if animeID == "" {
		return nil, fmt.Errorf("could not extract anime id from %q", showID)
	}

	rawURL := fmt.Sprintf("%s/api/theme/episode/list/%s", baseURL, url.PathEscape(animeID))
	var resp episodesResponse
	if err := fetchJSON(rawURL, baseURL+"/", &resp); err != nil {
		return nil, fmt.Errorf("fetch hianime episode list: %w", err)
	}

	if !resp.Status {
		return nil, fmt.Errorf("hianime episode list returned status false for %s", animeID)
	}

	matches := epItemRE.FindAllStringSubmatch(resp.HTML, -1)
	if len(matches) == 0 {
		// Fallback: check all <a> tags that have class containing ep-item
		allA := epItemAltRE.FindAllStringSubmatch(resp.HTML, -1)
		for _, m := range allA {
			if len(m) < 3 {
				continue
			}
			attrs := m[1]
			if strings.Contains(attrs, "ep-item") {
				matches = append(matches, []string{m[0], attrs, "", m[2]})
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no episodes found in hianime response for %s", showID)
	}

	entries := make([]episodeEntry, 0, len(matches))
	seenIDs := make(map[string]struct{}, len(matches))

	for _, m := range matches {
		attrs := m[1] + " " + m[2]

		numMatch := dataNumberRE.FindStringSubmatch(attrs)
		idMatch := dataIDRE.FindStringSubmatch(attrs)
		if len(numMatch) < 2 || len(idMatch) < 2 {
			continue
		}

		epNum := strings.TrimSpace(numMatch[1])
		epID := strings.TrimSpace(idMatch[1])
		if epID == "" || epNum == "" {
			continue
		}

		if _, seen := seenIDs[epID]; seen {
			continue
		}
		seenIDs[epID] = struct{}{}

		var epTitle string
		if tMatch := epTitleRE.FindStringSubmatch(attrs); len(tMatch) >= 2 {
			epTitle = strings.TrimSpace(html.UnescapeString(tMatch[1]))
		}

		entries = append(entries, episodeEntry{
			Number: epNum,
			ID:     epID,
			Title:  epTitle,
		})
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no valid episode entries parsed for %s", showID)
	}

	hianimeEpisodesCacheMu.Lock()
	hianimeEpisodesCache[showID] = &hianimeEpisodesCacheEntry{
		entries:   entries,
		fetchedAt: time.Now(),
	}
	hianimeEpisodesCacheMu.Unlock()

	return entries, nil
}

func matchEpisodeNumber(rawNumber string, target int) bool {
	rawNumber = strings.TrimSpace(rawNumber)
	if rawNumber == strconv.Itoa(target) {
		return true
	}
	if n, err := strconv.Atoi(rawNumber); err == nil && n == target {
		return true
	}
	if f, err := strconv.ParseFloat(rawNumber, 64); err == nil && int(f) == target {
		return true
	}
	// Also test stripping leading zeros e.g. "01" -> "1"
	if strings.TrimLeft(rawNumber, "0") == strconv.Itoa(target) {
		return true
	}
	return false
}

func episodesList(showID, mode string) ([]string, error) {
	_ = mode
	entries, err := fetchEpisodeEntries(showID)
	if err != nil {
		return nil, err
	}

	list := make([]string, 0, len(entries))
	for _, entry := range entries {
		list = append(list, entry.Number)
	}
	return list, nil
}
