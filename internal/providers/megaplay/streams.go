package megaplay

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/wraient/curd/internal/providers"
)

var dataIDRE = regexp.MustCompile(`data-id="(\d+)"`)

func getEpisodeStreamsForMode(malIDStr string, config providers.PlaybackConfig, epNo int) ([]string, map[string]providers.StreamPlaybackHint, error) {
	malID, err := parseMalID(malIDStr)
	if err != nil {
		return nil, nil, err
	}
	if epNo <= 0 {
		return nil, nil, fmt.Errorf("invalid episode number %d", epNo)
	}

	mode := providers.NormalizeTranslationType(config.SubOrDub)

	// Step 1: Fetch the stream page HTML.
	streamPageURL := fmt.Sprintf("%s/stream/mal/%d/%d/%s", megaplayBaseURL, malID, epNo, mode)
	html, err := fetchString(streamPageURL, megaplayBaseURL+"/")
	if err != nil {
		return nil, nil, fmt.Errorf("fetch stream page: %w", err)
	}

	// Step 2: Extract data-id from the HTML.
	matches := dataIDRE.FindStringSubmatch(html)
	if len(matches) < 2 {
		return nil, nil, fmt.Errorf("megaplay data-id not found in stream page for mal %d ep %d %s", malID, epNo, mode)
	}
	dataID := matches[1]

	// Step 3: Fetch stream sources JSON.
	sourcesURL := fmt.Sprintf("%s/stream/getSources?id=%s", megaplayBaseURL, dataID)
	var payload megaplaySourcesResponse
	if err := fetchJSON(sourcesURL, megaplayBaseURL+"/", &payload); err != nil {
		return nil, nil, fmt.Errorf("fetch stream sources: %w", err)
	}

	// Step 4: Extract HLS stream URL.
	streamURL := strings.TrimSpace(payload.streamFile())
	if streamURL == "" {
		return nil, nil, fmt.Errorf("megaplay stream url missing for mal %d ep %d", malID, epNo)
	}

	// Step 5: Pick the best subtitle track.
	subtitle := pickSubtitleTrack(payload, mode)

	hints := map[string]providers.StreamPlaybackHint{
		streamURL: {
			Referrer: megaplayBaseURL + "/",
			Subtitle: subtitle,
		},
	}
	return []string{streamURL}, hints, nil
}

// pickSubtitleTrack selects the best English subtitle from the response.
// Matches the anikoto-cli logic: prefer exact "english", then partial "eng" (excluding signs/songs).
func pickSubtitleTrack(payload megaplaySourcesResponse, mode string) string {
	if mode == "dub" {
		return ""
	}

	tracks := payload.allSubTracks()
	var first, best string
	bestScore := 0

	for _, track := range tracks {
		file := strings.TrimSpace(track.fileURL())
		if file == "" {
			continue
		}

		kind := strings.ToLower(strings.TrimSpace(track.kindOrType()))
		// Only consider caption/subtitle tracks (or tracks with no kind specified).
		if kind != "" && !strings.Contains(kind, "caption") && !strings.Contains(kind, "subtitle") && !strings.Contains(kind, "sub") {
			continue
		}

		if first == "" {
			first = file
		}

		label := strings.ToLower(strings.TrimSpace(track.labelOrTitle()))

		// Exact "english" label gets highest priority.
		if strings.TrimSpace(label) == "english" {
			if bestScore < 3 {
				best = file
				bestScore = 3
			}
		} else if strings.Contains(label, "eng") && !strings.Contains(label, "sign") && !strings.Contains(label, "song") {
			if bestScore < 2 {
				best = file
				bestScore = 2
			}
		} else if strings.Contains(label, "eng") {
			if bestScore < 1 {
				best = file
				bestScore = 1
			}
		}

		// Prefer tracks marked as default.
		if track.Default && strings.Contains(label, "eng") && bestScore < 3 {
			best = file
			bestScore = 3
		}
	}

	if best != "" {
		return best
	}
	return first
}
