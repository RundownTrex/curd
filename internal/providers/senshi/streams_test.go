package senshi

import (
	"testing"

	"github.com/wraient/curd/internal/providers"
)

func TestLiveFetchSenshiStreams(t *testing.T) {
	p := &Provider{}
	opts, err := p.SearchAnime("one piece", "sub")
	if err != nil {
		t.Fatalf("SearchAnime failed: %v", err)
	}
	if len(opts) == 0 {
		t.Fatalf("SearchAnime returned 0 options")
	}

	showID := opts[0].Key
	t.Logf("Found anime: %s (MAL ID: %s)", opts[0].Title, showID)

	eps, err := p.EpisodesList(showID, "sub")
	if err != nil {
		t.Fatalf("EpisodesList failed: %v", err)
	}
	if len(eps) == 0 {
		t.Fatalf("EpisodesList returned 0 episodes")
	}

	links, hints, err := getEpisodeStreamsForMode(showID, providers.PlaybackConfig{SubOrDub: "sub"}, 1)
	if err != nil {
		t.Fatalf("getEpisodeStreamsForMode failed: %v", err)
	}
	if len(links) == 0 {
		t.Fatalf("No stream links returned")
	}

	t.Logf("Fetched stream URL: %s", links[0])
	if hint, ok := hints[links[0]]; ok {
		t.Logf("Stream hint referrer: %s, subtitle: %s", hint.Referrer, hint.Subtitle)
	}
}
