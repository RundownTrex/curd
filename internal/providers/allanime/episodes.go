package allanime

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wraient/curd/internal/curdhost"
)

type anidbEpisodeItem struct {
	ID     int  `json:"id"`
	Number int  `json:"number"`
	Filler bool `json:"filler"`
}

type anidbEpisodesResponse struct {
	Episodes []anidbEpisodeItem `json:"episodes"`
}

// anidbEpisodesCache caches the full episode list per numeric anime ID so
// that both EpisodesList() and the stream resolver never hit the same
// endpoint twice for the same session.
type anidbEpisodesCacheEntry struct {
	items     []anidbEpisodeItem
	fetchedAt time.Time
}

var (
	anidbEpisodesCache   = map[string]*anidbEpisodesCacheEntry{}
	anidbEpisodesCacheMu sync.Mutex
	anidbEpisodesTTL     = 15 * time.Minute
)

// fetchAnidbEpisodeItems fetches (or returns a cached copy of) the full
// episode list for a given numeric anime ID.
func fetchAnidbEpisodeItems(numericID string) ([]anidbEpisodeItem, error) {
	anidbEpisodesCacheMu.Lock()
	if e, ok := anidbEpisodesCache[numericID]; ok && time.Since(e.fetchedAt) < anidbEpisodesTTL {
		items := e.items
		anidbEpisodesCacheMu.Unlock()
		return items, nil
	}
	anidbEpisodesCacheMu.Unlock()

	episodesURL := fmt.Sprintf("%s/api/frontend/anime/%s/episodes", anidbBaseURL, numericID)
	req, err := http.NewRequest("GET", episodesURL, nil)
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
		return nil, curdhost.HTTPStatusError("anidb episode list", resp.StatusCode, body)
	}

	var res anidbEpisodesResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("parse anidb episodes response: %w", err)
	}

	anidbEpisodesCacheMu.Lock()
	anidbEpisodesCache[numericID] = &anidbEpisodesCacheEntry{
		items:     res.Episodes,
		fetchedAt: time.Now(),
	}
	anidbEpisodesCacheMu.Unlock()

	return res.Episodes, nil
}

func getAllAnimeEpisodesList(showID, mode string) ([]string, error) {
	numericID := extractNumericID(showID)
	if numericID == "" {
		return nil, fmt.Errorf("invalid show id %q", showID)
	}

	episodes, err := fetchAnidbEpisodeItems(numericID)
	if err != nil {
		return nil, err
	}
	if len(episodes) == 0 {
		return nil, fmt.Errorf("no episodes found for anime %s", showID)
	}

	var numbers []int
	for _, ep := range episodes {
		if ep.Number > 0 {
			numbers = append(numbers, ep.Number)
		}
	}
	sort.Ints(numbers)

	episodesStr := make([]string, 0, len(numbers))
	for _, num := range numbers {
		episodesStr = append(episodesStr, strconv.Itoa(num))
	}
	return episodesStr, nil
}

func extractNumericID(showID string) string {
	showID = strings.TrimSpace(showID)
	if idx := strings.LastIndex(showID, "-"); idx >= 0 {
		return showID[idx+1:]
	}
	return showID
}
