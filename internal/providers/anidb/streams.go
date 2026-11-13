package anidb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/wraient/curd/internal/curdhost"
	"github.com/wraient/curd/internal/providers"
)

type anidbLanguageItem struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	EmbedURL string `json:"embed_url"`
}

type anidbLanguagesResponse struct {
	Languages []anidbLanguageItem `json:"languages"`
}

var embedMasterPattern = regexp.MustCompile(`file:\s*[\x27\x22]([^\x27\x22]+master\.m3u8[^\x27\x22]*|[^\x27\x22]+\.m3u8[^\x27\x22]*)[\x27\x22]`)

func getAniDBEpisodeStreamsForMode(id, mode string, epNo int) ([]string, map[string]providers.StreamPlaybackHint, error) {
	numericID := extractNumericID(id)
	if numericID == "" {
		return nil, nil, fmt.Errorf("invalid show id %q", id)
	}

	// 1. Fetch (or use cached) episodes list to locate episode ID matching epNo
	episodes, err := fetchAnidbEpisodeItems(numericID)
	if err != nil {
		return nil, nil, err
	}

	var targetEpID int
	// 1. Direct match on ep.Number == epNo
	for _, ep := range episodes {
		if ep.Number == epNo {
			targetEpID = ep.ID
			break
		}
	}

	// 2. Relative index match (1-based: epNo 1 -> episodes[0], epNo 2 -> episodes[1], etc.)
	if targetEpID == 0 && epNo >= 1 && epNo <= len(episodes) {
		targetEpID = episodes[epNo-1].ID
	}

	// 3. Offset match: if episodes start at an absolute offset > 1 (e.g. 49 for Season 3)
	if targetEpID == 0 && len(episodes) > 0 {
		minNum := episodes[0].Number
		targetNum := minNum + epNo - 1
		for _, ep := range episodes {
			if ep.Number == targetNum {
				targetEpID = ep.ID
				break
			}
		}
	}

	if targetEpID == 0 {
		return nil, nil, fmt.Errorf("episode %d not found for anime %s", epNo, id)
	}

	// 2. Fetch language options for target episode ID
	langURL := fmt.Sprintf("%s/api/frontend/episode/%d/languages", anidbBaseURL, targetEpID)
	req2, err := http.NewRequest("GET", langURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req2.Header.Set("User-Agent", anidbUserAgent)
	req2.Header.Set("Referer", anidbBaseURL+"/")

	resp2, err := httpClient().Do(req2)
	if err != nil {
		return nil, nil, err
	}
	defer resp2.Body.Close()

	body2, err := io.ReadAll(resp2.Body)
	if err != nil {
		return nil, nil, err
	}
	if !curdhost.HTTPStatusOK(resp2.StatusCode) {
		return nil, nil, curdhost.HTTPStatusError("anidb languages", resp2.StatusCode, body2)
	}

	var langRes anidbLanguagesResponse
	if err := json.Unmarshal(body2, &langRes); err != nil {
		return nil, nil, fmt.Errorf("parse anidb languages response: %w", err)
	}
	if len(langRes.Languages) == 0 {
		return nil, nil, fmt.Errorf("no language embeds found for episode %d", epNo)
	}

	normalizedMode := providers.NormalizeTranslationType(mode)
	wantCode := "jpn"
	if normalizedMode == "dub" {
		wantCode = "eng"
	}

	var embedURL string
	for _, l := range langRes.Languages {
		if strings.EqualFold(l.Code, wantCode) {
			embedURL = l.EmbedURL
			break
		}
	}
	if embedURL == "" {
		embedURL = langRes.Languages[0].EmbedURL
	}

	// 3. Fetch embed page to extract master m3u8 playlist URL
	req3, err := http.NewRequest("GET", embedURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req3.Header.Set("User-Agent", anidbUserAgent)
	req3.Header.Set("Referer", anidbBaseURL+"/")

	resp3, err := httpClient().Do(req3)
	if err != nil {
		return nil, nil, err
	}
	defer resp3.Body.Close()

	body3, err := io.ReadAll(resp3.Body)
	if err != nil {
		return nil, nil, err
	}
	if !curdhost.HTTPStatusOK(resp3.StatusCode) {
		return nil, nil, curdhost.HTTPStatusError("anidb embed", resp3.StatusCode, body3)
	}

	match := embedMasterPattern.FindStringSubmatch(string(body3))
	if len(match) < 2 {
		return nil, nil, fmt.Errorf("failed to extract master m3u8 from embed page %s", embedURL)
	}

	masterURL := strings.TrimSpace(match[1])

	links := []string{masterURL}
	hints := map[string]providers.StreamPlaybackHint{
		masterURL: {Referrer: anidbBaseURL + "/"},
	}

	return links, hints, nil
}
