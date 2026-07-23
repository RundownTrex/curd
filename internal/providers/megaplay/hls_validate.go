package megaplay

import (
	"fmt"
	"strings"
)

// knownAdCDNs lists CDN hostnames used for HLS ad-segment injection by megaplay.
// When a sub-playlist contains segments from these hosts, playback will fail
// because the segments are images (PNG/JPEG), not video.
var knownAdCDNs = []string{
	"ibyteimg.com",
	"p16-ad-sg",
	"p16-ad.",
}

// isAdSegmentURL returns true if the segment URL points to a known ad CDN.
func isAdSegmentURL(segURL string) bool {
	for _, cdn := range knownAdCDNs {
		if strings.Contains(segURL, cdn) {
			return true
		}
	}
	return false
}

// validateHLSStream fetches the first sub-playlist from a master m3u8 and checks
// whether the CDN has injected ad segments that will break mpv playback.
// Returns an error if the stream is unusable.
func validateHLSStream(masterURL string) error {
	// Fetch the master playlist.
	master, err := fetchString(masterURL, megaplayBaseURL+"/")
	if err != nil {
		// Can't validate — let mpv try anyway.
		return nil
	}

	// Find the first sub-playlist reference (lines that don't start with '#').
	var subPlaylistPath string
	for _, line := range strings.Split(master, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		subPlaylistPath = line
		break
	}
	if subPlaylistPath == "" {
		return nil
	}

	// Resolve the sub-playlist URL (it may be relative).
	subURL := subPlaylistPath
	if !strings.HasPrefix(subURL, "http://") && !strings.HasPrefix(subURL, "https://") {
		base := masterURL[:strings.LastIndex(masterURL, "/")+1]
		subURL = base + subPlaylistPath
	}

	subPlaylist, err := fetchString(subURL, megaplayBaseURL+"/")
	if err != nil {
		return nil
	}

	// Count real vs. ad segments.
	var totalSegs, adSegs int
	for _, line := range strings.Split(subPlaylist, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		totalSegs++
		if isAdSegmentURL(line) {
			adSegs++
		}
	}

	if totalSegs == 0 {
		return nil
	}

	// If more than half the segments are ads, the stream is broken.
	if adSegs*2 > totalSegs {
		return fmt.Errorf(
			"megaplay CDN is injecting ads into the HLS stream (%d/%d segments are ads). "+
				"The stream is unplayable. Please use a different provider",
			adSegs, totalSegs,
		)
	}
	return nil
}
