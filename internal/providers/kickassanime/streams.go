package kickassanime

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/wraient/curd/internal/providers"
)

type watchResponse struct {
	Title   string `json:"title"`
	Servers []struct {
		Name      string `json:"name"`
		ShortName string `json:"shortName"`
		Src       string `json:"src"`
	} `json:"servers"`
}

var (
	manifestRE = regexp.MustCompile(`(?:https?:)?//[^\s"'<>\\]+\.(?:m3u8|mpd)`)
	subRE      = regexp.MustCompile(`(?:language|name)[\"':\[\s,0]+([a-zA-Z\-]+)[\"':\],]+(?:[^\}]*?)src[\"':\[\s,0]+((?:https?:)?//[^\s"'<>\\]+\.(?:vtt|srt|ass))`)
	allSubsRE  = regexp.MustCompile(`((?:https?:)?//[^\s"'<>\\]+\.(?:vtt|srt|ass))`)
)

func getEpisodeStreamsForMode(id string, config providers.PlaybackConfig, epNo int, mode string) ([]string, map[string]providers.StreamPlaybackHint, error) {
	slug := strings.TrimSpace(id)
	if slug == "" {
		return nil, nil, fmt.Errorf("empty show id")
	}

	normalizedMode := providers.NormalizeTranslationType(mode)
	lang := "ja-JP"
	if normalizedMode == "dub" {
		lang = "en-US"
	}

	// 1. Fetch page 1 to locate episode or determine target page
	epURL := fmt.Sprintf("%s/api/show/%s/episodes?lang=%s&page=1", baseURL, slug, lang)
	body, err := fetchBytes(epURL, map[string]string{"Referer": baseURL + "/"})
	if err != nil {
		return nil, nil, fmt.Errorf("kickassanime episodes: %w", err)
	}

	var resp episodesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("parse kickassanime episodes response: %w", err)
	}

	targetPage := 1
	if len(resp.Pages) > 0 {
		for _, p := range resp.Pages {
			for _, epRaw := range p.Eps {
				var epNumVal float64
				switch v := epRaw.(type) {
				case float64:
					epNumVal = v
				case string:
					epNumVal, _ = strconv.ParseFloat(v, 64)
				case json.Number:
					epNumVal, _ = v.Float64()
				}
				if epNumVal == float64(epNo) {
					targetPage = p.Number
					break
				}
			}
			if targetPage != 1 {
				break
			}
		}
	}

	if targetPage != 1 {
		epURL = fmt.Sprintf("%s/api/show/%s/episodes?lang=%s&page=%d", baseURL, slug, lang, targetPage)
		body, err = fetchBytes(epURL, map[string]string{"Referer": baseURL + "/"})
		if err != nil {
			return nil, nil, fmt.Errorf("kickassanime episodes page %d: %w", targetPage, err)
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, nil, fmt.Errorf("parse kickassanime episodes page %d: %w", targetPage, err)
		}
	}

	var epSlug string
	var epStr string
	for _, ep := range resp.Result {
		epNumVal, _ := ep.EpisodeNumber.Float64()
		if int(epNumVal) == epNo || ep.EpisodeString == fmt.Sprintf("%d", epNo) {
			epSlug = ep.Slug
			epStr = ep.EpisodeString
			if epStr == "" {
				epStr = fmt.Sprintf("%d", epNo)
			}
			break
		}
	}

	if epSlug == "" {
		return nil, nil, fmt.Errorf("episode %d not found for show %q", epNo, slug)
	}

	// 2. Fetch watch data for episode
	watchURL := fmt.Sprintf("%s/api/show/%s/episode/ep-%s-%s", baseURL, slug, epStr, epSlug)
	watchBody, err := fetchBytes(watchURL, map[string]string{"Referer": baseURL + "/"})
	if err != nil {
		return nil, nil, fmt.Errorf("kickassanime watch data: %w", err)
	}

	var watch watchResponse
	if err := json.Unmarshal(watchBody, &watch); err != nil {
		return nil, nil, fmt.Errorf("parse kickassanime watch response: %w", err)
	}

	if len(watch.Servers) == 0 {
		return nil, nil, fmt.Errorf("no streaming servers found for %s ep %d", slug, epNo)
	}

	links := make([]string, 0, len(watch.Servers))
	hints := make(map[string]providers.StreamPlaybackHint)

	for _, s := range watch.Servers {
		manifestURL, subs, ref, err := extractStreamsFromPlayer(s.Src)
		if err != nil {
			continue
		}

		var selectedSub string
		for langKey, subURL := range subs {
			if strings.HasPrefix(langKey, "en") || strings.Contains(langKey, "eng") {
				selectedSub = subURL
				break
			}
		}
		if selectedSub == "" && len(subs) > 0 {
			for _, subURL := range subs {
				selectedSub = subURL
				break
			}
		}

		links = append(links, manifestURL)
		hints[manifestURL] = providers.StreamPlaybackHint{
			Referrer: ref,
			Subtitle: selectedSub,
		}
	}

	if len(links) == 0 {
		return nil, nil, fmt.Errorf("failed to extract playable stream for %s ep %d", slug, epNo)
	}

	return links, hints, nil
}

func extractStreamsFromPlayer(playerURL string) (string, map[string]string, string, error) {
	parsedURL, err := url.Parse(playerURL)
	if err != nil {
		return "", nil, "", err
	}
	embedOrigin := fmt.Sprintf("%s://%s/", parsedURL.Scheme, parsedURL.Host)

	body, err := fetchBytes(playerURL, map[string]string{
		"Referer": baseURL + "/",
	})
	if err != nil {
		return "", nil, "", err
	}

	decoded := html.UnescapeString(string(body))

	manifestMatch := manifestRE.FindString(decoded)
	if manifestMatch == "" {
		return "", nil, "", fmt.Errorf("no stream manifest found in player %s", playerURL)
	}
	manifestURL := sanitizeMediaURL(manifestMatch)

	subs := make(map[string]string)
	for _, m := range subRE.FindAllStringSubmatch(decoded, -1) {
		lang := strings.ToLower(m[1])
		subURL := sanitizeMediaURL(m[2])
		subs[lang] = subURL
	}

	if len(subs) == 0 {
		for _, m := range allSubsRE.FindAllStringSubmatch(decoded, -1) {
			u := sanitizeMediaURL(m[1])
			if strings.Contains(u, "preview") || strings.Contains(u, "thumbnail") {
				continue
			}
			subs["en"] = u
		}
	}

	return manifestURL, subs, embedOrigin, nil
}
