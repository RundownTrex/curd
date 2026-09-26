package hianime

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wraient/curd/internal/curdhost"
	"github.com/wraient/curd/internal/providers"
)

func withHiAnimeTestClient(t *testing.T, client *http.Client) {
	t.Helper()
	previous := curdhost.HTTPClient
	t.Cleanup(func() {
		curdhost.HTTPClient = previous
	})
	curdhost.HTTPClient = func() *http.Client { return client }
}

func TestProviderRegistration(t *testing.T) {
	p, err := providers.New("hianime")
	if err != nil {
		t.Fatalf("expected hianime to be registered, got error: %v", err)
	}
	if p.Name() != "hianime" {
		t.Errorf("expected name 'hianime', got %q", p.Name())
	}

	for _, alias := range []string{"hianime.to", "hianime.at", "hianimetv", "aniwatch"} {
		pAlias, err := providers.New(alias)
		if err != nil {
			t.Errorf("expected alias %q to resolve, got error: %v", alias, err)
		} else if pAlias.Name() != "hianime" {
			t.Errorf("alias %q resolved to %q, want 'hianime'", alias, pAlias.Name())
		}
	}
}

func TestExtractAnimeID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"frieren-beyond-journeys-end-481", "481"},
		{"one-piece-1", "1"},
		{"naruto-shippuden-600", "600"},
		{"481", "481"},
		{"solo-leveling-season-2-arise-from-the-shadow-84", "84"},
		{"no-numbers-here", "no-numbers-here"},
	}

	for _, tt := range tests {
		got := extractAnimeID(tt.input)
		if got != tt.want {
			t.Errorf("extractAnimeID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMatchEpisodeNumber(t *testing.T) {
	if !matchEpisodeNumber("1", 1) {
		t.Errorf("expected matchEpisodeNumber('1', 1) to be true")
	}
	if !matchEpisodeNumber("01", 1) {
		t.Errorf("expected matchEpisodeNumber('01', 1) to be true")
	}
	if !matchEpisodeNumber("1.0", 1) {
		t.Errorf("expected matchEpisodeNumber('1.0', 1) to be true")
	}
	if matchEpisodeNumber("2", 1) {
		t.Errorf("expected matchEpisodeNumber('2', 1) to be false")
	}
}

func TestDeobfuscateBlob(t *testing.T) {
	// JSON: {"src":"https://example.com/master.m3u8"}
	jsonText := `{"src":"https://example.com/master.m3u8"}`
	key := []byte(xorKey)
	xorBytes := make([]byte, len(jsonText))
	for i := range jsonText {
		xorBytes[i] = jsonText[i] ^ key[i%len(key)]
	}
	b64Blob := base64.StdEncoding.EncodeToString(xorBytes)

	decrypted, err := deobfuscateBlob(b64Blob)
	if err != nil {
		t.Fatalf("deobfuscateBlob failed: %v", err)
	}

	if string(decrypted) != jsonText {
		t.Errorf("got %q, want %q", string(decrypted), jsonText)
	}
}

func TestPickSubtitleTrack(t *testing.T) {
	tracks := []zokoSubtitle{
		{Lang: "ar", Label: "Arabic", Default: false, Src: "https://example.com/ar.vtt"},
		{Lang: "en", Label: "English", Default: true, Src: "https://example.com/en-default.vtt"},
		{Lang: "en", Label: "English (SDH)", Default: false, Src: "https://example.com/en-sdh.vtt"},
	}

	// In sub mode, should prefer default English track
	got := pickSubtitleTrack(tracks, "sub")
	if got != "https://example.com/en-default.vtt" {
		t.Errorf("pickSubtitleTrack(tracks, 'sub') = %q, want https://example.com/en-default.vtt", got)
	}

	// In dub mode, should return empty
	gotDub := pickSubtitleTrack(tracks, "dub")
	if gotDub != "" {
		t.Errorf("pickSubtitleTrack(tracks, 'dub') = %q, want empty string", gotDub)
	}
}

func TestSearchAnimeParsesResults(t *testing.T) {
	sampleHTML := `
<!DOCTYPE html>
<html>
<body>
<div class="film_list-wrap">
    <div class="flw-item flw-item-big">
        <div class="film-poster">
            <div class="tick ltr">
                <div class="tick-item tick-sub">28</div>
                <div class="tick-item tick-dub">28</div>
                <div class="tick-item tick-eps">28</div>
            </div>
            <img src="https://cdn.example.com/poster.jpg" class="film-poster-img" alt="Frieren">
        </div>
        <div class="film-detail">
            <h3 class="film-name">
                <a href="https://hianime.at/frieren-beyond-journeys-end-481"
                   title="Frieren: Beyond Journey&#039;s End"
                   class="dynamic-name"
                   data-jname="Sousou no Frieren">Frieren: Beyond Journey&#039;s End</a>
            </h3>
            <div class="fd-infor">
                <span class="fdi-item">TV</span>
            </div>
        </div>
    </div>
</div>
<div id="main-sidebar">
    <h3 class="film-name"><a href="/ignored-sidebar-item-999" title="Ignored">Ignored</a></h3>
</div>
</body>
</html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, sampleHTML)
	}))
	defer server.Close()

	withHiAnimeTestClient(t, server.Client())

	origBase := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = origBase })

	results, err := searchAnime("frieren", "sub")
	if err != nil {
		t.Fatalf("searchAnime failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	res := results[0]
	if res.Key != "frieren-beyond-journeys-end-481" {
		t.Errorf("expected Key 'frieren-beyond-journeys-end-481', got %q", res.Key)
	}
	if res.Title != "Frieren: Beyond Journey's End" {
		t.Errorf("expected Title 'Frieren: Beyond Journey\\'s End', got %q", res.Title)
	}
	if res.Thumbnail != "https://cdn.example.com/poster.jpg" {
		t.Errorf("expected Thumbnail 'https://cdn.example.com/poster.jpg', got %q", res.Thumbnail)
	}
}

func TestEpisodesListAndStreams(t *testing.T) {
	embedJSON := `{"src":"https://cdn.example.com/master.m3u8","subtitles":[{"lang":"en","label":"English","default":true,"src":"https://cdn.example.com/en.vtt"}]}`
	key := []byte(xorKey)
	xorBytes := make([]byte, len(embedJSON))
	for i := range embedJSON {
		xorBytes[i] = embedJSON[i] ^ key[i%len(key)]
	}
	b64Blob := base64.StdEncoding.EncodeToString(xorBytes)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/theme/episode/list/481":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"status":true,"totalItems":2,"html":"<div class=\"ss-list\"><a class=\"ep-item\" data-number=\"1\" data-id=\"9001\" href=\"/watch/frieren-481?ep=9001\"></a><a class=\"ep-item\" data-number=\"2\" data-id=\"9002\" href=\"/watch/frieren-481?ep=9002\"></a></div>"}`)
		case "/api/theme/episode/servers":
			w.Header().Set("Content-Type", "application/json")
			embedURL := server.URL + "/embed/zoko"
			b64Hash := base64.StdEncoding.EncodeToString([]byte(embedURL))
			html := fmt.Sprintf(`<div class="ps__-list"><div class="item server-item" data-type="sub" data-server-name="ZokoAnime" data-hash="%s"></div></div>`, b64Hash)
			_, _ = io.WriteString(w, fmt.Sprintf(`{"status":true,"html":%q}`, html))
		case "/embed/zoko":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, fmt.Sprintf(`<html><script>window.__P="%s"</script></html>`, b64Blob))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	withHiAnimeTestClient(t, server.Client())

	origBase := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = origBase })

	episodes, err := episodesList("frieren-beyond-journeys-end-481", "sub")
	if err != nil {
		t.Fatalf("episodesList failed: %v", err)
	}
	if len(episodes) != 2 || episodes[0] != "1" || episodes[1] != "2" {
		t.Fatalf("unexpected episodes list: %v", episodes)
	}

	links, hints, err := getEpisodeStreamsForMode("frieren-beyond-journeys-end-481", providers.PlaybackConfig{SubOrDub: "sub"}, 1)
	if err != nil {
		t.Fatalf("getEpisodeStreamsForMode failed: %v", err)
	}

	if len(links) != 1 || links[0] != "https://cdn.example.com/master.m3u8" {
		t.Errorf("unexpected links: %v", links)
	}
	hint, ok := hints["https://cdn.example.com/master.m3u8"]
	if !ok {
		t.Fatalf("missing hint for stream URL")
	}
	if hint.Subtitle != "https://cdn.example.com/en.vtt" {
		t.Errorf("expected subtitle hint 'https://cdn.example.com/en.vtt', got %q", hint.Subtitle)
	}
}

func TestLiveHiAnimeFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in short mode")
	}

	if curdhost.HTTPClient == nil {
		curdhost.HTTPClient = func() *http.Client { return &http.Client{} }
	}

	p, err := providers.New("hianime")
	if err != nil {
		t.Fatalf("failed to get hianime provider: %v", err)
	}

	// 1. Search anime
	options, err := p.SearchAnime("frieren", "sub")
	if err != nil {
		t.Fatalf("live search failed: %v", err)
	}
	if len(options) == 0 {
		t.Fatalf("expected search results for 'frieren', got 0")
	}

	slug := options[0].Key
	if slug == "" {
		t.Fatalf("expected non-empty key in search result")
	}

	// 2. Episode list
	episodes, err := p.EpisodesList(slug, "sub")
	if err != nil {
		t.Fatalf("live episode list failed for %s: %v", slug, err)
	}
	if len(episodes) == 0 {
		t.Fatalf("expected episodes for %s, got 0", slug)
	}

	// 3. Resolve episode 1 stream
	links, err := p.GetEpisodeURL(providers.PlaybackConfig{SubOrDub: "sub"}, slug, 1)
	if err != nil {
		t.Fatalf("live stream resolution failed: %v", err)
	}
	if len(links) == 0 {
		t.Fatalf("expected at least 1 stream link for episode 1")
	}
}

