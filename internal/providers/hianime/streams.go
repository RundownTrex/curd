package hianime

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/wraient/curd/internal/providers"
)

var (
	serverItemRE = regexp.MustCompile(`(?s)<div\b([^>]*)class=["'][^"']*server-item[^"']*["']([^>]*)>`)
	serverTypeRE = regexp.MustCompile(`data-type=["']([^"']+)["']`)
	serverNameRE = regexp.MustCompile(`data-server-name=["']([^"']+)["']`)
	serverHashRE = regexp.MustCompile(`data-hash=["']([^"']+)["']`)
	windowPRE    = regexp.MustCompile(`window\.__P\s*=\s*["']([^"']+)["']`)
)

func fetchEpisodeServers(episodeID string) ([]episodeServer, error) {
	rawURL := fmt.Sprintf("%s/api/theme/episode/servers?episodeId=%s", baseURL, url.QueryEscape(episodeID))
	var resp serversResponse
	if err := fetchJSON(rawURL, baseURL+"/", &resp); err != nil {
		return nil, fmt.Errorf("fetch hianime episode servers: %w", err)
	}

	if !resp.Status {
		return nil, fmt.Errorf("hianime servers response returned status false for ep %s", episodeID)
	}

	matches := serverItemRE.FindAllStringSubmatch(resp.HTML, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no server items found in hianime response for ep %s", episodeID)
	}

	servers := make([]episodeServer, 0, len(matches))
	for _, m := range matches {
		attrs := m[1] + " " + m[2]

		tMatch := serverTypeRE.FindStringSubmatch(attrs)
		nMatch := serverNameRE.FindStringSubmatch(attrs)
		hMatch := serverHashRE.FindStringSubmatch(attrs)
		if len(tMatch) < 2 || len(nMatch) < 2 || len(hMatch) < 2 {
			continue
		}

		sType := strings.TrimSpace(tMatch[1])
		sName := strings.TrimSpace(nMatch[1])
		sHash := strings.TrimSpace(hMatch[1])

		decodedBytes, err := decodeBase64Safe(sHash)
		if err != nil {
			continue
		}
		decodedURL := strings.TrimSpace(string(decodedBytes))

		servers = append(servers, episodeServer{
			Type:       sType,
			Name:       sName,
			Hash:       sHash,
			DecodedURL: decodedURL,
		})
	}

	return servers, nil
}

func originFromURL(rawURL, defaultOrigin string) string {
	u, err := url.Parse(rawURL)
	if err == nil && u.Scheme != "" && u.Host != "" {
		return fmt.Sprintf("%s://%s/", u.Scheme, u.Host)
	}
	return defaultOrigin
}

func resolveZokoStream(embedURL, mode string) (string, providers.StreamPlaybackHint, error) {
	embedHTML, err := fetchString(embedURL, baseURL+"/")
	if err != nil {
		return "", providers.StreamPlaybackHint{}, fmt.Errorf("fetch zoko embed: %w", err)
	}

	match := windowPRE.FindStringSubmatch(embedHTML)
	if len(match) < 2 {
		return "", providers.StreamPlaybackHint{}, fmt.Errorf("window.__P blob not found in zoko embed")
	}

	plainJSON, err := deobfuscateBlob(match[1])
	if err != nil {
		return "", providers.StreamPlaybackHint{}, fmt.Errorf("deobfuscate zoko blob: %w", err)
	}

	var payload zokoPayload
	if err := json.Unmarshal(plainJSON, &payload); err != nil {
		return "", providers.StreamPlaybackHint{}, fmt.Errorf("parse zoko json: %w", err)
	}

	streamURL := strings.TrimSpace(payload.Src)
	if streamURL == "" {
		return "", providers.StreamPlaybackHint{}, fmt.Errorf("empty stream url in zoko payload")
	}

	subtitleURL := pickSubtitleTrack(payload.Subtitles, mode)
	referrer := originFromURL(embedURL, "https://zokoanime.video/")

	hint := providers.StreamPlaybackHint{
		Referrer: referrer,
		Subtitle: subtitleURL,
	}

	return streamURL, hint, nil
}

func pickSubtitleTrack(tracks []zokoSubtitle, mode string) string {
	if strings.EqualFold(mode, "dub") {
		return ""
	}

	var (
		firstTrack   string
		defaultTrack string
		englishTrack string
	)

	for _, track := range tracks {
		src := strings.TrimSpace(track.Src)
		if src == "" {
			continue
		}
		if firstTrack == "" {
			firstTrack = src
		}
		if track.Default {
			defaultTrack = src
		}

		label := strings.ToLower(track.Label)
		lang := strings.ToLower(track.Lang)
		if strings.Contains(label, "english") || lang == "en" || strings.Contains(lang, "eng") {
			if track.Default {
				return src
			}
			if englishTrack == "" {
				englishTrack = src
			}
		}
	}

	if englishTrack != "" {
		return englishTrack
	}
	if defaultTrack != "" {
		return defaultTrack
	}
	return firstTrack
}

func getEpisodeStreamsForMode(showID string, config providers.PlaybackConfig, epNo int) ([]string, map[string]providers.StreamPlaybackHint, error) {
	showID = strings.TrimSpace(showID)
	if showID == "" {
		return nil, nil, fmt.Errorf("empty show id")
	}
	if epNo <= 0 {
		return nil, nil, fmt.Errorf("invalid episode number %d", epNo)
	}

	mode := providers.NormalizeTranslationType(config.SubOrDub)
	if mode == "" {
		mode = "sub"
	}

	entries, err := fetchEpisodeEntries(showID)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch episodes for %s: %w", showID, err)
	}

	var targetEpisode *episodeEntry
	for i := range entries {
		if matchEpisodeNumber(entries[i].Number, epNo) {
			targetEpisode = &entries[i]
			break
		}
	}

	if targetEpisode == nil {
		return nil, nil, fmt.Errorf("episode %d not found for %s", epNo, showID)
	}

	servers, err := fetchEpisodeServers(targetEpisode.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch servers for episode %d (id %s): %w", epNo, targetEpisode.ID, err)
	}

	// Filter by mode (sub / dub)
	var modeServers []episodeServer
	for _, s := range servers {
		if strings.EqualFold(s.Type, mode) {
			modeServers = append(modeServers, s)
		}
	}

	if len(modeServers) == 0 {
		return nil, nil, fmt.Errorf("no %s servers found for episode %d", mode, epNo)
	}

	// Priority: ZokoAnime first, then any other servers
	orderedServers := make([]episodeServer, 0, len(modeServers))
	for _, s := range modeServers {
		if strings.EqualFold(s.Name, "zokoanime") {
			orderedServers = append(orderedServers, s)
		}
	}
	for _, s := range modeServers {
		if !strings.EqualFold(s.Name, "zokoanime") {
			orderedServers = append(orderedServers, s)
		}
	}

	var (
		allLinks []string
		allHints = make(map[string]providers.StreamPlaybackHint)
		lastErr  error
	)

	for _, s := range orderedServers {
		if s.DecodedURL == "" {
			continue
		}

		if strings.EqualFold(s.Name, "zokoanime") || strings.Contains(s.DecodedURL, "zokoanime.") {
			streamURL, hint, err := resolveZokoStream(s.DecodedURL, mode)
			if err != nil {
				lastErr = err
				continue
			}
			if streamURL != "" && allHints[streamURL].Referrer == "" {
				allLinks = append(allLinks, streamURL)
				allHints[streamURL] = hint
				// Found primary working stream
				break
			}
		}
	}

	if len(allLinks) == 0 {
		if lastErr != nil {
			return nil, nil, fmt.Errorf("hianime stream resolution failed for episode %d (%s): %w", epNo, mode, lastErr)
		}
		return nil, nil, fmt.Errorf("no playable %s streams found for episode %d on hianime", mode, epNo)
	}

	return allLinks, allHints, nil
}
