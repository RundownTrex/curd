package senshi

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

var (
	senshiProxyMu     sync.Mutex
	senshiProxyServer *senshiProxy
)

type senshiSession struct {
	playlistURL string
	referer     string
	segments    map[int]string
}

type senshiProxy struct {
	baseURL  string
	server   *http.Server
	sessions map[string]*senshiSession
}

func registerSenshiStream(playlistURL, referer string) (string, error) {
	proxy, err := getSenshiProxy()
	if err != nil {
		return "", err
	}
	return proxy.register(playlistURL, referer)
}

func getSenshiProxy() (*senshiProxy, error) {
	senshiProxyMu.Lock()
	defer senshiProxyMu.Unlock()

	if senshiProxyServer != nil {
		return senshiProxyServer, nil
	}

	proxy := &senshiProxy{sessions: map[string]*senshiSession{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/stream/", proxy.handle)
	server := &http.Server{Handler: mux}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start senshi proxy listener: %w", err)
	}
	proxy.server = server
	proxy.baseURL = "http://" + listener.Addr().String()

	go func() {
		_ = server.Serve(listener)
	}()

	senshiProxyServer = proxy
	return proxy, nil
}

func (p *senshiProxy) register(playlistURL, referer string) (string, error) {
	id, err := randomSenshiSessionID()
	if err != nil {
		return "", err
	}
	senshiProxyMu.Lock()
	p.sessions[id] = &senshiSession{
		playlistURL: playlistURL,
		referer:     referer,
		segments:    map[int]string{},
	}
	senshiProxyMu.Unlock()
	return p.baseURL + "/stream/" + id + "/playlist.m3u8", nil
}

func (p *senshiProxy) handle(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/stream/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	sessionID := parts[0]

	senshiProxyMu.Lock()
	session, ok := p.sessions[sessionID]
	senshiProxyMu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch {
	case len(parts) == 2 && parts[1] == "playlist.m3u8":
		p.servePlaylist(w, sessionID, session)
	case len(parts) == 3 && parts[1] == "seg":
		p.serveSegment(w, r, session, parts[2])
	default:
		http.NotFound(w, r)
	}
}

func (p *senshiProxy) servePlaylist(w http.ResponseWriter, sessionID string, session *senshiSession) {
	req, err := http.NewRequest("GET", session.playlistURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("User-Agent", userAgent)
	if session.referer != "" {
		req.Header.Set("Referer", session.referer)
	} else {
		req.Header.Set("Referer", baseURL+"/")
	}

	resp, err := senshiHTTPClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("upstream returned %d", resp.StatusCode), resp.StatusCode)
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	playlistStr := string(bodyBytes)
	base, _ := url.Parse(session.playlistURL)

	segments := map[int]string{}
	segmentCounter := 0

	lines := strings.Split(strings.ReplaceAll(playlistStr, "\r\n", "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// This is a segment URL or path
		segmentURL := trimmed
		if u, err := base.Parse(trimmed); err == nil {
			segmentURL = u.String()
		}

		segmentIdx := segmentCounter
		segmentCounter++
		segments[segmentIdx] = segmentURL

		lines[i] = fmt.Sprintf("%s/stream/%s/seg/%d", p.baseURL, sessionID, segmentIdx)
	}

	senshiProxyMu.Lock()
	session.segments = segments
	senshiProxyMu.Unlock()

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	_, _ = io.WriteString(w, strings.Join(lines, "\n"))
}

func (p *senshiProxy) serveSegment(w http.ResponseWriter, r *http.Request, session *senshiSession, indexStr string) {
	idx, err := strconv.Atoi(indexStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	senshiProxyMu.Lock()
	segmentURL, ok := session.segments[idx]
	senshiProxyMu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	req, err := http.NewRequest("GET", segmentURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("User-Agent", userAgent)
	if session.referer != "" {
		req.Header.Set("Referer", session.referer)
	} else {
		req.Header.Set("Referer", baseURL+"/")
	}

	resp, err := senshiHTTPClient.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		http.Error(w, fmt.Sprintf("upstream segment returned %d", resp.StatusCode), resp.StatusCode)
		return
	}

	w.Header().Set("Content-Type", "video/mp2t")
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	_, _ = io.Copy(w, resp.Body)
}

func randomSenshiSessionID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
