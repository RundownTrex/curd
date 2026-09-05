package anineko

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/wraient/curd/internal/curdhost"
)

var (
	bibiembMasterRE = regexp.MustCompile(`const src = "(https?://[^"]+/master\.m3u8)"`)
	streamInfRE     = regexp.MustCompile(`(?m)^#EXT-X-STREAM-INF:.*NAME="([^"]+)".*\r?\n([^\r\n#]+)`)
)

var bibiembQualityOrder = []string{"1080p", "720p", "480p", "360p"}

type resolvedStream struct {
	URL      string
	Referrer string
	Subtitle string
}

func resolveBibiemb(embedURL string) (resolvedStream, error) {
	embedURL = strings.TrimSpace(embedURL)
	if embedURL == "" {
		return resolvedStream{}, fmt.Errorf("empty bibiemb embed url")
	}

	html, err := fetchString(embedURL, baseURL+"/")
	if err != nil {
		return resolvedStream{}, err
	}

	match := bibiembMasterRE.FindStringSubmatch(html)
	if len(match) < 2 {
		return resolvedStream{}, fmt.Errorf("bibiemb master m3u8 not found")
	}

	masterURL := match[1]
	variantURL, err := pickBibiembVariant(masterURL, embedURL)
	if err != nil {
		return resolvedStream{}, err
	}

	return resolvedStream{
		URL:      variantURL,
		Referrer: embedURL,
		Subtitle: resolveSubtitle(embedURL, html),
	}, nil
}

func extractFirstSegmentURL(playlistURL, playlistBody string) string {
	lines := strings.Split(playlistBody, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return resolvePlaylistURL(playlistURL, line)
	}
	return ""
}

func fetchSegmentSample(segURL, referer string) ([]byte, error) {
	req, err := newRequest(http.MethodGet, segURL, referer)
	if err != nil {
		return nil, err
	}
	client := curdhost.HTTPClient()
	if client == nil {
		client = &http.Client{}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if !curdhost.HTTPStatusOK(resp.StatusCode) {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	buf := make([]byte, 512)
	n, _ := io.ReadFull(resp.Body, buf)
	if n == 0 {
		return nil, fmt.Errorf("empty segment")
	}
	return buf[:n], nil
}

func isPlayableHLS(streamURL, referer string) bool {
	body, err := fetchString(streamURL, referer)
	if err != nil {
		return false
	}
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "#EXTM3U") || strings.Contains(trimmed, "Backblaze Error") || strings.Contains(trimmed, "too_many_requests") {
		return false
	}

	// Validate the first video segment
	firstSegment := extractFirstSegmentURL(streamURL, body)
	if firstSegment != "" {
		segData, err := fetchSegmentSample(firstSegment, referer)
		if err != nil || len(segData) == 0 {
			return false
		}
		// A valid MPEG-TS segment must start with the sync byte 0x47 ('G')
		// and cannot be a Backblaze error message.
		if segData[0] != 0x47 || strings.Contains(string(segData), "Backblaze Error") {
			return false
		}
	}

	return true
}

func pickBibiembVariant(masterURL, referer string) (string, error) {
	playlist, err := fetchString(masterURL, referer)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(strings.TrimSpace(playlist), "#EXTM3U") {
		return "", fmt.Errorf("master playlist is not valid HLS")
	}

	type variant struct {
		name string
		url  string
	}
	variants := make([]variant, 0)
	for _, match := range streamInfRE.FindAllStringSubmatch(playlist, -1) {
		if len(match) < 3 {
			continue
		}
		variants = append(variants, variant{
			name: strings.TrimSpace(match[1]),
			url:  resolvePlaylistURL(masterURL, strings.TrimSpace(match[2])),
		})
	}
	if len(variants) == 0 {
		return "", fmt.Errorf("no bibiemb variants in master playlist")
	}

	byName := map[string]string{}
	for _, item := range variants {
		byName[strings.ToLower(item.name)] = item.url
	}
	for _, quality := range bibiembQualityOrder {
		if streamURL, ok := byName[quality]; ok {
			if isPlayableHLS(streamURL, referer) {
				return streamURL, nil
			}
		}
	}
	for _, item := range variants {
		if isPlayableHLS(item.url, referer) {
			return item.url, nil
		}
	}
	return "", fmt.Errorf("no playable bibiemb variants in master playlist")
}

func resolvePlaylistURL(baseURL, entry string) string {
	if strings.HasPrefix(entry, "http://") || strings.HasPrefix(entry, "https://") {
		return entry
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return entry
	}
	if strings.HasPrefix(entry, "/") {
		parsed.Path = entry
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.Scheme + "://" + parsed.Host + entry
	}
	if idx := strings.LastIndex(parsed.Path, "/"); idx >= 0 {
		parsed.Path = parsed.Path[:idx+1] + entry
	} else {
		parsed.Path = "/" + entry
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
