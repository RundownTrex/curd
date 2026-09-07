package internal

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAndroidStreamProxy_HLSMasterPlaylist(t *testing.T) {
	resetAndroidProxyForTest()
	defer resetAndroidProxyForTest()

	expectedReferer := "https://example.com/anime/watch"
	expectedOrigin := "https://example.com"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != expectedReferer {
			http.Error(w, "missing or invalid referer", http.StatusForbidden)
			return
		}
		if r.Header.Get("Origin") != expectedOrigin {
			http.Error(w, "missing or invalid origin", http.StatusForbidden)
			return
		}

		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1280000,RESOLUTION=1280x720
720p/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2560000,RESOLUTION=1920x1080
1080p/index.m3u8`))
	}))
	defer server.Close()

	proxy, err := GetAndroidStreamProxy()
	if err != nil {
		t.Fatalf("failed to get proxy: %v", err)
	}

	proxiedURL, err := proxy.Register(server.URL+"/master.m3u8", expectedReferer, expectedOrigin, "")
	if err != nil {
		t.Fatalf("failed to register stream: %v", err)
	}

	if !strings.HasPrefix(proxiedURL, proxy.baseURL) {
		t.Fatalf("expected proxied URL to start with %s, got: %s", proxy.baseURL, proxiedURL)
	}

	resp, err := http.Get(proxiedURL)
	if err != nil {
		t.Fatalf("failed to fetch proxied master: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from proxy, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	content := string(body)
	if !strings.Contains(content, "/variant.m3u8?u=") {
		t.Errorf("expected rewritten variant URLs in master playlist, got:\n%s", content)
	}
}

func TestAndroidStreamProxy_HLSVariantAndKrussdomiJpgSegments(t *testing.T) {
	resetAndroidProxyForTest()
	defer resetAndroidProxyForTest()

	expectedReferer := "https://krussdomi.com/watch"
	expectedOrigin := "https://krussdomi.com"
	rawVideoBytes := []byte("FAKE_MPEG_TS_PAYLOAD_CHUNK_DATA")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != expectedReferer || r.Header.Get("Origin") != expectedOrigin {
			http.Error(w, "forbidden without cdn headers", http.StatusForbidden)
			return
		}

		switch r.URL.Path {
		case "/1080p/index.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXTINF:9.009,
seg-001.jpg
#EXTINF:9.009,
seg-002.jpg
#EXT-X-ENDLIST`))
		case "/1080p/seg-001.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(rawVideoBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	proxy, err := GetAndroidStreamProxy()
	if err != nil {
		t.Fatalf("failed to get proxy: %v", err)
	}

	proxiedURL, err := proxy.Register(server.URL+"/1080p/index.m3u8", expectedReferer, expectedOrigin, "")
	if err != nil {
		t.Fatalf("failed to register stream: %v", err)
	}

	// 1. Fetch variant playlist through proxy
	resp, err := http.Get(proxiedURL)
	if err != nil {
		t.Fatalf("failed to fetch variant: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from variant fetch, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read variant: %v", err)
	}

	content := string(body)
	if !strings.Contains(content, "/seg.ts?u=") {
		t.Fatalf("expected segments to be rewritten with .ts extension, got:\n%s", content)
	}

	// Extract the first segment URL
	var segPath string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/hls/") && strings.Contains(line, "/seg.ts?u=") {
			segPath = line
			break
		}
	}
	if segPath == "" {
		t.Fatalf("failed to find rewritten segment URL in:\n%s", content)
	}

	// 2. Fetch segment chunk through proxy
	segResp, err := http.Get(proxy.baseURL + segPath)
	if err != nil {
		t.Fatalf("failed to fetch segment chunk: %v", err)
	}
	defer segResp.Body.Close()

	if segResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for segment fetch, got %d", segResp.StatusCode)
	}
	if ct := segResp.Header.Get("Content-Type"); ct != "video/mp2t" {
		t.Errorf("expected Content-Type video/mp2t for disguised .jpg segment, got %q", ct)
	}

	segBody, err := io.ReadAll(segResp.Body)
	if err != nil {
		t.Fatalf("failed to read segment body: %v", err)
	}
	if string(segBody) != string(rawVideoBytes) {
		t.Errorf("segment payload mismatch: expected %q, got %q", string(rawVideoBytes), string(segBody))
	}
}

func TestAndroidStreamProxy_DirectFileWithRange(t *testing.T) {
	resetAndroidProxyForTest()
	defer resetAndroidProxyForTest()

	expectedReferer := "https://direct.cdn/embed"
	fileData := []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != expectedReferer {
			http.Error(w, "missing referer", http.StatusForbidden)
			return
		}

		if rangeHdr := r.Header.Get("Range"); rangeHdr != "" {
			if rangeHdr == "bytes=10-19" {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 10-19/%d", len(fileData)))
				w.Header().Set("Content-Type", "video/mp4")
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(fileData[10:20])
				return
			}
		}

		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fileData)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fileData)
	}))
	defer server.Close()

	proxy, err := GetAndroidStreamProxy()
	if err != nil {
		t.Fatalf("failed to get proxy: %v", err)
	}

	proxiedURL, err := proxy.Register(server.URL+"/video.mp4", expectedReferer, "", "")
	if err != nil {
		t.Fatalf("failed to register file stream: %v", err)
	}

	// Test range request
	req, _ := http.NewRequest("GET", proxiedURL, nil)
	req.Header.Set("Range", "bytes=10-19")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("range request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206 Partial Content, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	expectedChunk := string(fileData[10:20])
	if string(body) != expectedChunk {
		t.Errorf("expected chunk %q, got %q", expectedChunk, string(body))
	}
}

func TestPrepareAndroidPlaybackURL(t *testing.T) {
	resetAndroidProxyForTest()
	defer resetAndroidProxyForTest()

	anime := &Anime{
		ProviderName: "kickassanime",
		Ep: Episode{
			StreamReferrer: "https://krussdomi.com/watch/ep-1",
		},
	}

	target := "https://krussdomi.com/hls/master.m3u8"
	proxied, err := PrepareAndroidPlaybackURL(anime, target)
	if err != nil {
		t.Fatalf("PrepareAndroidPlaybackURL error: %v", err)
	}

	if !strings.HasPrefix(proxied, "http://127.0.0.1:") {
		t.Errorf("expected localhost proxy URL, got: %s", proxied)
	}

	// Should not double-proxy already local URLs
	reProxied, err := PrepareAndroidPlaybackURL(anime, proxied)
	if err != nil {
		t.Fatalf("re-proxy error: %v", err)
	}
	if reProxied != proxied {
		t.Errorf("expected unchanged URL for local proxy stream, got: %s", reProxied)
	}
}

func TestAndroidStreamProxy_HLSMasterPlaylistWithSubtitles(t *testing.T) {
	resetAndroidProxyForTest()
	defer resetAndroidProxyForTest()

	expectedReferer := "https://example.com/watch"
	subContent := `WEBVTT

00:00:01.000 --> 00:00:05.000
Hello Anime Subtitles!
`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1280000,RESOLUTION=1280x720
720p/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2560000,RESOLUTION=1920x1080
1080p/index.m3u8`))
		case "/subs/eng.vtt":
			w.Header().Set("Content-Type", "text/vtt")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(subContent))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	proxy, err := GetAndroidStreamProxy()
	if err != nil {
		t.Fatalf("failed to get proxy: %v", err)
	}

	proxiedURL, err := proxy.Register(server.URL+"/master.m3u8", expectedReferer, "", server.URL+"/subs/eng.vtt")
	if err != nil {
		t.Fatalf("failed to register stream: %v", err)
	}

	// 1. Fetch proxied master playlist
	resp, err := http.Get(proxiedURL)
	if err != nil {
		t.Fatalf("failed to fetch proxied master: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read master: %v", err)
	}

	content := string(body)
	if !strings.Contains(content, `#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="curd-subs"`) {
		t.Errorf("expected injected subtitle track in master playlist, got:\n%s", content)
	}
	if !strings.Contains(content, `SUBTITLES="curd-subs"`) {
		t.Errorf("expected stream inf to reference curd-subs, got:\n%s", content)
	}
	if !strings.Contains(content, "/sub.m3u8") {
		t.Errorf("expected URI pointing to /sub.m3u8, got:\n%s", content)
	}

	// 2. Fetch subtitle playlist
	subPlaylistURL := proxy.baseURL + fmt.Sprintf("/hls/%s/sub.m3u8", strings.Split(proxiedURL, "/")[4])
	subResp, err := http.Get(subPlaylistURL)
	if err != nil {
		t.Fatalf("failed to fetch subtitle playlist: %v", err)
	}
	defer subResp.Body.Close()

	if subResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from sub playlist, got %d", subResp.StatusCode)
	}

	subPlayBody, err := io.ReadAll(subResp.Body)
	if err != nil {
		t.Fatalf("failed to read sub playlist: %v", err)
	}
	if !strings.Contains(string(subPlayBody), "/sub.vtt") {
		t.Errorf("expected sub playlist to reference /sub.vtt, got:\n%s", string(subPlayBody))
	}

	// 3. Fetch subtitle file through proxy
	subFileURL := proxy.baseURL + fmt.Sprintf("/hls/%s/sub.vtt", strings.Split(proxiedURL, "/")[4])
	subFileResp, err := http.Get(subFileURL)
	if err != nil {
		t.Fatalf("failed to fetch subtitle file: %v", err)
	}
	defer subFileResp.Body.Close()

	if subFileResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from sub file, got %d", subFileResp.StatusCode)
	}
	if ct := subFileResp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/vtt") {
		t.Errorf("expected Content-Type text/vtt, got %q", ct)
	}

	subFileBody, err := io.ReadAll(subFileResp.Body)
	if err != nil {
		t.Fatalf("failed to read subtitle body: %v", err)
	}
	if !strings.Contains(string(subFileBody), "Hello Anime Subtitles!") {
		t.Errorf("expected subtitle content, got:\n%s", string(subFileBody))
	}
	if !strings.Contains(string(subFileBody), "X-TIMESTAMP-MAP=") {
		t.Errorf("expected X-TIMESTAMP-MAP header in converted WebVTT, got:\n%s", string(subFileBody))
	}
}

func TestAndroidStreamProxy_HLSVariantWrappingWithSubtitles(t *testing.T) {
	resetAndroidProxyForTest()
	defer resetAndroidProxyForTest()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/single/index.m3u8":
			// A single media playlist without #EXT-X-STREAM-INF
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`#EXTM3U
#EXT-X-VERSION:3
#EXTINF:10.0,
seg-1.ts
#EXT-X-ENDLIST`))
		case "/subs/track.ass":
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[Script Info]
Title: Test
[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:01:00.00,0:01:05.50,Default,,0,0,0,,{\pos(10,20)}Converted from ASS!\NSecond Line
`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	proxy, err := GetAndroidStreamProxy()
	if err != nil {
		t.Fatalf("failed to get proxy: %v", err)
	}

	proxiedURL, err := proxy.Register(server.URL+"/single/index.m3u8", "", "", server.URL+"/subs/track.ass")
	if err != nil {
		t.Fatalf("failed to register stream: %v", err)
	}

	// 1. Fetch master playlist -> should wrap single media playlist
	resp, err := http.Get(proxiedURL)
	if err != nil {
		t.Fatalf("failed to fetch master wrapper: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	content := string(body)
	if !strings.Contains(content, "#EXT-X-STREAM-INF") {
		t.Errorf("expected wrapped master playlist with #EXT-X-STREAM-INF, got:\n%s", content)
	}
	if !strings.Contains(content, "/variant.m3u8?u=") {
		t.Errorf("expected variant URL in wrapper, got:\n%s", content)
	}
	if !strings.Contains(content, "#EXT-X-MEDIA:TYPE=SUBTITLES") {
		t.Errorf("expected subtitle definition in wrapper, got:\n%s", content)
	}

	// 2. Fetch converted ASS subtitle
	sessionID := strings.Split(proxiedURL, "/")[4]
	subFileURL := proxy.baseURL + fmt.Sprintf("/hls/%s/sub.vtt", sessionID)
	subFileResp, err := http.Get(subFileURL)
	if err != nil {
		t.Fatalf("failed to fetch converted subtitle: %v", err)
	}
	defer subFileResp.Body.Close()

	subData, err := io.ReadAll(subFileResp.Body)
	if err != nil {
		t.Fatalf("failed to read converted subtitle: %v", err)
	}

	subStr := string(subData)
	if !strings.HasPrefix(subStr, "WEBVTT") {
		t.Errorf("expected WEBVTT header, got:\n%s", subStr)
	}
	if !strings.Contains(subStr, "00:01:00.000 --> 00:01:05.500") {
		t.Errorf("expected converted timestamp, got:\n%s", subStr)
	}
	if !strings.Contains(subStr, "Converted from ASS!\nSecond Line") {
		t.Errorf("expected clean converted text without ASS tags, got:\n%s", subStr)
	}
}

func TestConvertSubtitleToWebVTT(t *testing.T) {
	// 1. Already WebVTT
	vttInput := []byte("WEBVTT\n\n00:00:01.000 --> 00:00:04.000\nHello\n")
	out := convertSubtitleToWebVTT(vttInput)
	if !strings.Contains(string(out), "X-TIMESTAMP-MAP=") || !strings.Contains(string(out), "Hello") {
		t.Errorf("WebVTT conversion issue: %s", string(out))
	}

	// 2. SRT input
	srtInput := []byte("1\n00:00:01,234 --> 00:00:04,567\nHello SRT\n")
	outSRT := convertSubtitleToWebVTT(srtInput)
	if !strings.Contains(string(outSRT), "00:00:01.234 --> 00:00:04.567") || !strings.HasPrefix(string(outSRT), "WEBVTT") {
		t.Errorf("SRT conversion issue: %s", string(outSRT))
	}

	// 3. ASS input
	assInput := []byte(`[Script Info]
[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:02:10.50,0:02:15.80,Default,,0,0,0,,{\b1}Bold{\b0} dialogue\Nnew line
`)
	outASS := convertSubtitleToWebVTT(assInput)
	if !strings.Contains(string(outASS), "00:02:10.500 --> 00:02:15.800") || !strings.Contains(string(outASS), "Bold dialogue\nnew line") {
		t.Errorf("ASS conversion issue: %s", string(outASS))
	}
}

