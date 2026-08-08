package anidb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/wraient/curd/internal/curdhost"
)

type anidbEpisodeItem struct {
	ID      int  `json:"id"`
	Number  int  `json:"number"`
	Number2 *int `json:"number2"`
	Filler  bool `json:"filler"`
}

func getAniDBEpisodesList(showID, mode string) ([]string, error) {
	numericID := extractNumericID(showID)
	if numericID == "" {
		return nil, fmt.Errorf("invalid show id %q", showID)
	}

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

	var episodes []anidbEpisodeItem
	if err := json.Unmarshal(body, &episodes); err != nil {
		var wrapper struct {
			Episodes []anidbEpisodeItem `json:"episodes"`
		}
		if err2 := json.Unmarshal(body, &wrapper); err2 == nil {
			episodes = wrapper.Episodes
		} else {
			return nil, fmt.Errorf("parse anidb episodes response: %w", err)
		}
	}

	if len(episodes) == 0 {
		return nil, fmt.Errorf("no episodes found for anime %s", showID)
	}

	// Return 1..N relative episode numbers for the season.
	// This ensures AniList / MAL syncing operates on season-relative episode numbers (e.g. 1..24)
	// and doesn't pollute tracker progress with absolute series offsets (e.g. 49..72).
	episodesStr := make([]string, 0, len(episodes))
	for i := 1; i <= len(episodes); i++ {
		episodesStr = append(episodesStr, strconv.Itoa(i))
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
