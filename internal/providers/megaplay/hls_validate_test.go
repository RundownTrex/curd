package megaplay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wraient/curd/internal/curdhost"
)

func withTestClient(t *testing.T, client *http.Client) {
	t.Helper()
	previous := curdhost.HTTPClient
	t.Cleanup(func() { curdhost.HTTPClient = previous })
	curdhost.HTTPClient = func() *http.Client { return client }
}

func TestVariantPlaylistURLs(t *testing.T) {
	master := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1280000,RESOLUTION=640x360
360.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2560000,RESOLUTION=1280x720
https://cdn.example/720/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=5120000,RESOLUTION=1920x1080
/abs/1080.m3u8
`
	got := variantPlaylistURLs(master, "https://cdn.example/master.m3u8")
	want := []string{
		"https://cdn.example/360.m3u8",
		"https://cdn.example/720/index.m3u8",
		"https://cdn.example/abs/1080.m3u8",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected variant urls: %v", got)
	}
}

func TestCountAdSegments(t *testing.T) {
	playlist := `#EXTM3U
#EXTINF:6.0,
https://p16-ad-sg.ibyteimg.com/seg1.ts
#EXTINF:6.0,
https://cdn.example/seg2.ts
#EXTINF:6.0,
https://p16-ad.ibyteimg.com/seg3.ts
`
	ad, total := countAdSegments(playlist)
	if total != 3 || ad != 2 {
		t.Fatalf("expected 2/3 ad segments, got %d/%d", ad, total)
	}
}

func TestValidateHLSStreamAllVariantsInfected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\n360.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2000\n720.m3u8\n")
		case "/360.m3u8", "/720.m3u8":
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:6.0,\nhttps://p16-ad-sg.ibyteimg.com/seg.ts\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	withTestClient(t, server.Client())

	err := validateHLSStream(server.URL + "/master.m3u8")
	if err == nil {
		t.Fatal("expected error for all-ad variants")
	}
	if !strings.Contains(err.Error(), "variant playlists are ads") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateHLSStreamOneHealthyVariantPasses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\n360.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2000\n720.m3u8\n")
		case "/360.m3u8":
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:6.0,\nhttps://p16-ad-sg.ibyteimg.com/seg.ts\n")
		case "/720.m3u8":
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:6.0,\nhttps://cdn.example/seg.ts\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	withTestClient(t, server.Client())

	if err := validateHLSStream(server.URL + "/master.m3u8"); err != nil {
		t.Fatalf("expected healthy variant to pass, got: %v", err)
	}
}

func TestValidateHLSStreamUnverifiableVariantIsTolerated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=1000\n360.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=2000\n720.m3u8\n")
		case "/360.m3u8":
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:6.0,\nhttps://p16-ad-sg.ibyteimg.com/seg.ts\n")
		case "/720.m3u8":
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	withTestClient(t, server.Client())

	if err := validateHLSStream(server.URL + "/master.m3u8"); err != nil {
		t.Fatalf("expected unverifiable variant to be tolerated, got: %v", err)
	}
}

func TestValidateHLSStreamMediaPlaylist(t *testing.T) {
	allAds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:6.0,\nhttps://p16-ad-sg.ibyteimg.com/seg1.ts\n#EXTINF:6.0,\nhttps://p16-ad.ibyteimg.com/seg2.ts\n")
	}))
	defer allAds.Close()
	withTestClient(t, allAds.Client())

	if err := validateHLSStream(allAds.URL + "/media.m3u8"); err == nil {
		t.Fatal("expected error for all-ad media playlist")
	}

	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:6.0,\nhttps://cdn.example/seg.ts\n")
	}))
	defer healthy.Close()
	withTestClient(t, healthy.Client())

	if err := validateHLSStream(healthy.URL + "/media.m3u8"); err != nil {
		t.Fatalf("expected healthy media playlist to pass, got: %v", err)
	}
}

func TestValidateHLSStreamMasterFetchFailsIsTolerated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
	withTestClient(t, server.Client())

	if err := validateHLSStream(server.URL + "/master.m3u8"); err != nil {
		t.Fatalf("expected master fetch failure to be tolerated, got: %v", err)
	}
}
