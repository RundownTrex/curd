package megaplay

import (
	"strings"
	"testing"

	"github.com/wraient/curd/internal/providers"
)

func TestDecryptMegaPlayEnc(t *testing.T) {
	// Sample ciphertext captured from megaplay.buzz getSources
	sampleEnc := "wdeBruh3qqn_i5wUNnyaPcXqidp1UWP84FfPHzGyKXAz4mAVkH6j3DueswO2yXLWn8H-XMHNvbAo5Gsg7zIcFBuQI_zsUvMGI1gKwQsPTSHQHiF55R4BopgEQ-7jebQQ4C0Gu7YhaMucopp6d3Q8yAY9b5GdsSvPGq6CUn7SHyc"

	streamURL, err := decryptMegaPlayEnc(sampleEnc)
	if err != nil {
		t.Fatalf("unexpected error decrypting sample enc: %v", err)
	}

	expectedPrefix := "https://fetch.nexabloom.top/anime/"
	if !strings.HasPrefix(streamURL, expectedPrefix) {
		t.Fatalf("expected stream url to start with %q, got %q", expectedPrefix, streamURL)
	}
	if !strings.HasSuffix(streamURL, ".m3u8") {
		t.Fatalf("expected stream url to end with .m3u8, got %q", streamURL)
	}
}

func TestDecryptMegaPlayEnc_Invalid(t *testing.T) {
	_, err := decryptMegaPlayEnc("")
	if err == nil {
		t.Fatal("expected error for empty enc payload")
	}

	_, err = decryptMegaPlayEnc("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}

	_, err = decryptMegaPlayEnc("dGVzdA==") // "test", length 4 bytes, not multiple of 16
	if err == nil {
		t.Fatal("expected error for non-multiple-of-blocksize ciphertext")
	}
}

func TestLiveMegaPlayFlow(t *testing.T) {
	p := New()

	// 1. Search for anime
	options, err := p.SearchAnime("Frieren", "sub")
	if err != nil {
		t.Fatalf("SearchAnime failed: %v", err)
	}
	if len(options) == 0 {
		t.Fatal("expected at least one search result")
	}

	first := options[0]
	t.Logf("Found anime: Key=%s, Title=%s, Label=%s", first.Key, first.Title, first.Label)

	// 2. Fetch episode list
	episodes, err := p.EpisodesList(first.Key, "sub")
	if err != nil {
		t.Fatalf("EpisodesList failed: %v", err)
	}
	if len(episodes) == 0 {
		t.Fatal("expected at least one episode")
	}
	t.Logf("Found %d episodes", len(episodes))

	// 3. Resolve stream for episode 1
	config := providers.PlaybackConfig{
		SubOrDub: "sub",
	}
	urls, hints, err := p.GetEpisodeURLForModeWithHints(config, first.Key, 1, "sub")
	if err != nil {
		t.Fatalf("GetEpisodeURLForModeWithHints failed: %v", err)
	}
	if len(urls) == 0 {
		t.Fatal("expected at least one stream URL")
	}

	streamURL := urls[0]
	t.Logf("Resolved stream: %s", streamURL)
	if !strings.HasSuffix(streamURL, ".m3u8") {
		t.Fatalf("expected .m3u8 stream, got %s", streamURL)
	}

	hint, ok := hints[streamURL]
	if !ok {
		t.Fatal("expected playback hint for resolved stream")
	}
	t.Logf("Hint: Referrer=%s, Subtitle=%s", hint.Referrer, hint.Subtitle)
	if hint.Subtitle == "" {
		t.Log("Note: No subtitle track returned (or hardsub)")
	}
}
