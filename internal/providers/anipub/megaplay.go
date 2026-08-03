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

// isMediaPlaylist reports whether the playlist text references segments
// (#EXTINF) instead of variant streams (#EXT-X-STREAM-INF).
func isMediaPlaylist(playlist string) bool {
	return strings.Contains(playlist, "#EXTINF")
}

// variantPlaylistURLs extracts the sub-playlist (variant) URLs referenced by a
// master playlist. Playlist references are lines that don't start with '#';
// they may be absolute, root-relative, or relative to the master URL.
func variantPlaylistURLs(master, masterURL string) []string {
	masterParsed, err := url.Parse(masterURL)
	if err != nil {
		return nil
	}
	var urls []string
	for _, line := range strings.Split(master, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ref, err := url.Parse(line)
		if err != nil {
			continue
		}
		resolved := masterParsed.ResolveReference(ref)
		if resolved.Scheme != "" && resolved.Host != "" {
			urls = append(urls, resolved.String())
		}
	}
	return urls
}

// countAdSegments counts the segments in a playlist text and how many of them
// point at known ad CDNs.
func countAdSegments(playlist string) (ad, total int) {
	for _, line := range strings.Split(playlist, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		total++
		if isAdSegmentURL(line) {
			ad++
		}
	}
	return ad, total
}

// validateHLSStream fetches the master m3u8 and checks every variant
// sub-playlist it references for CDN-injected ad segments. The stream is only
// rejected when every variant was fetched and every variant is ad-infected:
// a single healthy variant means mpv can still play the episode.
// Returns an error if the stream is unusable.
func validateHLSStream(masterURL string) error {
	// Fetch the master playlist.
	master, err := fetchString(masterURL, megaplayBaseURL+"/")
	if err != nil {
		// Can't validate — let mpv try anyway.
		return nil
	}

	// A media playlist (segments, no variants) is validated directly — its
	// segment URLs must never be fetched.
	if isMediaPlaylist(master) {
		adSegs, totalSegs := countAdSegments(master)
		if totalSegs > 0 && adSegs*2 > totalSegs {
			return fmt.Errorf(
				"megaplay CDN is injecting ads into the HLS stream (%d/%d segments are ads). "+
					"The stream is unplayable. Please use a different provider",
				adSegs, totalSegs,
			)
		}
		return nil
	}

	variants := variantPlaylistURLs(master, masterURL)
	if len(variants) == 0 {
		return nil
	}

	var healthy, adInfected, unverifiable int
	for _, variantURL := range variants {
		variant, fetchErr := fetchString(variantURL, megaplayBaseURL+"/")
		if fetchErr != nil {
			// Can't validate this variant — don't count it either way.
			unverifiable++
			continue
		}
		adSegs, totalSegs := countAdSegments(variant)
		if totalSegs == 0 {
			unverifiable++
			continue
		}
		if adSegs*2 > totalSegs {
			adInfected++
		} else {
			healthy++
		}
	}

	// If any variant is healthy, playback can work — let mpv try.
	if healthy > 0 {
		return nil
	}
	// Reject only when every variant was verified and all of them are ads.
	if adInfected > 0 && unverifiable == 0 {
		return fmt.Errorf(
			"megaplay CDN is injecting ads into the HLS stream (%d/%d variant playlists are ads). "+
				"The stream is unplayable. Please use a different provider",
			adInfected, len(variants),
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
