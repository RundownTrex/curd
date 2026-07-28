package senshi

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/wraient/curd/internal/providers"
)

func getEpisodeStreamsForMode(malIDStr string, config providers.PlaybackConfig, epNo int) ([]string, map[string]providers.StreamPlaybackHint, error) {
	malID, err := parseMalID(malIDStr)
	if err != nil {
		return nil, nil, err
	}
	if epNo <= 0 {
		return nil, nil, fmt.Errorf("invalid episode number %d", epNo)
	}

	mode := providers.NormalizeTranslationType(config.SubOrDub)

	var embeds []embedItem
	reqURL := fmt.Sprintf("%s/episode-embeds/%d/%d", baseURL, malID, epNo)
	if err := fetchJSON(http.MethodGet, reqURL, nil, &embeds); err != nil {
		return nil, nil, err
	}
	if len(embeds) == 0 {
		return nil, nil, fmt.Errorf("no streams found for episode %d", epNo)
	}

	var chosenItem *embedItem
	for i := range embeds {
		item := &embeds[i]
		status := strings.TrimSpace(item.Status)
		if mode == "dub" {
			if strings.EqualFold(status, "Dub") {
				chosenItem = item
				break
			}
		} else {
			if !strings.EqualFold(status, "Dub") {
				chosenItem = item
				break
			}
		}
	}
	if chosenItem == nil {
		// Fallback to first available embed with non-empty URL
		for i := range embeds {
			if strings.TrimSpace(embeds[i].URL) != "" {
				chosenItem = &embeds[i]
				break
			}
		}
	}
	if chosenItem == nil || strings.TrimSpace(chosenItem.URL) == "" {
		return nil, nil, fmt.Errorf("no %s streams found for episode %d", mode, epNo)
	}

	streamURL := strings.TrimSpace(chosenItem.URL)
	finalURL := streamURL
	if proxiedURL, err := registerSenshiStream(streamURL, baseURL+"/"); err == nil {
		finalURL = proxiedURL
	}

	hints := map[string]providers.StreamPlaybackHint{
		finalURL: {
			Referrer: baseURL + "/",
		},
	}

	if chosenItem.ServerFM != nil {
		if u, err := url.Parse(*chosenItem.ServerFM); err == nil {
			subInfoURL := u.Query().Get("sub.info")
			if subInfoURL != "" {
				var subs []subtitleItem
				if err := fetchJSON(http.MethodGet, subInfoURL, nil, &subs); err == nil {
					for _, sub := range subs {
						// Prefer English subtitles
						if strings.Contains(strings.ToLower(sub.Label), "eng") || sub.Default {
							hint := hints[finalURL]
							hint.Subtitle = sub.Src
							hints[finalURL] = hint
							break
						}
					}
				}
			}
		}
	}

	return []string{finalURL}, hints, nil
}
