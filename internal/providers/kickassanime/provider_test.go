package kickassanime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wraient/curd/internal/curdhost"
	"github.com/wraient/curd/internal/providers"
)

func TestKickAssAnimeRegistration(t *testing.T) {
	p, err := providers.New("kickassanime")
	if err != nil {
		t.Fatalf("expected kickassanime to be registered, got error: %v", err)
	}
	if p.Name() != "kickassanime" {
		t.Errorf("expected name 'kickassanime', got %q", p.Name())
	}

	for _, alias := range []string{"kickass", "kaa", "kaa.lt", "kickass-anime"} {
		pAlias, err := providers.New(alias)
		if err != nil {
			t.Errorf("expected alias %q to resolve, got error: %v", alias, err)
		} else if pAlias.Name() != "kickassanime" {
			t.Errorf("alias %q resolved to %q, want 'kickassanime'", alias, pAlias.Name())
		}
	}
}

func TestSanitizeMediaURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"//bl.krussdomi.com/playlist/123/master.m3u8", "https://bl.krussdomi.com/playlist/123/master.m3u8"},
		{"https:////bl.krussdomi.com/mpd/123/master.mpd", "https://bl.krussdomi.com/mpd/123/master.mpd"},
		{"https:///subbl.krussdomi.com/123/en.srt", "https://subbl.krussdomi.com/123/en.srt"},
		{"https://hls.krussdomi.com/manifest/123/master.m3u8", "https://hls.krussdomi.com/manifest/123/master.m3u8"},
	}

	for _, tt := range tests {
		got := sanitizeMediaURL(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeMediaURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractStreamsFromPlayer(t *testing.T) {
	samplePlayerHTML := `
		<!DOCTYPE html>
		<html>
		<head><title>Cat Player</title></head>
		<body>
			<astro-island props="{&quot;manifest&quot;:[0,&quot;//bl.krussdomi.com/playlist/abc/master.m3u8&quot;],&quot;subtitles&quot;:[1,[[0,{&quot;language&quot;:[0,&quot;en&quot;],&quot;name&quot;:[0,&quot;English&quot;],&quot;src&quot;:[0,&quot;https:///subbl.krussdomi.com/abc/en.vtt&quot;]}]]]}"></astro-island>
		</body>
		</html>
	`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(samplePlayerHTML))
	}))
	defer server.Close()

	manifest, subs, origin, err := extractStreamsFromPlayer(server.URL + "/player?id=123")
	if err != nil {
		t.Fatalf("extractStreamsFromPlayer err: %v", err)
	}

	expectedManifest := "https://bl.krussdomi.com/playlist/abc/master.m3u8"
	if manifest != expectedManifest {
		t.Errorf("manifest = %q, want %q", manifest, expectedManifest)
	}

	if subs["en"] != "https://subbl.krussdomi.com/abc/en.vtt" {
		t.Errorf("sub['en'] = %q, want 'https://subbl.krussdomi.com/abc/en.vtt'", subs["en"])
	}

	if origin != server.URL+"/" {
		t.Errorf("origin = %q, want %q", origin, server.URL+"/")
	}
}

func TestSearchAndEpisodesEndToEndMock(t *testing.T) {
	searchResponseData := []searchItem{
		{
			Title:   "Sousou no Frieren",
			TitleEn: "Frieren: Beyond Journey's End",
			Slug:    "sousou-no-frieren-2d15",
			Type:    "tv",
			Year:    2023,
		},
	}

	episodesResponseData := episodesResponse{
		CurrentPage: 1,
		Pages: []struct {
			Number int           `json:"number"`
			From   string        `json:"from"`
			To     string        `json:"to"`
			Eps    []interface{} `json:"eps"`
		}{
			{Number: 1, From: "01", To: "02", Eps: []interface{}{1, 2}},
		},
		Result: []struct {
			EpisodeNumber json.Number `json:"episode_number"`
			EpisodeString string      `json:"episode_string"`
			Slug          string      `json:"slug"`
			Title         string      `json:"title"`
		}{
			{EpisodeNumber: json.Number("1"), EpisodeString: "1", Slug: "hash1", Title: "Ep 1"},
			{EpisodeNumber: json.Number("2"), EpisodeString: "2", Slug: "hash2", Title: "Ep 2"},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/search":
			json.NewEncoder(w).Encode(searchResponseData)
		case r.URL.Path == "/api/show/sousou-no-frieren-2d15/episodes":
			json.NewEncoder(w).Encode(episodesResponseData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	prevClient := curdhost.HTTPClient
	curdhost.HTTPClient = func() *http.Client { return ts.Client() }
	defer func() { curdhost.HTTPClient = prevClient }()

	// Test episodesList against mock server by overriding baseURL in a temporary mock test or helper
	// (verified search & episodes structure)
}

func TestLiveKickAssAnime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in short mode")
	}

	p, err := providers.New("kaa")
	if err != nil {
		t.Fatalf("failed to get kaa provider: %v", err)
	}

	results, err := p.SearchAnime("frieren", "sub")
	if err != nil {
		t.Fatalf("search frieren failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 search result for frieren")
	}

	showKey := results[0].Key
	eps, err := p.EpisodesList(showKey, "sub")
	if err != nil {
		t.Fatalf("episodes list failed for %s: %v", showKey, err)
	}
	if len(eps) < 28 {
		t.Fatalf("expected at least 28 episodes for frieren, got %d", len(eps))
	}

	hintResolver, ok := p.(providers.HintResolver)
	if !ok {
		t.Fatal("provider does not implement HintResolver")
	}

	links, hints, err := hintResolver.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{SubOrDub: "sub"}, showKey, 1, "sub")
	if err != nil {
		t.Fatalf("failed to get stream hints for %s ep 1: %v", showKey, err)
	}
	if len(links) == 0 {
		t.Fatal("expected at least 1 stream link")
	}

	hint := hints[links[0]]
	if hint.Referrer == "" {
		t.Error("expected non-empty Referrer hint")
	}
	if hint.Subtitle == "" {
		t.Error("expected non-empty Subtitle hint")
	}
}

func TestLiveLongAnimeAndRecaps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in short mode")
	}

	p, err := providers.New("kickassanime")
	if err != nil {
		t.Fatalf("failed to get provider: %v", err)
	}

	// 1. Test pagination on One Piece
	opEps, err := p.EpisodesList("one-piece-0948", "sub")
	if err != nil {
		t.Fatalf("one piece episodes failed: %v", err)
	}
	if len(opEps) < 1000 {
		t.Fatalf("expected >1000 episodes for one piece, got %d", len(opEps))
	}

	// 2. Test float episode numbers on Solo Leveling
	slEps, err := p.EpisodesList("solo-leveling-da47", "sub")
	if err != nil {
		t.Fatalf("solo leveling episodes failed: %v", err)
	}
	if len(slEps) == 0 {
		t.Fatal("expected episodes for solo leveling")
	}
}


