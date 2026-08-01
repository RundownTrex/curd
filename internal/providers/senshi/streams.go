package senshi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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
	url := fmt.Sprintf("%s/episode-embeds/%d/%d", baseURL, malID, epNo)
	if err := fetchJSON(http.MethodGet, url, nil, &embeds); err != nil {
		return nil, nil, err
	}
	if len(embeds) == 0 {
		return nil, nil, fmt.Errorf("no streams found for episode %d", epNo)
	}

	wantStatus := "HardSub"
	if mode == "dub" {
		wantStatus = "Dub"
	}

	hadCandidates := false
	for _, item := range embeds {
		if !strings.EqualFold(strings.TrimSpace(item.Status), wantStatus) {
			continue
		}

		candidates := senshiStreamCandidates(item)
		if len(candidates) == 0 {
			continue
		}
		hadCandidates = true

		streamURL, ok := pickWorkingSenshiStream(item)
		if !ok {
			continue
		}

		subtitle := ""
		if mode == "sub" {
			// Senshi labels streams HardSub even when subtitles are external.
			subtitle = resolveSenshiSubtitle(item)
		}

		hints := map[string]providers.StreamPlaybackHint{
			streamURL: {
				Referrer: baseURL + "/",
				Subtitle: subtitle,
			},
		}
		return []string{streamURL}, hints, nil
	}

	if hadCandidates {
		return nil, nil, fmt.Errorf("no playable %s streams found for episode %d (all stream sources unreachable: senshi's upstream CDN is down and this episode has no alternate source)", mode, epNo)
	}
	return nil, nil, fmt.Errorf("no playable %s streams found for episode %d (episode embeds contained no stream sources)", mode, epNo)
}

// pickWorkingSenshiStream returns the first stream URL on the embed that
// actually serves content, preferring the HLS manifest and falling back to the
// direct server2 file. This keeps mpv from being handed a dead CDN link that
// would otherwise open an empty player window.
func pickWorkingSenshiStream(item embedItem) (string, bool) {
	for _, candidate := range senshiStreamCandidates(item) {
		if senshiStreamReachable(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func senshiStreamCandidates(item embedItem) []string {
	var candidates []string
	if u := strings.TrimSpace(item.URL); u != "" {
		candidates = append(candidates, u)
	}
	if item.Server2 != nil {
		if u := strings.TrimSpace(*item.Server2); u != "" && (len(candidates) == 0 || u != candidates[0]) {
			candidates = append(candidates, u)
		}
	}
	return candidates
}

func senshiStreamReachable(streamURL string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", baseURL+"/")
	req.Header.Set("Range", "bytes=0-2047")

	resp, err := httpClient().Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return false
	}

	prefix, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return false
	}
	if len(prefix) == 0 {
		return false
	}

	if strings.Contains(streamURL, ".m3u8") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(string(prefix), "\ufeff"))
		return strings.HasPrefix(trimmed, "#EXTM3U")
	}
	return true
}
