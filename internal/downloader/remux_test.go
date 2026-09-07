package downloader

import (
	"strings"
	"testing"
)

func TestSanitizeFileName(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"Frieren: Beyond Journey's End", "Frieren Beyond Journey's End"},
		{"Attack on Titan / Shingeki no Kyojin", "Attack on Titan  Shingeki no Kyojin"},
		{"Fate/stay night: Heaven's Feel - I. presage flower", "Fatestay night Heaven's Feel - I. presage flower"},
		{"", "download"},
		{"   Episode ?*!   ", "Episode !"},
	}

	for _, c := range cases {
		result := SanitizeFileName(c.input)
		if result != c.expected {
			t.Errorf("SanitizeFileName(%q) = %q, expected %q", c.input, result, c.expected)
		}
	}
}

func TestBuildTargetPaths(t *testing.T) {
	final, part, sub, err := BuildTargetPaths(".", "Naruto - Episode 01")
	if err != nil {
		t.Fatalf("BuildTargetPaths failed: %v", err)
	}

	if !strings.HasSuffix(final, "Naruto - Episode 01.mp4") {
		t.Errorf("expected final path to end in .mp4, got %q", final)
	}
	if !strings.Contains(part, ".Naruto - Episode 01.mp4.part") {
		t.Errorf("expected part path, got %q", part)
	}
	if !strings.HasSuffix(sub, "Naruto - Episode 01") {
		t.Errorf("expected sub base path, got %q", sub)
	}
}
