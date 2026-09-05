package kickassanime

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/wraient/curd/internal/providers"
)

type episodesResponse struct {
	CurrentPage int `json:"current_page"`
	Pages       []struct {
		Number int           `json:"number"`
		From   string        `json:"from"`
		To     string        `json:"to"`
		Eps    []interface{} `json:"eps"`
	} `json:"pages"`
	Result []struct {
		EpisodeNumber json.Number `json:"episode_number"`
		EpisodeString string      `json:"episode_string"`
		Slug          string      `json:"slug"`
		Title         string      `json:"title"`
	} `json:"result"`
}

func episodesList(showID, mode string) ([]string, error) {
	slug := strings.TrimSpace(showID)
	if slug == "" {
		return nil, fmt.Errorf("empty show id")
	}

	normalizedMode := providers.NormalizeTranslationType(mode)
	lang := "ja-JP"
	if normalizedMode == "dub" {
		lang = "en-US"
	}

	epURL := fmt.Sprintf("%s/api/show/%s/episodes?lang=%s&page=1", baseURL, slug, lang)
	body, err := fetchBytes(epURL, map[string]string{"Referer": baseURL + "/"})
	if err != nil {
		return nil, fmt.Errorf("kickassanime episodes list: %w", err)
	}

	var resp episodesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse kickassanime episodes response: %w", err)
	}

	seen := make(map[string]struct{})
	var epList []string

	addEp := func(epNumStr string) {
		epNumStr = strings.TrimSpace(epNumStr)
		if epNumStr == "" {
			return
		}
		if _, exists := seen[epNumStr]; !exists {
			seen[epNumStr] = struct{}{}
			epList = append(epList, epNumStr)
		}
	}

	if len(resp.Pages) > 0 {
		for _, page := range resp.Pages {
			for _, epRaw := range page.Eps {
				switch v := epRaw.(type) {
				case float64:
					if v == float64(int(v)) {
						addEp(fmt.Sprintf("%d", int(v)))
					} else {
						addEp(fmt.Sprintf("%v", v))
					}
				case string:
					addEp(v)
				case json.Number:
					addEp(v.String())
				}
			}
		}
	}

	if len(epList) == 0 {
		for _, r := range resp.Result {
			if r.EpisodeString != "" {
				addEp(r.EpisodeString)
			} else if r.EpisodeNumber.String() != "" {
				addEp(r.EpisodeNumber.String())
			}
		}
	}

	if len(epList) == 0 {
		return nil, fmt.Errorf("no episodes found for show %q", slug)
	}

	sort.SliceStable(epList, func(i, j int) bool {
		numI, errI := strconv.ParseFloat(epList[i], 64)
		numJ, errJ := strconv.ParseFloat(epList[j], 64)
		if errI == nil && errJ == nil {
			return numI < numJ
		}
		return epList[i] < epList[j]
	})

	return epList, nil
}
