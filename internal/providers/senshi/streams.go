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
	wantStatus := "HardSub"
	if mode == "dub" {
		wantStatus = "Dub"
	}

	var embeds []embedItem
	reqURL := fmt.Sprintf("%s/episode-embeds/%d/%d", baseURL, malID, epNo)
	if err := fetchJSON(http.MethodGet, reqURL, nil, &embeds); err != nil {
		return nil, nil, err
	}
	if len(embeds) == 0 {
		return nil, nil, fmt.Errorf("no streams found for episode %d", epNo)
	}

	for _, item := range embeds {
		if !strings.EqualFold(strings.TrimSpace(item.Status), wantStatus) {
			continue
		}
		streamURL := strings.TrimSpace(item.URL)
		if streamURL == "" {
			continue
		}
		hints := map[string]providers.StreamPlaybackHint{
			streamURL: {
				Referrer: baseURL + "/",
			},
		}

		if item.ServerFM != nil {
			if u, err := url.Parse(*item.ServerFM); err == nil {
				subInfoURL := u.Query().Get("sub.info")
				if subInfoURL != "" {
					var subs []subtitleItem
					if err := fetchJSON(http.MethodGet, subInfoURL, nil, &subs); err == nil {
						for _, sub := range subs {
							// Prefer English subtitles
							if strings.Contains(strings.ToLower(sub.Label), "eng") || sub.Default {
								hint := hints[streamURL]
								hint.Subtitle = sub.Src
								hints[streamURL] = hint
								break
							}
						}
					}
				}
			}
		}

		return []string{streamURL}, hints, nil
	}

	return nil, nil, fmt.Errorf("no %s streams found for episode %d", mode, epNo)
}
