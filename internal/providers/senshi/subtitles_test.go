package senshi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wraient/curd/internal/curdhost"
)

func TestParseSenshiSubtitleTracks(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []senshiSubtitleTrack
		wantErr bool
	}{
		{
			name: "filemoon array",
			raw:  `[{"src":"https://cdn.example/en.vtt","label":"English","default":true},{"src":"https://cdn.example/fr.vtt","label":"Francais"}]`,
			want: []senshiSubtitleTrack{
				{Src: "https://cdn.example/en.vtt", Label: "English", Default: true},
				{Src: "https://cdn.example/fr.vtt", Label: "Francais"},
			},
		},
		{
			name: "artplayer array with url/html/type",
			raw:  `[{"url":"https://cdn.example/en.ass","html":"English","type":"ass"},{"url":"https://cdn.example/es.vtt","html":"Español","type":"vtt"}]`,
			want: []senshiSubtitleTrack{
				{Src: "https://cdn.example/en.ass", Label: "English"},
				{Src: "https://cdn.example/es.vtt", Label: "Español"},
			},
		},
		{
			name: "wrapper object with tracks array",
			raw:  `{"tracks":[{"url":"https://cdn.example/en.ass","html":"English"}]}`,
			want: []senshiSubtitleTrack{
				{Src: "https://cdn.example/en.ass", Label: "English"},
			},
		},
		{
			name: "wrapper object with subtitles array",
			raw:  `{"subtitles":[{"file":"https://cdn.example/en.vtt","lang":"en"}]}`,
			want: []senshiSubtitleTrack{
				{Src: "https://cdn.example/en.vtt", Label: "en"},
			},
		},
		{
			name: "plain array of url strings",
			raw:  `["https://cdn.example/en.vtt","https://cdn.example/en.ass"]`,
			want: []senshiSubtitleTrack{
				{Src: "https://cdn.example/en.vtt"},
				{Src: "https://cdn.example/en.ass"},
			},
		},
		{
			name: "single track object",
			raw:  `{"src":"https://cdn.example/en.ass","label":"English","default":"default"}`,
			want: []senshiSubtitleTrack{
				{Src: "https://cdn.example/en.ass", Label: "English", Default: true},
			},
		},
		{
			name:    "garbage json",
			raw:     `not json at all`,
			wantErr: true,
		},
		{
			name:    "no recognizable tracks",
			raw:     `{"foo":"bar"}`,
			wantErr: true,
		},
		{
			name: "empty manifest",
			raw:  `[]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSenshiSubtitleTracks([]byte(tt.raw))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d tracks, got %d: %v", len(tt.want), len(got), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("track %d: expected %+v, got %+v", i, tt.want[i], got[i])
				}
			}
		})
	}
}

func TestSenshiSubtitleTrackSources(t *testing.T) {
	enFull := senshiSubtitleTrack{Src: "https://cdn.example/full.en.vtt", Label: "English"}
	enForced := senshiSubtitleTrack{Src: "https://cdn.example/forced.en.vtt", Label: "English (Forced)"}
	enSigns := senshiSubtitleTrack{Src: "https://cdn.example/signs.en.vtt", Label: "English - Signs & Songs"}
	enDefault := senshiSubtitleTrack{Src: "https://cdn.example/default.en.vtt", Label: "English", Default: true}
	fr := senshiSubtitleTrack{Src: "https://cdn.example/fr.vtt", Label: "Francais"}

	tests := []struct {
		name   string
		tracks []senshiSubtitleTrack
		want   []string
	}{
		{
			name:   "non-forced default first",
			tracks: []senshiSubtitleTrack{enForced, enFull, enDefault},
			want:   []string{enDefault.Src, enFull.Src, enForced.Src},
		},
		{
			name:   "non-forced default before forced english",
			tracks: []senshiSubtitleTrack{enSigns, enDefault, fr},
			want:   []string{enDefault.Src, enSigns.Src, fr.Src},
		},
		{
			name:   "forced english still listed last",
			tracks: []senshiSubtitleTrack{fr, enForced},
			want:   []string{enForced.Src, fr.Src},
		},
		{
			name:   "dedupes identical sources",
			tracks: []senshiSubtitleTrack{enFull, {Src: "https://cdn.example/full.en.vtt", Label: "English"}},
			want:   []string{enFull.Src},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := senshiSubtitleTrackSources(tt.tracks)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestSenshiSubtitleManifestURLs(t *testing.T) {
	subInfo := "https://cdn.example/abc/manifest.json"
	serverFM := "https://embed.example/e/123/?sub.info=" + subInfo
	base := "https://cdn.example/abc"

	tests := []struct {
		name string
		item embedItem
		want []string
	}{
		{
			name: "sub.info primary, base fallbacks appended",
			item: embedItem{ServerFM: &serverFM, MaskedBaseURL: base},
			want: []string{
				subInfo,
				base + "/sub_filemoon.json",
				base + "/sub_artplayer.json",
			},
		},
		{
			name: "sub.info missing falls back to base manifests",
			item: embedItem{MaskedBaseURL: base},
			want: []string{
				base + "/sub_filemoon.json",
				base + "/sub_artplayer.json",
			},
		},
		{
			name: "sub.info equal to base filemoon manifest is deduped",
			item: embedItem{
				ServerFM:      func() *string { u := "https://embed.example/e/123/?sub.info=" + base + "/sub_filemoon.json"; return &u }(),
				MaskedBaseURL: "https://cdn.example/abc",
			},
			want: []string{
				base + "/sub_filemoon.json",
				base + "/sub_artplayer.json",
			},
		},
		{
			name: "no base and no sub.info yields nothing",
			item: embedItem{},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := senshiSubtitleManifestURLs(tt.item)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestSubtitleExt(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"https://cdn.example/subs/en.ass", ".ass"},
		{"https://cdn.example/subs/en.vtt", ".vtt"},
		{"https://cdn.example/subs/en.vtt?token=abc", ".vtt"},
		{"https://cdn.example/subs/en", ""},
		{"", ""},
		{":not-a-url", ""},
	}
	for _, tt := range tests {
		if got := subtitleExt(tt.raw); got != tt.want {
			t.Errorf("subtitleExt(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestSanitizeSenshiWebVTT(t *testing.T) {
	input := "WEBVTT\r\n\r\n00:00:01.000 --> 00:00:03.000\r\nHello \\h world\r\n\r\n00:00:04.000 --> 00:00:06.000\r\nSecond\r\n"
	got, changed := sanitizeSenshiWebVTT([]byte(input))
	if !changed {
		t.Fatal("expected sanitization to report changes")
	}
	text := string(got)
	if !strings.Contains(text, "Hello \u00a0 world") {
		t.Errorf("expected \\h replaced with nbsp, got %q", text)
	}
	if strings.Contains(text, "\\h") {
		t.Errorf("raw \\h still present: %q", text)
	}
	if !strings.HasPrefix(text, "WEBVTT") {
		t.Errorf("expected WEBVTT header, got %q", text)
	}
}

func withSenshiTestHTTPClient(t *testing.T, client *http.Client) {
	t.Helper()
	previous := curdhost.HTTPClient
	t.Cleanup(func() {
		curdhost.HTTPClient = previous
	})
	curdhost.HTTPClient = func() *http.Client { return client }
}

func TestValidatedSenshiASSURL(t *testing.T) {
	assBody := "[Script Info]\nTitle: Test\n\n[V4+ Styles]\nFormat: Name\nStyle: Default,Test\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/subs/en.ass", "/subs/en.vtt.ass":
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, assBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	withSenshiTestHTTPClient(t, server.Client())

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "direct ass url validated in place",
			raw:  server.URL + "/subs/en.ass",
			want: server.URL + "/subs/en.ass",
		},
		{
			name: "vtt url swaps extension to ass",
			raw:  server.URL + "/subs/en.vtt",
			want: server.URL + "/subs/en.ass",
		},
		{
			name: "missing ass twin is rejected",
			raw:  server.URL + "/subs/missing.vtt",
			want: "",
		},
		{
			name: "non-http urls are rejected",
			raw:  "/subs/en.ass",
			want: "",
		},
		{
			name: "unsupported extensions are rejected",
			raw:  server.URL + "/subs/en.srt",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validatedSenshiASSURL(tt.raw); got != tt.want {
				t.Errorf("validatedSenshiASSURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
