package allanime

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

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

func getAllAnimeEpisodesList(showID, mode string) ([]string, error) {
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

	var res anidbEpisodesResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("parse anidb episodes response: %w", err)
	}
	if len(res.Episodes) == 0 {
		return nil, fmt.Errorf("no episodes found for anime %s", showID)
	}

	var numbers []int
	for _, ep := range res.Episodes {
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
