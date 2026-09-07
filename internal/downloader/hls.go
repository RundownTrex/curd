package downloader

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	streamInfRE   = regexp.MustCompile(`(?i)#EXT-X-STREAM-INF:([^\r\n]+)`)
	keyRE         = regexp.MustCompile(`(?i)#EXT-X-KEY:([^\r\n]+)`)
	resAttrRE     = regexp.MustCompile(`(?i)RESOLUTION=(\d+)x(\d+)`)
	bandAttrRE    = regexp.MustCompile(`(?i)BANDWIDTH=(\d+)`)
	uriAttrRE     = regexp.MustCompile(`(?i)URI="([^"]+)"`)
	methodRE      = regexp.MustCompile(`(?i)METHOD=([A-Za-z0-9\-]+)`)
	ivAttrRE      = regexp.MustCompile(`(?i)IV=0x([0-9a-fA-F]+)`)
	audioAttrRE   = regexp.MustCompile(`(?i)AUDIO="([^"]+)"`)
	mediaAudioRE  = regexp.MustCompile(`(?i)#EXT-X-MEDIA:([^\r\n]+)`)
	typeAttrRE    = regexp.MustCompile(`(?i)TYPE=([A-Za-z0-9\-]+)`)
	groupAttrRE   = regexp.MustCompile(`(?i)GROUP-ID="([^"]+)"`)
	nameAttrRE    = regexp.MustCompile(`(?i)NAME="([^"]+)"`)
	langAttrRE    = regexp.MustCompile(`(?i)LANGUAGE="([^"]+)"`)
	defaultAttrRE = regexp.MustCompile(`(?i)DEFAULT=(YES|NO)`)
)

// HLSSegment represents a single transport stream segment.
type HLSSegment struct {
	Index    int
	URL      string
	Duration float64
	IV       []byte
}

// HLSPlaylist holds parsed media playlist data ready for download.
type HLSPlaylist struct {
	MediaURL      string
	Segments      []HLSSegment
	AudioMediaURL string
	AudioSegments []HLSSegment
	AudioKeyBytes []byte
	AudioKeyIV    []byte
	AudioIsAES128 bool
	KeyBytes      []byte
	KeyIV         []byte
	IsAES128      bool
}

type streamVariant struct {
	URL          string
	Bandwidth    int
	Resolution   int // height e.g. 1080
	AudioGroupID string
}

type hlsAudioTrack struct {
	GroupID  string
	Name     string
	Language string
	Default  bool
	URI      string
}

// FetchHLSPlaylist retrieves and parses the master and media playlists.
func FetchHLSPlaylist(ctx context.Context, client *http.Client, masterURL string, headers map[string]string, preferredQuality string) (*HLSPlaylist, error) {
	parsedMaster, err := url.Parse(masterURL)
	if err != nil {
		return nil, fmt.Errorf("invalid stream URL %q: %w", masterURL, err)
	}

	body, finalURL, err := fetchWithHeaders(ctx, client, masterURL, headers)
	if err != nil {
		return nil, fmt.Errorf("fetch playlist: %w", err)
	}

	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "#EXTM3U") {
		return nil, fmt.Errorf("URL does not return a valid HLS playlist (#EXTM3U missing)")
	}

	finalParsed, err := url.Parse(finalURL)
	if err == nil {
		parsedMaster = finalParsed
	}

	mediaURL := parsedMaster.String()
	mediaBody := body
	var selectedAudioURI string

	// Check if this is a master playlist containing variants
	if strings.Contains(body, "#EXT-X-STREAM-INF") {
		selectedVariant, audioURI, err := selectBestVariant(parsedMaster, body, preferredQuality)
		if err != nil {
			return nil, fmt.Errorf("select stream variant: %w", err)
		}

		mediaURL = selectedVariant.URL
		selectedAudioURI = audioURI
		variantParsed, err := url.Parse(selectedVariant.URL)
		if err == nil {
			parsedMaster = variantParsed
		}

		vBody, vFinalURL, err := fetchWithHeaders(ctx, client, selectedVariant.URL, headers)
		if err != nil {
			return nil, fmt.Errorf("fetch media playlist %q: %w", selectedVariant.URL, err)
		}
		mediaBody = vBody
		if fURL, err := url.Parse(vFinalURL); err == nil {
			parsedMaster = fURL
		}
	}

	// Parse video media playlist
	playlist, err := parseMediaPlaylist(ctx, client, parsedMaster, mediaBody, headers)
	if err != nil {
		return nil, fmt.Errorf("parse media playlist: %w", err)
	}

	playlist.MediaURL = mediaURL

	// If a separate audio stream is defined (e.g. KickAssAnime/Krussdomi), fetch and parse audio playlist
	if selectedAudioURI != "" {
		aBody, aFinalURL, aErr := fetchWithHeaders(ctx, client, selectedAudioURI, headers)
		if aErr == nil {
			aParsed, err := url.Parse(aFinalURL)
			if err != nil {
				aParsed, _ = url.Parse(selectedAudioURI)
			}
			audioPlaylist, parseErr := parseMediaPlaylist(ctx, client, aParsed, aBody, headers)
			if parseErr == nil && len(audioPlaylist.Segments) > 0 {
				playlist.AudioMediaURL = selectedAudioURI
				playlist.AudioSegments = audioPlaylist.Segments
				playlist.AudioKeyBytes = audioPlaylist.KeyBytes
				playlist.AudioKeyIV = audioPlaylist.KeyIV
				playlist.AudioIsAES128 = audioPlaylist.IsAES128
			}
		}
	}

	return playlist, nil
}

func selectBestVariant(baseURL *url.URL, masterContent string, preferredQuality string) (streamVariant, string, error) {
	scanner := bufio.NewScanner(strings.NewReader(masterContent))
	var variants []streamVariant
	var audioTracks []hlsAudioTrack
	var currentAttrs string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Extract audio renditions (#EXT-X-MEDIA:TYPE=AUDIO,...)
		if strings.HasPrefix(line, "#EXT-X-MEDIA:") {
			if typeMatch := typeAttrRE.FindStringSubmatch(line); len(typeMatch) > 1 && strings.EqualFold(typeMatch[1], "AUDIO") {
				var track hlsAudioTrack
				if g := groupAttrRE.FindStringSubmatch(line); len(g) > 1 {
					track.GroupID = g[1]
				}
				if n := nameAttrRE.FindStringSubmatch(line); len(n) > 1 {
					track.Name = n[1]
				}
				if l := langAttrRE.FindStringSubmatch(line); len(l) > 1 {
					track.Language = l[1]
				}
				if d := defaultAttrRE.FindStringSubmatch(line); len(d) > 1 {
					track.Default = strings.EqualFold(d[1], "YES")
				}
				if u := uriAttrRE.FindStringSubmatch(line); len(u) > 1 {
					relURL, err := url.Parse(u[1])
					if err == nil {
						track.URI = baseURL.ResolveReference(relURL).String()
					}
				}
				if track.URI != "" {
					audioTracks = append(audioTracks, track)
				}
			}
			continue
		}

		if match := streamInfRE.FindStringSubmatch(line); len(match) > 1 {
			currentAttrs = match[1]
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		if currentAttrs != "" {
			var resolution int
			var bandwidth int
			var audioGroupID string

			if resMatch := resAttrRE.FindStringSubmatch(currentAttrs); len(resMatch) > 2 {
				if h, err := strconv.Atoi(resMatch[2]); err == nil {
					resolution = h
				}
			}
			if bMatch := bandAttrRE.FindStringSubmatch(currentAttrs); len(bMatch) > 1 {
				if b, err := strconv.Atoi(bMatch[1]); err == nil {
					bandwidth = b
				}
			}
			if aMatch := audioAttrRE.FindStringSubmatch(currentAttrs); len(aMatch) > 1 {
				audioGroupID = aMatch[1]
			}

			relURL, err := url.Parse(line)
			if err == nil {
				resolved := baseURL.ResolveReference(relURL).String()
				variants = append(variants, streamVariant{
					URL:          resolved,
					Bandwidth:    bandwidth,
					Resolution:   resolution,
					AudioGroupID: audioGroupID,
				})
			}
			currentAttrs = ""
		}
	}

	if len(variants) == 0 {
		return streamVariant{}, "", fmt.Errorf("no variants found in master playlist")
	}

	// Sort variants
	targetRes := parseTargetResolution(preferredQuality)
	var chosenVariant streamVariant

	if targetRes > 0 {
		// Look for exact match first
		for _, v := range variants {
			if v.Resolution == targetRes {
				chosenVariant = v
				break
			}
		}
		// If not exact, find closest lower resolution, or closest overall
		if chosenVariant.URL == "" {
			sort.Slice(variants, func(i, j int) bool {
				diffI := abs(variants[i].Resolution - targetRes)
				diffJ := abs(variants[j].Resolution - targetRes)
				if diffI != diffJ {
					return diffI < diffJ
				}
				return variants[i].Bandwidth > variants[j].Bandwidth
			})
			chosenVariant = variants[0]
		}
	} else {
		// Best quality: highest resolution, then highest bandwidth
		sort.Slice(variants, func(i, j int) bool {
			if variants[i].Resolution != variants[j].Resolution {
				return variants[i].Resolution > variants[j].Resolution
			}
			return variants[i].Bandwidth > variants[j].Bandwidth
		})
		chosenVariant = variants[0]
	}

	// Resolve matching audio track if separate audio stream is specified
	var chosenAudioURI string
	if chosenVariant.AudioGroupID != "" && len(audioTracks) > 0 {
		// Filter matching group
		var groupTracks []hlsAudioTrack
		for _, at := range audioTracks {
			if at.GroupID == chosenVariant.AudioGroupID {
				groupTracks = append(groupTracks, at)
			}
		}

		if len(groupTracks) > 0 {
			// Priority: DEFAULT=YES, then Japanese ("jpn"), then English ("eng"), then first available
			for _, at := range groupTracks {
				if at.Default {
					chosenAudioURI = at.URI
					break
				}
			}
			if chosenAudioURI == "" {
				for _, at := range groupTracks {
					if strings.HasPrefix(strings.ToLower(at.Language), "jp") {
						chosenAudioURI = at.URI
						break
					}
				}
			}
			if chosenAudioURI == "" {
				chosenAudioURI = groupTracks[0].URI
			}
		}
	}

	return chosenVariant, chosenAudioURI, nil
}

func parseTargetResolution(quality string) int {
	quality = strings.ToLower(strings.TrimSpace(quality))
	switch {
	case strings.Contains(quality, "1080"):
		return 1080
	case strings.Contains(quality, "720"):
		return 720
	case strings.Contains(quality, "480"):
		return 480
	case strings.Contains(quality, "360"):
		return 360
	default:
		return 0 // best
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func parseMediaPlaylist(ctx context.Context, client *http.Client, mediaBaseURL *url.URL, content string, headers map[string]string) (*HLSPlaylist, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	playlist := &HLSPlaylist{}

	var currentDuration float64
	var keyURI string
	var keyIV []byte
	var isAES128 bool

	segmentIndex := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#EXT-X-KEY:") {
			if mMatch := methodRE.FindStringSubmatch(line); len(mMatch) > 1 {
				if strings.EqualFold(mMatch[1], "AES-128") {
					isAES128 = true
				}
			}
			if uMatch := uriAttrRE.FindStringSubmatch(line); len(uMatch) > 1 {
				rawURI := uMatch[1]
				parsedURI, err := url.Parse(rawURI)
				if err == nil {
					keyURI = mediaBaseURL.ResolveReference(parsedURI).String()
				}
			}
			if ivMatch := ivAttrRE.FindStringSubmatch(line); len(ivMatch) > 1 {
				decodedIV, err := hex.DecodeString(ivMatch[1])
				if err == nil && len(decodedIV) == 16 {
					keyIV = decodedIV
				}
			}
			continue
		}

		if strings.HasPrefix(line, "#EXTINF:") {
			durStr := strings.TrimPrefix(line, "#EXTINF:")
			durStr = strings.Split(durStr, ",")[0]
			durStr = strings.TrimSpace(durStr)
			if d, err := strconv.ParseFloat(durStr, 64); err == nil {
				currentDuration = d
			}
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		// Non-comment line represents a segment URL
		relURL, err := url.Parse(line)
		if err != nil {
			continue
		}
		resolvedSegURL := mediaBaseURL.ResolveReference(relURL).String()

		// Skip ad segments if detected
		if isKnownAdSegment(resolvedSegURL) {
			continue
		}

		seg := HLSSegment{
			Index:    segmentIndex,
			URL:      resolvedSegURL,
			Duration: currentDuration,
		}

		// If no explicit IV in EXT-X-KEY, HLS RFC 8216 specifies sequence number as 16-byte big-endian IV
		if len(keyIV) == 16 {
			seg.IV = keyIV
		} else if isAES128 {
			segIV := make([]byte, 16)
			binary.BigEndian.PutUint64(segIV[8:], uint64(segmentIndex))
			seg.IV = segIV
		}

		playlist.Segments = append(playlist.Segments, seg)
		segmentIndex++
		currentDuration = 0
	}

	if len(playlist.Segments) == 0 {
		return nil, fmt.Errorf("no media segments found in playlist")
	}

	playlist.IsAES128 = isAES128
	if isAES128 && keyURI != "" {
		keyBytes, _, err := fetchBytesWithHeaders(ctx, client, keyURI, headers)
		if err != nil {
			return nil, fmt.Errorf("fetch AES-128 key from %q: %w", keyURI, err)
		}
		if len(keyBytes) != 16 {
			return nil, fmt.Errorf("invalid AES-128 key length %d (expected 16 bytes)", len(keyBytes))
		}
		playlist.KeyBytes = keyBytes
		playlist.KeyIV = keyIV
	}

	return playlist, nil
}

func isKnownAdSegment(segURL string) bool {
	adMarkers := []string{"ibyteimg.com", "p16-ad-sg", "p16-ad."}
	for _, marker := range adMarkers {
		if strings.Contains(segURL, marker) {
			return true
		}
	}
	return false
}

func fetchWithHeaders(ctx context.Context, client *http.Client, targetURL string, headers map[string]string) (string, string, error) {
	bytes, finalURL, err := fetchBytesWithHeaders(ctx, client, targetURL, headers)
	if err != nil {
		return "", "", err
	}
	return string(bytes), finalURL, nil
}

func fetchBytesWithHeaders(ctx context.Context, client *http.Client, targetURL string, headers map[string]string) ([]byte, string, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, "", err
	}

	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	finalURL := targetURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	return data, finalURL, nil
}
