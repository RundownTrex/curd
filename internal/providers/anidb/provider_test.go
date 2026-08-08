package anidb

import (
	"testing"

	"github.com/wraient/curd/internal/providers"
)

func TestAniDBProviderRegistration(t *testing.T) {
	provider, err := providers.New("anidb")
	if err != nil {
		t.Fatalf("expected anidb provider to be registered, got error: %v", err)
	}
	if provider.Name() != "anidb" {
		t.Errorf("expected provider name 'anidb', got '%s'", provider.Name())
	}
}

func TestParseAniDBSearchPage(t *testing.T) {
	sampleHTML := `
		<!DOCTYPE html>
		<html>
		<body>
			<a href="https://anidb.app/anime/naruto-3686" class="anime-card block group" title="Naruto">
				<img src="https://cdn.xlsbox.com/poster/small/1782735600/3686.jpg" alt="Naruto">
			</a>
			<a href="https://anidb.app/anime/naruto-shippuden-3687" class="anime-card block group" title="Naruto Shippuden">
				<img src="https://cdn.xlsbox.com/poster/small/1782735600/3687.jpg" alt="Naruto Shippuden">
			</a>
		</body>
		</html>
	`
	options := parseAniDBSearchPage(sampleHTML)
	if len(options) != 2 {
		t.Fatalf("expected 2 options, got %d", len(options))
	}

	if options[0].Key != "naruto-3686" || options[0].Title != "Naruto" {
		t.Errorf("unexpected option 0: %+v", options[0])
	}
	if options[1].Key != "naruto-shippuden-3687" || options[1].Title != "Naruto Shippuden" {
		t.Errorf("unexpected option 1: %+v", options[1])
	}
}

func TestExtractNumericID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"naruto-3686", "3686"},
		{"3686", "3686"},
		{"boruto-naruto-next-generations-744", "744"},
	}

	for _, tt := range tests {
		got := extractNumericID(tt.input)
		if got != tt.want {
			t.Errorf("extractNumericID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLiveAniDBSearch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in short mode")
	}
	p := &Provider{}
	results, err := p.SearchAnime("naruto", "sub")
	if err != nil {
		t.Fatalf("SearchAnime failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results for 'naruto', got 0")
	}
	t.Logf("Found %d search results for 'naruto'. First: %s (%s)", len(results), results[0].Title, results[0].Key)
}

func TestLiveAniDBEpisodes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in short mode")
	}
	p := &Provider{}
	episodes, err := p.EpisodesList("naruto-3686", "sub")
	if err != nil {
		t.Fatalf("EpisodesList failed: %v", err)
	}
	if len(episodes) == 0 {
		t.Fatal("expected episodes for naruto-3686, got 0")
	}
	t.Logf("Found %d episodes for naruto-3686", len(episodes))
}

func TestLiveAniDBStreams(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in short mode")
	}
	p := &Provider{}
	links, hints, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{SubOrDub: "sub"}, "naruto-3686", 1, "sub")
	if err != nil {
		t.Fatalf("GetEpisodeURLForModeWithHints failed: %v", err)
	}
	if len(links) == 0 {
		t.Fatal("expected stream links for naruto-3686 ep 1, got 0")
	}
	t.Logf("Resolved %d stream links for naruto-3686 ep 1: %v (hints: %+v)", len(links), links, hints)
}

func TestLiveAniDBSlimeSeason3Offset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live network test in short mode")
	}
	p := &Provider{}
	// Slime Season 3 (5235) has site numbers 49..72 (24 episodes total)
	episodes, err := p.EpisodesList("that-time-i-got-reincarnated-as-a-slime-season-3-5235", "sub")
	if err != nil {
		t.Fatalf("EpisodesList failed for Slime S3: %v", err)
	}
	if len(episodes) != 24 {
		t.Fatalf("expected 24 relative season episodes for Slime S3, got %d", len(episodes))
	}
	if episodes[0] != "1" || episodes[23] != "24" {
		t.Errorf("expected relative episodes 1..24, got first=%s last=%s", episodes[0], episodes[23])
	}
	t.Logf("Confirmed Slime S3 episode list has 24 relative season episodes (1..24)")

	// Test resolving relative Episode 14 (should resolve to site Ep 62 ID)
	linksEp14, _, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{SubOrDub: "sub"}, "that-time-i-got-reincarnated-as-a-slime-season-3-5235", 14, "sub")
	if err != nil {
		t.Fatalf("GetEpisodeURLForModeWithHints relative ep 14 failed: %v", err)
	}
	if len(linksEp14) == 0 {
		t.Fatal("expected stream links for Slime S3 ep 14, got 0")
	}
	t.Logf("Successfully resolved Slime S3 relative episode 14 stream links: %v", linksEp14)

	// Test resolving absolute Episode 62 directly
	linksEp62, _, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{SubOrDub: "sub"}, "that-time-i-got-reincarnated-as-a-slime-season-3-5235", 62, "sub")
	if err != nil {
		t.Fatalf("GetEpisodeURLForModeWithHints absolute ep 62 failed: %v", err)
	}
	if len(linksEp62) == 0 {
		t.Fatal("expected stream links for Slime S3 absolute ep 62, got 0")
	}
	t.Logf("Successfully resolved Slime S3 absolute episode 62 stream links: %v", linksEp62)
}
