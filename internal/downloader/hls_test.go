package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSelectBestVariantAndAudio(t *testing.T) {
	masterContent := `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="English",LANGUAGE="eng",URI="audio_en.m3u8"
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="Japanese",DEFAULT=YES,LANGUAGE="jpn",URI="audio_jp.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=1280000,RESOLUTION=1280x720,AUDIO="stereo"
720p.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2560000,RESOLUTION=1920x1080,AUDIO="stereo"
1080p.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=854x480,AUDIO="stereo"
480p.m3u8
`
	baseURL, err := url.Parse("https://cdn.example.com/stream/master.m3u8")
	if err != nil {
		t.Fatalf("url parse failed: %v", err)
	}

	// 1. Test best quality (1080p)
	best, audioURI, err := selectBestVariant(baseURL, masterContent, "best")
	if err != nil {
		t.Fatalf("selectBestVariant failed: %v", err)
	}

	if best.URL != "https://cdn.example.com/stream/1080p.m3u8" {
		t.Errorf("expected 1080p variant, got %q", best.URL)
	}
	if audioURI != "https://cdn.example.com/stream/audio_jp.m3u8" {
		t.Errorf("expected default Japanese audio URI, got %q", audioURI)
	}

	// 2. Test 720p preferred quality
	v720, _, err := selectBestVariant(baseURL, masterContent, "720p")
	if err != nil {
		t.Fatalf("selectBestVariant 720p failed: %v", err)
	}
	if v720.URL != "https://cdn.example.com/stream/720p.m3u8" {
		t.Errorf("expected 720p variant, got %q", v720.URL)
	}
}

func TestParseMediaPlaylist(t *testing.T) {
	mediaContent := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:6
#EXTINF:4.000,
seg0.ts
#EXTINF:5.000,
/absolute/seg1.ts
#EXTINF:3.500,
https://cdn.other.com/seg2.ts
`
	baseURL, _ := url.Parse("https://cdn.example.com/hls/media.m3u8")
	playlist, err := parseMediaPlaylist(context.Background(), nil, baseURL, mediaContent, nil)
	if err != nil {
		t.Fatalf("parseMediaPlaylist failed: %v", err)
	}

	if len(playlist.Segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(playlist.Segments))
	}

	if playlist.Segments[0].URL != "https://cdn.example.com/hls/seg0.ts" {
		t.Errorf("expected segment 0 URL to resolve relatively, got %q", playlist.Segments[0].URL)
	}
	if playlist.Segments[1].URL != "https://cdn.example.com/absolute/seg1.ts" {
		t.Errorf("expected segment 1 URL to resolve root-relatively, got %q", playlist.Segments[1].URL)
	}
	if playlist.Segments[2].URL != "https://cdn.other.com/seg2.ts" {
		t.Errorf("expected segment 2 absolute URL, got %q", playlist.Segments[2].URL)
	}
}

func TestFetchHLSPlaylistWithSeparateAudio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			w.Write([]byte(`#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="stereo",NAME="Japanese",DEFAULT=YES,LANGUAGE="jpn",URI="audio.m3u8"
#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=1920x1080,AUDIO="stereo"
video.m3u8
`))
		case "/video.m3u8":
			w.Write([]byte(`#EXTM3U
#EXT-X-TARGETDURATION:4
#EXTINF:4.000,
v_chunk1.ts
#EXTINF:4.000,
v_chunk2.ts
`))
		case "/audio.m3u8":
			w.Write([]byte(`#EXTM3U
#EXT-X-TARGETDURATION:4
#EXTINF:4.000,
a_chunk1.ts
#EXTINF:4.000,
a_chunk2.ts
`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	playlist, err := FetchHLSPlaylist(context.Background(), server.Client(), server.URL+"/master.m3u8", nil, "best")
	if err != nil {
		t.Fatalf("FetchHLSPlaylist failed: %v", err)
	}

	if len(playlist.Segments) != 2 {
		t.Fatalf("expected 2 video segments, got %d", len(playlist.Segments))
	}
	if len(playlist.AudioSegments) != 2 {
		t.Fatalf("expected 2 audio segments, got %d", len(playlist.AudioSegments))
	}
	if playlist.AudioSegments[0].URL != server.URL+"/a_chunk1.ts" {
		t.Errorf("expected audio chunk URL to resolve properly, got %q", playlist.AudioSegments[0].URL)
	}
}
