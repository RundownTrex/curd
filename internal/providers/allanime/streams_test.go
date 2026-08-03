package allanime

import (
	"testing"

	"github.com/wraient/curd/internal/providers"
)

func TestLiveFetchAnidbSearch(t *testing.T) {
	opts, err := searchAllAnime("one piece", "sub")
	if err != nil {
		t.Fatalf("searchAllAnime failed: %v", err)
	}
	if len(opts) == 0 {
		t.Fatal("searchAllAnime returned 0 results")
	}
	t.Logf("Search returned %d anime options. First: %s (Key: %s)", len(opts), opts[0].Title, opts[0].Key)
}

func TestLiveFetchAnidbEpisodes(t *testing.T) {
	eps, err := getAllAnimeEpisodesList("one-piece-3880", "sub")
	if err != nil {
		t.Fatalf("getAllAnimeEpisodesList failed: %v", err)
	}
	if len(eps) == 0 {
		t.Fatal("getAllAnimeEpisodesList returned 0 episodes")
	}
	t.Logf("Fetched %d episodes for One Piece. First: %s, Last: %s", len(eps), eps[0], eps[len(eps)-1])
}

func TestLiveFetchAnidbStreams(t *testing.T) {
	p := &Provider{}
	links, hints, err := p.GetEpisodeURLForModeWithHints(providers.PlaybackConfig{SubOrDub: "sub"}, "one-piece-3880", 1, "sub")
	if err != nil {
		t.Fatalf("GetEpisodeURLForModeWithHints failed: %v", err)
	}
	if len(links) == 0 {
		t.Fatal("GetEpisodeURLForModeWithHints returned 0 links")
	}
	t.Logf("Fetched %d playable stream links! First link: %s (referrer: %s)", len(links), links[0], hints[links[0]].Referrer)
}
