package senshi

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxSenshiSubtitleSize = 8 << 20

var senshiVTTTimingLine = regexp.MustCompile(`^\s*(?:(?:\d{2}:)?\d{2}:\d{2}\.\d{3})\s+-->\s+(?:(?:\d{2}:)?\d{2}:\d{2}\.\d{3})(?:\s|$)`)

type senshiSubtitleTrack struct {
	Src     string `json:"src"`
	Label   string `json:"label"`
	Default bool   `json:"default"`
}

// senshiSubtitleManifestURLs returns the candidate subtitle manifest URLs in
// preference order. Senshi historically exposed a Filemoon-style manifest via
// the serverFM "sub.info" query parameter (sub_filemoon.json), but migrated to
// an ArtPlayer-based player that renders styled ASS subtitles from
// sub_artplayer.json. Try the explicit sub.info first, then both well-known
// manifest names on the masked base URL.
func senshiSubtitleManifestURLs(item embedItem) []string {
	var out []string
	push := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		for _, existing := range out {
			if existing == raw {
				return
			}
		}
		out = append(out, raw)
	}
	if item.ServerFM != nil {
		push(subtitleInfoFromURL(strings.TrimSpace(*item.ServerFM)))
	}
	base := strings.TrimRight(strings.TrimSpace(item.MaskedBaseURL), "/")
	if base != "" {
		push(base + "/sub_filemoon.json")
		push(base + "/sub_artplayer.json")
	}
	return out
}

func subtitleInfoFromURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Query().Get("sub.info"))
}

// fetchSenshiSubtitle downloads and parses a subtitle manifest, returning the
// subtitle file URLs in preference order.
func fetchSenshiSubtitle(manifestURL string) ([]string, error) {
	manifestURL = strings.TrimSpace(manifestURL)
	if manifestURL == "" {
		return nil, nil
	}

	var raw json.RawMessage
	if err := fetchJSON(http.MethodGet, manifestURL, nil, &raw); err != nil {
		return nil, err
	}
	tracks, err := parseSenshiSubtitleTracks(raw)
	if err != nil {
		return nil, err
	}
	return senshiSubtitleTrackSources(tracks), nil
}

// parseSenshiSubtitleTracks decodes a subtitle manifest into tracks. Senshi
// has used several shapes over time, so this tolerates them all:
//   - Filemoon style: [{"src":"...","label":"...","default":true}, ...]
//   - ArtPlayer style: [{"url":"...","html":"English","type":"ass"}, ...]
//   - a wrapper object holding the array under "tracks"/"subtitles"/"subs"
//   - a plain array of URL strings
//   - a single track object
func parseSenshiSubtitleTracks(raw []byte) ([]senshiSubtitleTrack, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse senshi subtitle manifest: %w", err)
	}
	tracks := subtitleTracksFromNode(root)
	if len(tracks) == 0 {
		if isEmptyNode(root) {
			return nil, nil
		}
		return nil, fmt.Errorf("senshi subtitle manifest contains no recognizable tracks")
	}
	return tracks, nil
}

// isEmptyNode reports whether the parsed manifest root is structurally empty
// (an empty array/object), which is a valid "no subtitles" result rather than
// a parse failure.
func isEmptyNode(node any) bool {
	switch value := node.(type) {
	case nil:
		return true
	case []any:
		return len(value) == 0
	case map[string]any:
		return len(value) == 0
	case string:
		return strings.TrimSpace(value) == ""
	}
	return false
}

func subtitleTracksFromNode(node any) []senshiSubtitleTrack {
	switch value := node.(type) {
	case []any:
		var tracks []senshiSubtitleTrack
		for _, item := range value {
			tracks = append(tracks, subtitleTracksFromNode(item)...)
		}
		return tracks
	case map[string]any:
		if tracks, ok := subtitleTracksFromObject(value); ok {
			return tracks
		}
	case string:
		if src := strings.TrimSpace(value); src != "" {
			return []senshiSubtitleTrack{{Src: src}}
		}
	}
	return nil
}

func subtitleTracksFromObject(obj map[string]any) ([]senshiSubtitleTrack, bool) {
	for _, key := range []string{"tracks", "subtitles", "subs", "data"} {
		if child, ok := obj[key]; ok {
			if tracks := subtitleTracksFromNode(child); len(tracks) > 0 {
				return tracks, true
			}
		}
	}
	if track, ok := subtitleTrackFromObject(obj); ok {
		return []senshiSubtitleTrack{track}, true
	}
	return nil, false
}

func subtitleTrackFromObject(obj map[string]any) (senshiSubtitleTrack, bool) {
	track := senshiSubtitleTrack{
		Src:   subtitleFirstString(obj, "src", "url", "file"),
		Label: subtitleFirstString(obj, "label", "html", "lang", "language", "name"),
	}
	switch value := obj["default"].(type) {
	case bool:
		track.Default = value
	case string:
		track.Default = strings.EqualFold(value, "true") || strings.EqualFold(value, "default")
	}
	if track.Src == "" {
		return track, false
	}
	return track, true
}

func subtitleFirstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := obj[key].(string); ok {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	return ""
}

// senshiSubtitleTrackSources returns the subtitle file URLs in preference
// order. Senshi can put a forced/signs-only English track before the full
// dialogue track, so non-forced defaults are preferred, then English tracks.
func senshiSubtitleTrackSources(tracks []senshiSubtitleTrack) []string {
	var out []string
	push := func(src string) {
		src = strings.TrimSpace(src)
		if src == "" {
			return
		}
		for _, existing := range out {
			if existing == src {
				return
			}
		}
		out = append(out, src)
	}
	addByFilter := func(pred func(senshiSubtitleTrack) bool) {
		for _, track := range tracks {
			if pred(track) {
				push(track.Src)
			}
		}
	}

	addByFilter(func(track senshiSubtitleTrack) bool {
		return track.Default && !isSenshiForcedSubtitleLabel(track.Label)
	})
	addByFilter(func(track senshiSubtitleTrack) bool {
		label := strings.ToLower(strings.TrimSpace(track.Label))
		return strings.Contains(label, "eng") && !isSenshiForcedSubtitleLabel(label)
	})
	addByFilter(func(track senshiSubtitleTrack) bool { return track.Default })
	addByFilter(func(track senshiSubtitleTrack) bool {
		label := strings.ToLower(strings.TrimSpace(track.Label))
		return strings.Contains(label, "eng")
	})
	addByFilter(func(track senshiSubtitleTrack) bool { return true })
	return out
}

func isSenshiForcedSubtitleLabel(label string) bool {
	label = strings.ToLower(strings.TrimSpace(label))
	return strings.Contains(label, "forced") ||
		strings.Contains(label, "sign") ||
		strings.Contains(label, "song")
}

func resolveSenshiSubtitle(item embedItem) string {
	for _, manifestURL := range senshiSubtitleManifestURLs(item) {
		sources, err := fetchSenshiSubtitle(manifestURL)
		if err != nil || len(sources) == 0 {
			continue
		}
		for _, source := range sources {
			prepared := prepareSenshiSubtitle(source)
			if senshiSubtitleReachable(prepared) {
				return prepared
			}
		}
	}
	return ""
}

// senshiSubtitleReachable reports whether a prepared subtitle location can be
// loaded. Local cached files are always usable; remote URLs are probed so mpv
// is never handed a dead subtitle link.
func senshiSubtitleReachable(subtitleLocation string) bool {
	subtitleLocation = strings.TrimSpace(subtitleLocation)
	if subtitleLocation == "" {
		return false
	}
	if !strings.HasPrefix(subtitleLocation, "http://") && !strings.HasPrefix(subtitleLocation, "https://") {
		return true
	}
	req, err := newRequest(http.MethodGet, subtitleLocation)
	if err != nil {
		return false
	}
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
	return len(prefix) > 0
}

func prepareSenshiSubtitle(subtitleURL string) string {
	subtitleURL = strings.TrimSpace(subtitleURL)
	if subtitleURL == "" {
		return ""
	}
	switch subtitleExt(subtitleURL) {
	case ".ass", ".ssa":
		// Styled track: hand the validated URL straight to the player (mpv
		// renders ASS natively).
		if styled := validatedSenshiASSURL(subtitleURL); styled != "" {
			return styled
		}
		return subtitleURL
	case ".vtt":
		// Prefer the styled ASS twin when senshi ships one, then the
		// sanitized VTT, then the raw URL.
		if styled := validatedSenshiASSURL(subtitleURL); styled != "" {
			return styled
		}
		if local, err := cacheSanitizedSenshiVTT(subtitleURL, senshiSubtitleCacheDir()); err == nil {
			return local
		}
	}
	return subtitleURL
}

// subtitleExt returns the lowercase extension of the URL's path, ignoring any
// query string.
func subtitleExt(subtitleURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(subtitleURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(filepath.Ext(parsed.Path))
}

func validatedSenshiASSURL(subtitleURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(subtitleURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(parsed.Path))
	candidate := subtitleURL
	switch ext {
	case ".vtt":
		parsed.Path = strings.TrimSuffix(parsed.Path, filepath.Ext(parsed.Path)) + ".ass"
		candidate = parsed.String()
	case ".ass", ".ssa":
		// already a styled track; validate in place
	default:
		return ""
	}

	req, err := newRequest(http.MethodGet, candidate)
	if err != nil {
		return ""
	}
	req.Header.Set("Range", "bytes=0-8191")
	resp, err := httpClient().Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ""
	}
	prefix, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(strings.TrimPrefix(string(prefix), "\ufeff"))
	if !strings.HasPrefix(text, "[Script Info]") || !strings.Contains(text, "[V4+ Styles]") {
		return ""
	}
	return candidate
}

func senshiSubtitleCacheDir() string {
	return filepath.Join(os.TempDir(), "curd", "subtitles")
}

func cacheSanitizedSenshiVTT(subtitleURL, cacheDir string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(subtitleURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || !strings.EqualFold(filepath.Ext(parsed.Path), ".vtt") {
		return "", fmt.Errorf("not a remote WebVTT subtitle")
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", err
	}
	cleanupOldSenshiSubtitles(cacheDir)

	cacheKey := fmt.Sprintf("%x", sha256.Sum256([]byte(subtitleURL)))
	cachePath := filepath.Join(cacheDir, cacheKey+".vtt")
	if info, statErr := os.Stat(cachePath); statErr == nil && info.Size() > 0 && time.Since(info.ModTime()) < 12*time.Hour {
		return cachePath, nil
	}

	req, err := newRequest(http.MethodGet, subtitleURL)
	if err != nil {
		return "", err
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("subtitle request failed with status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxSenshiSubtitleSize+1))
	if err != nil {
		return "", err
	}
	if len(raw) > maxSenshiSubtitleSize {
		return "", fmt.Errorf("subtitle is too large")
	}
	sanitized, changed := sanitizeSenshiWebVTT(raw)
	if !changed {
		return subtitleURL, nil
	}
	tempPath := cachePath + ".tmp"
	if err := os.WriteFile(tempPath, sanitized, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, cachePath); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}
	return cachePath, nil
}

func sanitizeSenshiWebVTT(raw []byte) ([]byte, bool) {
	text := strings.TrimPrefix(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\ufeff")
	lines := strings.Split(text, "\n")
	timingIndexes := make([]int, 0)
	for index, line := range lines {
		if senshiVTTTimingLine.MatchString(strings.TrimSpace(line)) {
			timingIndexes = append(timingIndexes, index)
		}
	}
	if len(timingIndexes) == 0 {
		return raw, false
	}

	changed := strings.Contains(text, "\\h")
	out := []string{"WEBVTT", ""}
	for cueIndex, timingIndex := range timingIndexes {
		end := len(lines)
		if cueIndex+1 < len(timingIndexes) {
			end = timingIndexes[cueIndex+1]
		}
		content := append([]string(nil), lines[timingIndex+1:end]...)
		for len(content) > 0 && strings.TrimSpace(content[len(content)-1]) == "" {
			content = content[:len(content)-1]
		}
		for len(content) > 0 && strings.TrimSpace(content[0]) == "" {
			content = content[1:]
		}
		if len(content) == 0 {
			continue
		}
		out = append(out, strings.TrimSpace(lines[timingIndex]))
		for _, line := range content {
			line = strings.ReplaceAll(strings.TrimSuffix(line, "\r"), "\\h", "\u00a0")
			if strings.TrimSpace(line) == "" {
				line = "\u00a0"
				changed = true
			}
			out = append(out, line)
		}
		out = append(out, "")
	}
	if !changed {
		return raw, false
	}
	return []byte(strings.Join(out, "\n")), true
}

func cleanupOldSenshiSubtitles(cacheDir string) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".vtt") {
			continue
		}
		info, err := entry.Info()
		if err == nil && time.Since(info.ModTime()) > 48*time.Hour {
			_ = os.Remove(filepath.Join(cacheDir, entry.Name()))
		}
	}
}

func isSubEmbedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "hardsub", "softsub", "sub":
		return true
	default:
		return false
	}
}
