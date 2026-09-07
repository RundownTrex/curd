package internal

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	androidProxyMu     sync.Mutex
	androidProxyServer *AndroidStreamProxy
	pngIENDMarker      = []byte{0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}
	assTagRegex        = regexp.MustCompile(`\{[^}]*\}`)
	srtTimeRegex       = regexp.MustCompile(`(\d{2}:\d{2}:\d{2}),(\d{3})`)
	hlsSubGroupRegex   = regexp.MustCompile(`SUBTITLES="[^"]*"`)
)

type androidStreamSession struct {
	id           string
	targetURL    string
	referrer     string
	origin       string
	userAgent    string
	subtitleURL  string
	convertedSub []byte
	createdAt    time.Time
}

type AndroidStreamProxy struct {
	baseURL  string
	server   *http.Server
	listener net.Listener
	sessions map[string]*androidStreamSession
	mu       sync.RWMutex
	client   *http.Client
}

// GetAndroidStreamProxy returns or initializes the singleton stream proxy listening on 127.0.0.1:0.
func GetAndroidStreamProxy() (*AndroidStreamProxy, error) {
	androidProxyMu.Lock()
	defer androidProxyMu.Unlock()

	if androidProxyServer != nil {
		return androidProxyServer, nil
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start android stream proxy listener: %w", err)
	}

	proxy := &AndroidStreamProxy{
		baseURL:  "http://" + listener.Addr().String(),
		listener: listener,
		sessions: make(map[string]*androidStreamSession),
		client: &http.Client{
			Timeout: 45 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				if len(via) > 0 {
					for k, vv := range via[0].Header {
						for _, v := range vv {
							req.Header.Add(k, v)
						}
					}
				}
				return nil
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/hls/", proxy.handleHLS)
	mux.HandleFunc("/file/", proxy.handleFile)

	server := &http.Server{
		Handler: mux,
	}
	proxy.server = server

	go func() {
		_ = server.Serve(listener)
	}()

	androidProxyServer = proxy
	Log(fmt.Sprintf("Android stream relay initialized on %s", proxy.baseURL))
	return proxy, nil
}

// PrepareAndroidPlaybackURL registers the target stream link with the local proxy
// and returns a localhost URL that forwards all requests with necessary CDN headers.
func PrepareAndroidPlaybackURL(anime *Anime, link string) (string, error) {
	cleanLink := strings.TrimSpace(link)
	if cleanLink == "" {
		return "", fmt.Errorf("empty stream link")
	}

	// Already running through a local proxy
	if strings.HasPrefix(cleanLink, "http://127.0.0.1:") || strings.HasPrefix(cleanLink, "http://localhost:") {
		return cleanLink, nil
	}

	referrer := ""
	subtitleURL := ""
	origin := ""

	if anime != nil {
		subtitleURL = strings.TrimSpace(anime.Ep.SubtitleURL)
		referrer = strings.TrimSpace(anime.Ep.StreamReferrer)
		if referrer == "" {
			referrer = streamReferrerForLink(cleanLink, CurrentAnimeProviderName(anime))
		}

		providerName := strings.ToLower(CurrentAnimeProviderName(anime))
		if providerName == "kickassanime" || strings.Contains(cleanLink, "krussdomi.com") {
			origin = "https://krussdomi.com"
			if referrer != "" {
				if u, err := url.Parse(referrer); err == nil && u.Scheme != "" && u.Host != "" {
					origin = fmt.Sprintf("%s://%s", u.Scheme, u.Host)
				}
			}
		}
	}

	proxy, err := GetAndroidStreamProxy()
	if err != nil {
		Log(fmt.Sprintf("Failed to get stream proxy: %v, falling back to direct link", err))
		return cleanLink, nil
	}

	return proxy.Register(cleanLink, referrer, origin, subtitleURL)
}

// Register adds a stream session and returns the proxied local URL.
func (p *AndroidStreamProxy) Register(targetURL, referrer, origin, subtitleURL string) (string, error) {
	id, err := randomSessionID()
	if err != nil {
		id = fmt.Sprintf("%x", time.Now().UnixNano())
	}

	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	p.mu.Lock()
	p.sessions[id] = &androidStreamSession{
		id:          id,
		targetURL:   targetURL,
		referrer:    referrer,
		origin:      origin,
		userAgent:   ua,
		subtitleURL: subtitleURL,
		createdAt:   time.Now(),
	}
	p.mu.Unlock()

	isHLS := strings.Contains(targetURL, ".m3u8") || strings.Contains(targetURL, "m3u8")
	if isHLS {
		return fmt.Sprintf("%s/hls/%s/master.m3u8", p.baseURL, id), nil
	}

	ext := ".mp4"
	if strings.Contains(targetURL, ".mkv") {
		ext = ".mkv"
	} else if strings.Contains(targetURL, ".webm") {
		ext = ".webm"
	}
	return fmt.Sprintf("%s/file/%s/video%s", p.baseURL, id, ext), nil
}

func (p *AndroidStreamProxy) handleHLS(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/hls/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	sessionID := parts[0]
	endpoint := parts[1]

	p.mu.RLock()
	session, ok := p.sessions[sessionID]
	p.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch {
	case endpoint == "master.m3u8":
		p.serveHLSMaster(w, r, session)
	case endpoint == "variant.m3u8":
		p.serveHLSVariant(w, r, session)
	case endpoint == "seg.ts":
		p.serveHLSSegment(w, r, session)
	case endpoint == "key":
		p.serveHLSKey(w, r, session)
	case endpoint == "map.mp4":
		p.serveHLSInitMap(w, r, session)
	case endpoint == "sub.m3u8":
		p.serveHLSSubtitlePlaylist(w, r, session)
	case endpoint == "sub.vtt":
		p.serveHLSSubtitleFile(w, r, session)
	default:
		http.NotFound(w, r)
	}
}

func (p *AndroidStreamProxy) serveHLSMaster(w http.ResponseWriter, r *http.Request, session *androidStreamSession) {
	req, err := http.NewRequestWithContext(r.Context(), "GET", session.targetURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.setHeaders(req, session)

	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("fetch upstream master: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("upstream master status %d", resp.StatusCode), resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	content := string(body)

	// If not m3u8 playlist format, stream original body
	if !strings.Contains(content, "#EXTM3U") {
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return
	}

	isMaster := strings.Contains(content, "#EXT-X-STREAM-INF")

	if !isMaster && strings.TrimSpace(session.subtitleURL) != "" {
		// Upstream targetURL is a single variant media playlist (segments) rather than a multivariant master playlist.
		// In HLS (RFC 8216), subtitles cannot be attached directly to a media playlist; they require a master playlist.
		// Wrap the variant playlist with a master playlist referencing our injected subtitle track.
		master := fmt.Sprintf("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"curd-subs\",NAME=\"English\",DEFAULT=YES,AUTOSELECT=YES,FORCED=NO,LANGUAGE=\"en\",URI=\"/hls/%s/sub.m3u8\"\n#EXT-X-STREAM-INF:BANDWIDTH=5000000,SUBTITLES=\"curd-subs\"\n/hls/%s/variant.m3u8?u=%s\n",
			session.id, session.id, url.QueryEscape(session.targetURL))

		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(master))
		return
	}

	rewritten := p.rewritePlaylist(content, session.targetURL, session.id, session.subtitleURL)

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(rewritten))
}

func (p *AndroidStreamProxy) serveHLSVariant(w http.ResponseWriter, r *http.Request, session *androidStreamSession) {
	rawTarget := r.URL.Query().Get("u")
	if rawTarget == "" {
		http.Error(w, "missing u parameter", http.StatusBadRequest)
		return
	}
	targetURL, err := url.QueryUnescape(rawTarget)
	if err != nil {
		targetURL = rawTarget
	}

	req, err := http.NewRequestWithContext(r.Context(), "GET", targetURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.setHeaders(req, session)

	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("fetch variant: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("upstream variant status %d", resp.StatusCode), resp.StatusCode)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rewritten := p.rewritePlaylist(string(body), targetURL, session.id, "")

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(rewritten))
}

func (p *AndroidStreamProxy) serveHLSSegment(w http.ResponseWriter, r *http.Request, session *androidStreamSession) {
	rawTarget := r.URL.Query().Get("u")
	if rawTarget == "" {
		http.Error(w, "missing u parameter", http.StatusBadRequest)
		return
	}
	segURL, err := url.QueryUnescape(rawTarget)
	if err != nil {
		segURL = rawTarget
	}

	req, err := http.NewRequestWithContext(r.Context(), "GET", segURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.setHeaders(req, session)

	if rangeHdr := r.Header.Get("Range"); rangeHdr != "" {
		req.Header.Set("Range", rangeHdr)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("fetch segment: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// De-obfuscate fake PNG envelope if present (e.g. Vibe chunks)
	if bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
		if idx := bytes.Index(body, pngIENDMarker); idx != -1 {
			body = body[idx+len(pngIENDMarker):]
		}
	}

	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func (p *AndroidStreamProxy) serveHLSKey(w http.ResponseWriter, r *http.Request, session *androidStreamSession) {
	rawTarget := r.URL.Query().Get("u")
	if rawTarget == "" {
		http.Error(w, "missing u parameter", http.StatusBadRequest)
		return
	}
	keyURL, err := url.QueryUnescape(rawTarget)
	if err != nil {
		keyURL = rawTarget
	}

	req, err := http.NewRequestWithContext(r.Context(), "GET", keyURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.setHeaders(req, session)

	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (p *AndroidStreamProxy) serveHLSInitMap(w http.ResponseWriter, r *http.Request, session *androidStreamSession) {
	rawTarget := r.URL.Query().Get("u")
	if rawTarget == "" {
		http.Error(w, "missing u parameter", http.StatusBadRequest)
		return
	}
	mapURL, err := url.QueryUnescape(rawTarget)
	if err != nil {
		mapURL = rawTarget
	}

	req, err := http.NewRequestWithContext(r.Context(), "GET", mapURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.setHeaders(req, session)

	if rangeHdr := r.Header.Get("Range"); rangeHdr != "" {
		req.Header.Set("Range", rangeHdr)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		w.Header().Set("Content-Range", cr)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (p *AndroidStreamProxy) handleFile(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/file/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	sessionID := parts[0]
	endpoint := parts[1]

	p.mu.RLock()
	session, ok := p.sessions[sessionID]
	p.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	if endpoint == "sub.vtt" {
		p.serveHLSSubtitleFile(w, r, session)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, session.targetURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.setHeaders(req, session)

	if rangeHdr := r.Header.Get("Range"); rangeHdr != "" {
		req.Header.Set("Range", rangeHdr)
	}
	if ifRange := r.Header.Get("If-Range"); ifRange != "" {
		req.Header.Set("If-Range", ifRange)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for _, h := range []string{"Content-Type", "Content-Range", "Content-Length", "Accept-Ranges", "ETag", "Last-Modified"} {
		if val := resp.Header.Get(h); val != "" {
			w.Header().Set(h, val)
		}
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (p *AndroidStreamProxy) serveHLSSubtitlePlaylist(w http.ResponseWriter, r *http.Request, session *androidStreamSession) {
	playlist := fmt.Sprintf("#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:7200\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:7200.0,\n/hls/%s/sub.vtt\n#EXT-X-ENDLIST\n", session.id)
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(playlist))
}

func (p *AndroidStreamProxy) serveHLSSubtitleFile(w http.ResponseWriter, r *http.Request, session *androidStreamSession) {
	p.mu.Lock()
	if len(session.convertedSub) > 0 {
		cached := session.convertedSub
		p.mu.Unlock()
		w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cached)
		return
	}
	p.mu.Unlock()

	if strings.TrimSpace(session.subtitleURL) == "" {
		http.NotFound(w, r)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), "GET", session.subtitleURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.setHeaders(req, session)

	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("fetch subtitle: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("upstream subtitle status %d", resp.StatusCode), resp.StatusCode)
		return
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	converted := convertSubtitleToWebVTT(raw)

	p.mu.Lock()
	session.convertedSub = converted
	p.mu.Unlock()

	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(converted)
}

func (p *AndroidStreamProxy) rewritePlaylist(content, baseURL, sessionID, subtitleURL string) string {
	lines := strings.Split(content, "\n")
	var out []string

	isMaster := strings.Contains(content, "#EXT-X-STREAM-INF")
	hasSubtitles := isMaster && strings.TrimSpace(subtitleURL) != ""
	subInjected := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			out = append(out, line)
			continue
		}

		if strings.HasPrefix(trimmed, "#EXT-X-KEY:") && strings.Contains(trimmed, "URI=\"") {
			line = p.rewriteTagURI(trimmed, baseURL, sessionID, "key")
			out = append(out, line)
			continue
		}

		if strings.HasPrefix(trimmed, "#EXT-X-MAP:") && strings.Contains(trimmed, "URI=\"") {
			line = p.rewriteTagURI(trimmed, baseURL, sessionID, "map.mp4")
			out = append(out, line)
			continue
		}

		if strings.HasPrefix(trimmed, "#EXT-X-MEDIA:") && strings.Contains(trimmed, "URI=\"") {
			if strings.Contains(trimmed, "TYPE=SUBTITLES") && hasSubtitles {
				// Demote existing subtitle tracks so our configured soft subs take primary selection
				line = strings.Replace(line, "DEFAULT=YES", "DEFAULT=NO", 1)
				line = strings.Replace(line, "AUTOSELECT=YES", "AUTOSELECT=NO", 1)
			}
			line = p.rewriteTagURI(trimmed, baseURL, sessionID, "variant.m3u8")
			out = append(out, line)
			continue
		}

		if strings.HasPrefix(trimmed, "#EXT-X-STREAM-INF") {
			if hasSubtitles && !subInjected {
				subTag := fmt.Sprintf(`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="curd-subs",NAME="English",DEFAULT=YES,AUTOSELECT=YES,FORCED=NO,LANGUAGE="en",URI="/hls/%s/sub.m3u8"`, sessionID)
				out = append(out, subTag)
				subInjected = true
			}
			if hasSubtitles {
				if hlsSubGroupRegex.MatchString(line) {
					line = hlsSubGroupRegex.ReplaceAllString(line, `SUBTITLES="curd-subs"`)
				} else {
					line = line + `,SUBTITLES="curd-subs"`
				}
			}
			out = append(out, line)
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			out = append(out, line)
			continue
		}

		resolved := resolveRelativeURL(baseURL, trimmed)
		if isMaster {
			out = append(out, fmt.Sprintf("/hls/%s/variant.m3u8?u=%s", sessionID, url.QueryEscape(resolved)))
		} else {
			out = append(out, fmt.Sprintf("/hls/%s/seg.ts?u=%s", sessionID, url.QueryEscape(resolved)))
		}
	}

	return strings.Join(out, "\n")
}

func formatASSTimestamp(ts string) string {
	parts := strings.Split(strings.TrimSpace(ts), ":")
	if len(parts) != 3 {
		return ts
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return ts
	}
	secParts := strings.Split(parts[2], ".")
	if len(secParts) != 2 {
		return ts
	}
	cs := secParts[1]
	ms := cs
	if len(cs) == 1 {
		ms = cs + "00"
	} else if len(cs) == 2 {
		ms = cs + "0"
	} else if len(cs) > 3 {
		ms = cs[:3]
	}
	return fmt.Sprintf("%02d:%s:%02s.%s", h, parts[1], secParts[0], ms)
}

func convertSubtitleToWebVTT(raw []byte) []byte {
	// Strip UTF-8 BOM
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	content := strings.ReplaceAll(string(raw), "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	trimmed := strings.TrimSpace(content)

	if strings.HasPrefix(trimmed, "WEBVTT") {
		if !strings.Contains(trimmed, "X-TIMESTAMP-MAP=") {
			firstLineEnd := strings.Index(trimmed, "\n")
			if firstLineEnd == -1 {
				return []byte("WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:0\n")
			}
			return []byte(trimmed[:firstLineEnd] + "\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:0" + trimmed[firstLineEnd:])
		}
		return []byte(trimmed)
	}

	// Check if ASS / SSA
	if strings.Contains(content, "[Script Info]") || strings.Contains(content, "[Events]") || strings.Contains(content, "Dialogue:") {
		var out strings.Builder
		out.WriteString("WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:0\n\n")

		lines := strings.Split(content, "\n")
		var startIndex, endIndex, textIndex int = 1, 2, 9
		formatFound := false

		for _, line := range lines {
			lineTrim := strings.TrimSpace(line)
			if strings.HasPrefix(lineTrim, "Format:") {
				formatCols := strings.Split(strings.TrimPrefix(lineTrim, "Format:"), ",")
				for idx, col := range formatCols {
					c := strings.ToLower(strings.TrimSpace(col))
					switch c {
					case "start":
						startIndex = idx
					case "end":
						endIndex = idx
					case "text":
						textIndex = idx
					}
				}
				formatFound = true
				continue
			}

			if strings.HasPrefix(lineTrim, "Dialogue:") {
				rest := strings.TrimPrefix(lineTrim, "Dialogue:")
				cols := strings.SplitN(rest, ",", textIndex+1)
				if len(cols) <= textIndex {
					continue
				}
				rawStart := strings.TrimSpace(cols[startIndex])
				rawEnd := strings.TrimSpace(cols[endIndex])
				rawText := cols[textIndex]

				startVTT := formatASSTimestamp(rawStart)
				endVTT := formatASSTimestamp(rawEnd)

				cleanText := assTagRegex.ReplaceAllString(rawText, "")
				cleanText = strings.ReplaceAll(cleanText, `\N`, "\n")
				cleanText = strings.ReplaceAll(cleanText, `\n`, "\n")
				cleanText = strings.ReplaceAll(cleanText, `\h`, " ")
				cleanText = strings.TrimSpace(cleanText)

				if cleanText != "" {
					out.WriteString(fmt.Sprintf("%s --> %s\n%s\n\n", startVTT, endVTT, cleanText))
				}
			}
		}
		if formatFound || out.Len() > 60 {
			return []byte(out.String())
		}
	}

	// Check if SRT
	if srtTimeRegex.MatchString(content) {
		converted := srtTimeRegex.ReplaceAllString(content, "$1.$2")
		return []byte("WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:0\n\n" + converted)
	}

	// Fallback
	return []byte("WEBVTT\nX-TIMESTAMP-MAP=LOCAL:00:00:00.000,MPEGTS:0\n\n" + trimmed)
}

func (p *AndroidStreamProxy) rewriteTagURI(tagLine, baseURL, sessionID, endpoint string) string {
	uriIdx := strings.Index(tagLine, "URI=\"")
	if uriIdx == -1 {
		return tagLine
	}
	start := uriIdx + 5
	end := strings.Index(tagLine[start:], "\"")
	if end == -1 {
		return tagLine
	}
	rawURI := tagLine[start : start+end]
	resolved := resolveRelativeURL(baseURL, rawURI)
	proxied := fmt.Sprintf("/hls/%s/%s?u=%s", sessionID, endpoint, url.QueryEscape(resolved))
	return tagLine[:start] + proxied + tagLine[start+end:]
}

func (p *AndroidStreamProxy) setHeaders(req *http.Request, session *androidStreamSession) {
	req.Header.Set("User-Agent", session.userAgent)
	if session.referrer != "" {
		req.Header.Set("Referer", session.referrer)
	}
	if session.origin != "" {
		req.Header.Set("Origin", session.origin)
	}
}

func resolveRelativeURL(baseURL, target string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return target
	}
	rel, err := url.Parse(target)
	if err != nil {
		return target
	}
	return base.ResolveReference(rel).String()
}

func randomSessionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func resetAndroidProxyForTest() {
	androidProxyMu.Lock()
	defer androidProxyMu.Unlock()
	if androidProxyServer != nil && androidProxyServer.listener != nil {
		_ = androidProxyServer.listener.Close()
	}
	androidProxyServer = nil
}
