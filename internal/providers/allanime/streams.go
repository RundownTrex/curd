package allanime

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

var anidbEmbedMasterPattern = regexp.MustCompile(`file:\s*[\x27\x22]([^\x27\x22]+master\.m3u8[^\x27\x22]*)[\x27\x22]`)

func getAllanimeEpisodeStreamsForMode(id, mode string, epNo int) ([]string, map[string]providers.StreamPlaybackHint, error) {
	numericID := extractNumericID(id)
	if numericID == "" {
		return nil, nil, fmt.Errorf("invalid show id %q", id)
	}

	// 1. Fetch episodes list to find internal ep ID
	episodesURL := fmt.Sprintf("%s/api/frontend/anime/%s/episodes", anidbBaseURL, numericID)
	req1, err := http.NewRequest("GET", episodesURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req1.Header.Set("User-Agent", anidbUserAgent)
	req1.Header.Set("Referer", anidbBaseURL+"/")

	resp1, err := httpClient().Do(req1)
	if err != nil {
		return nil, nil, err
	}
	defer resp1.Body.Close()

	body1, err := io.ReadAll(resp1.Body)
	if err != nil {
		return nil, nil, err
	}
	if !curdhost.HTTPStatusOK(resp1.StatusCode) {
		return nil, nil, curdhost.HTTPStatusError("anidb episodes", resp1.StatusCode, body1)
	}

	var epRes anidbEpisodesResponse
	if err := json.Unmarshal(body1, &epRes); err != nil {
		return nil, nil, err
	}

	var targetEpID int
	for _, ep := range epRes.Episodes {
		if ep.Number == epNo {
			targetEpID = ep.ID
			break
		}
	}
	if targetEpID == 0 {
		return nil, nil, fmt.Errorf("episode %d not found for anime %s", epNo, id)
	}

	// 2. Fetch languages for the episode
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
		return nil, nil, err
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

	// 3. Fetch embed page to extract master.m3u8
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

	embedHTML := string(body3)
	match := anidbEmbedMasterPattern.FindStringSubmatch(embedHTML)
	if len(match) < 2 {
		return nil, nil, fmt.Errorf("failed to extract master.m3u8 from embed page %s", embedURL)
	}

	masterURL := match[1]

	// 4. Fetch master.m3u8 playlist to extract quality variant URLs
	req4, err := http.NewRequest("GET", masterURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req4.Header.Set("User-Agent", anidbUserAgent)
	req4.Header.Set("Referer", anidbBaseURL+"/")

	resp4, err := httpClient().Do(req4)
	if err != nil {
		return nil, nil, err
	}
	defer resp4.Body.Close()

	body4, err := io.ReadAll(resp4.Body)
	if err != nil {
		return nil, nil, err
	}

	links := []string{masterURL}
	hints := map[string]providers.StreamPlaybackHint{
		masterURL: {Referrer: anidbBaseURL + "/"},
	}

	if curdhost.HTTPStatusOK(resp4.StatusCode) {
		lines := strings.Split(string(body4), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
				links = append(links, line)
				hints[line] = providers.StreamPlaybackHint{Referrer: anidbBaseURL + "/"}
			}
		}
	}

	return links, hints, nil
}
