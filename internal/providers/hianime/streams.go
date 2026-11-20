package hianime

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/wraient/curd/internal/providers"
)

var (
	serverItemRE     = regexp.MustCompile(`(?s)<div\b([^>]*)class=["'][^"']*server-item[^"']*["']([^>]*)>`)
	serverTypeRE     = regexp.MustCompile(`data-type=["']([^"']+)["']`)
	serverNameRE     = regexp.MustCompile(`data-server-name=["']([^"']+)["']`)
	serverHashRE     = regexp.MustCompile(`data-hash=["']([^"']+)["']`)
	windowPRE        = regexp.MustCompile(`window\.__P\s*=\s*["']([^"']+)["']`)
	megaplayDataIDRE = regexp.MustCompile(`data-id="(\d+)"`)
)

const (
	megaplayBaseURL = "https://megaplay.buzz"
	megaplayAESKey  = "i?LMTAx0Q6,:}50U"
	megaplayAESIV   = "W0;27ToaUpl_P%'c"
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
		return "", providers.StreamPlaybackHint{}, fmt.Errorf("window.__P blob not found in zoko embed (html len %d, preview: %s)", len(embedHTML), strings.ReplaceAll(embedHTML[:min(len(embedHTML), 300)], "\n", " "))
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
	if strings.HasPrefix(streamURL, "//") {
		streamURL = "https:" + streamURL
	} else if strings.HasPrefix(streamURL, "/") {
		if u, err := url.Parse(embedURL); err == nil && u.Scheme != "" && u.Host != "" {
			streamURL = fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, streamURL)
		}
	}

	subtitleURL := pickSubtitleTrack(payload.Subtitles, mode)
	if strings.HasPrefix(subtitleURL, "//") {
		subtitleURL = "https:" + subtitleURL
	} else if strings.HasPrefix(subtitleURL, "/") {
		if u, err := url.Parse(streamURL); err == nil && u.Scheme != "" && u.Host != "" {
			subtitleURL = fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, subtitleURL)
		} else if u, err := url.Parse(embedURL); err == nil && u.Scheme != "" && u.Host != "" {
			subtitleURL = fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, subtitleURL)
		}
	}
	referrer := originFromURL(embedURL, "https://zokoanime.video/")

	hint := providers.StreamPlaybackHint{
		Referrer: referrer,
		Subtitle: subtitleURL,
	}

	return streamURL, hint, nil
}

type megaplaySourceTrack struct {
	File  string `json:"file"`
	Src   string `json:"src"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Type  string `json:"type"`
}

type megaplaySourcesPayload struct {
	SourcesRaw any                   `json:"sources"`
	Enc        string                `json:"enc"`
	Tracks     []megaplaySourceTrack `json:"tracks"`
	Captions   []megaplaySourceTrack `json:"captions"`
	Subtitles  []megaplaySourceTrack `json:"subtitles"`
}

func decryptMegaPlayEnc(enc string) (string, error) {
	enc = strings.TrimSpace(enc)
	if enc == "" {
		return "", fmt.Errorf("empty enc payload")
	}

	ciphertext, err := decodeBase64Safe(enc)
	if err != nil {
		return "", fmt.Errorf("decode base64 enc: %w", err)
	}

	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid ciphertext length %d (must be multiple of %d)", len(ciphertext), aes.BlockSize)
	}

	keyBytes := make([]byte, 32)
	copy(keyBytes, []byte(megaplayAESKey))

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", fmt.Errorf("create AES cipher: %w", err)
	}

	mode := cipher.NewCBCDecrypter(block, []byte(megaplayAESIV))
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	// PKCS#7 unpadding
	if len(plaintext) == 0 {
		return "", fmt.Errorf("empty decrypted plaintext")
	}
	padLen := int(plaintext[len(plaintext)-1])
	if padLen > 0 && padLen <= aes.BlockSize && padLen <= len(plaintext) {
		valid := true
		for i := len(plaintext) - padLen; i < len(plaintext); i++ {
			if int(plaintext[i]) != padLen {
				valid = false
				break
			}
		}
		if valid {
			plaintext = plaintext[:len(plaintext)-padLen]
		}
	}

	var singleObj struct {
		File string `json:"file"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(plaintext, &singleObj); err == nil {
		if f := strings.TrimSpace(singleObj.File); f != "" {
			return f, nil
		}
		if u := strings.TrimSpace(singleObj.URL); u != "" {
			return u, nil
		}
	}

	var arr []struct {
		File string `json:"file"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(plaintext, &arr); err == nil && len(arr) > 0 {
		if f := strings.TrimSpace(arr[0].File); f != "" {
			return f, nil
		}
		if u := strings.TrimSpace(arr[0].URL); u != "" {
			return u, nil
		}
	}

	return "", fmt.Errorf("failed to extract stream URL from decrypted payload")
}

func resolveMegaplayStream(embedURL, mode string) (string, providers.StreamPlaybackHint, error) {
	html, err := fetchString(embedURL, baseURL+"/")
	if err != nil {
		return "", providers.StreamPlaybackHint{}, fmt.Errorf("fetch megaplay embed: %w", err)
	}

	matches := megaplayDataIDRE.FindStringSubmatch(html)
	if len(matches) < 2 {
		return "", providers.StreamPlaybackHint{}, fmt.Errorf("data-id not found in megaplay embed")
	}
	dataID := matches[1]

	sourcesURL := fmt.Sprintf("%s/stream/getSources?id=%s", megaplayBaseURL, dataID)
	var payload megaplaySourcesPayload
	if err := fetchJSON(sourcesURL, megaplayBaseURL+"/", &payload); err != nil {
		return "", providers.StreamPlaybackHint{}, fmt.Errorf("fetch megaplay sources: %w", err)
	}

	var streamURL string
	switch v := payload.SourcesRaw.(type) {
	case string:
		streamURL = strings.TrimSpace(v)
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if f, ok := m["file"].(string); ok && strings.TrimSpace(f) != "" {
					streamURL = strings.TrimSpace(f)
					break
				}
			}
		}
	}

	if streamURL == "" && payload.Enc != "" {
		decURL, decErr := decryptMegaPlayEnc(payload.Enc)
		if decErr != nil {
			return "", providers.StreamPlaybackHint{}, fmt.Errorf("decrypt megaplay enc: %w", decErr)
		}
		streamURL = decURL
	}

	if streamURL == "" {
		return "", providers.StreamPlaybackHint{}, fmt.Errorf("empty stream url in megaplay payload")
	}

	var subtitleURL string
	if !strings.EqualFold(mode, "dub") {
		var allTracks []megaplaySourceTrack
		allTracks = append(allTracks, payload.Tracks...)
		allTracks = append(allTracks, payload.Captions...)
		allTracks = append(allTracks, payload.Subtitles...)

		for _, t := range allTracks {
			f := strings.TrimSpace(t.File)
			if f == "" {
				f = strings.TrimSpace(t.Src)
			}
			if f == "" {
				continue
			}
			label := strings.ToLower(t.Label)
			kind := strings.ToLower(t.Kind + " " + t.Type)
			if strings.Contains(kind, "sign") || strings.Contains(kind, "song") {
				continue
			}
			if strings.Contains(label, "english") || strings.Contains(label, "eng") {
				subtitleURL = f
				break
			}
			if subtitleURL == "" {
				subtitleURL = f
			}
		}
	}

	hint := providers.StreamPlaybackHint{
		Referrer: megaplayBaseURL + "/",
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

	// Priority: ZokoAnime first, then MegaPlay/HD-1, then any other servers
	orderedServers := make([]episodeServer, 0, len(modeServers))
	for _, s := range modeServers {
		if strings.Contains(s.DecodedURL, "zokoanime.") || strings.EqualFold(s.Name, "zokoanime") {
			orderedServers = append(orderedServers, s)
		}
	}
	for _, s := range modeServers {
		if strings.Contains(s.DecodedURL, "megaplay.") && !strings.Contains(s.DecodedURL, "zokoanime.") {
			orderedServers = append(orderedServers, s)
		}
	}
	for _, s := range modeServers {
		if !strings.Contains(s.DecodedURL, "zokoanime.") && !strings.EqualFold(s.Name, "zokoanime") && !strings.Contains(s.DecodedURL, "megaplay.") {
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

		if strings.Contains(s.DecodedURL, "zokoanime.") || strings.EqualFold(s.Name, "zokoanime") {
			streamURL, hint, err := resolveZokoStream(s.DecodedURL, mode)
			if err != nil {
				time.Sleep(300 * time.Millisecond)
				streamURL, hint, err = resolveZokoStream(s.DecodedURL, mode)
			}
			if err != nil {
				lastErr = fmt.Errorf("zoko server (%s) failed: %w", s.Name, err)
				continue
			}
			if streamURL != "" && allHints[streamURL].Referrer == "" {
				allLinks = append(allLinks, streamURL)
				allHints[streamURL] = hint
				// Found primary working stream
				break
			}
		} else if strings.Contains(s.DecodedURL, "megaplay.") {
			streamURL, hint, err := resolveMegaplayStream(s.DecodedURL, mode)
			if err != nil {
				time.Sleep(300 * time.Millisecond)
				streamURL, hint, err = resolveMegaplayStream(s.DecodedURL, mode)
			}
			if err != nil {
				lastErr = fmt.Errorf("megaplay server (%s) failed: %w", s.Name, err)
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
