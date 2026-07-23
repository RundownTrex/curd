package anipub

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/wraient/curd/internal/providers"
)

var (
	videoPathRE = regexp.MustCompile(`/video/(\d+)/(sub|dub)`)
	dataIDRE    = regexp.MustCompile(`data-id="(\d+)"`)
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
	// (A healthy stream should have zero ad segments.)
	if adSegs*2 > totalSegs {
		return fmt.Errorf(
			"megaplay CDN is injecting ads into the HLS stream (%d/%d segments are ads). "+
				"The stream is unplayable. Please use a different provider",
			adSegs, totalSegs,
		)
	}
	return nil
}

func resolveMegaplayStream(videoLink, mode string) (string, string, error) {
	videoLink = strings.TrimSpace(videoLink)
	if videoLink == "" {
		return "", "", fmt.Errorf("empty video link")
	}

	embedID, linkMode, err := parseVideoLink(videoLink)
	if err != nil {
		if strings.HasPrefix(videoLink, "http://") || strings.HasPrefix(videoLink, "https://") {
			return "", "", fmt.Errorf("anipub returned an unsupported external embed (%s) which is currently unplayable. Please use a fallback provider", videoLink)
		}
		return "", "", err
	}
	mode = providers.NormalizeTranslationType(mode)
	if mode == "dub" {
		linkMode = "dub"
	} else {
		linkMode = "sub"
	}

	streamPage := fmt.Sprintf("%s/stream/s-2/%s/%s", megaplayBaseURL, embedID, linkMode)
	html, err := fetchString(streamPage, baseURL+"/")
	if err != nil {
		return "", "", err
	}

	dataID := dataIDRE.FindStringSubmatch(html)
	if len(dataID) < 2 {
		return "", "", fmt.Errorf("megaplay data-id not found")
	}

	sourcesURL := fmt.Sprintf("%s/stream/getSources?id=%s", megaplayBaseURL, dataID[1])
	var payload megaplaySourcesResponse
	if err := fetchJSON(sourcesURL, streamPage, &payload); err != nil {
		return "", "", err
	}

	streamURL := strings.TrimSpace(payload.Sources.File)
	if streamURL == "" {
		return "", "", fmt.Errorf("megaplay stream url missing")
	}

	// Validate the HLS stream before returning it — the megaplay CDN has been
	// observed injecting PNG ad segments that cause mpv to open without video.
	if err := validateHLSStream(streamURL); err != nil {
		return "", "", err
	}

	subtitle := pickSubtitleTrack(payload, mode)
	return streamURL, subtitle, nil
}

func parseVideoLink(videoLink string) (embedID, mode string, err error) {
	parsed, err := url.Parse(videoLink)
	if err != nil {
		return "", "", fmt.Errorf("parse video link: %w", err)
	}
	matches := videoPathRE.FindStringSubmatch(parsed.Path)
	if len(matches) < 3 {
		return "", "", fmt.Errorf("unsupported video link %q", videoLink)
	}
	embedID = matches[1]
	mode = matches[2]
	if _, err := strconv.Atoi(embedID); err != nil {
		return "", "", fmt.Errorf("invalid embed id %q", embedID)
	}
	return embedID, mode, nil
}

func pickSubtitleTrack(payload megaplaySourcesResponse, mode string) string {
	if mode == "dub" {
		return ""
	}
	var fallback string
	for _, track := range payload.Tracks {
		file := strings.TrimSpace(track.File)
		if file == "" || !strings.EqualFold(strings.TrimSpace(track.Kind), "captions") {
			continue
		}
		label := strings.ToLower(strings.TrimSpace(track.Label))
		if track.Default || strings.Contains(label, "english") {
			return file
		}
		if fallback == "" {
			fallback = file
		}
	}
	return fallback
}
