package internal

import (
	"os"
	"strings"
	"testing"
)

func TestBuildAndroidIntentCommand(t *testing.T) {
	// Test with default config
	config := &CurdConfig{}
	cmd := BuildAndroidIntentCommand(config, "https://example.com/video.m3u8", "Frieren - Episode 1")

	expectedPrefix := []string{"start", "--user", "0", "-a", "android.intent.action.VIEW", "-d", "https://example.com/video.m3u8", "-n", "is.xyz.mpv/.MPVActivity", "-e", "title", "Frieren - Episode 1"}

	if len(cmd) != len(expectedPrefix) {
		t.Fatalf("expected %d args, got %d: %v", len(expectedPrefix), len(cmd), cmd)
	}

	for i := range cmd {
		if cmd[i] != expectedPrefix[i] {
			t.Errorf("arg %d mismatch: expected %q, got %q", i, expectedPrefix[i], cmd[i])
		}
	}

	// Test with custom player package and activity
	customConfig := &CurdConfig{
		AndroidPlayerPackage:  "org.videolan.vlc",
		AndroidPlayerActivity: "org.videolan.vlc.gui.video.VideoPlayerActivity",
	}
	customCmd := BuildAndroidIntentCommand(customConfig, "https://example.com/stream.mp4", "One Piece - Episode 1000")

	expectedComponent := "org.videolan.vlc/org.videolan.vlc.gui.video.VideoPlayerActivity"
	foundComponent := false
	for i, arg := range customCmd {
		if arg == "-n" && i+1 < len(customCmd) && customCmd[i+1] == expectedComponent {
			foundComponent = true
			break
		}
	}
	if !foundComponent {
		t.Fatalf("expected custom component %q in command: %v", expectedComponent, customCmd)
	}
}

func TestSanitizeConfigForPlatform(t *testing.T) {
	// Force Android detection via environment
	origTermux := os.Getenv("TERMUX_VERSION")
	os.Setenv("TERMUX_VERSION", "0.118.0")
	defer func() {
		if origTermux == "" {
			os.Unsetenv("TERMUX_VERSION")
		} else {
			os.Setenv("TERMUX_VERSION", origTermux)
		}
	}()

	if !IsAndroid() {
		t.Fatal("expected IsAndroid() to be true when TERMUX_VERSION is set")
	}

	cfg := &CurdConfig{
		RofiSelection:   true,
		ImagePreview:    true,
		DiscordPresence: true,
		AlternateScreen: true,
	}

	SanitizeConfigForPlatform(cfg)

	if cfg.RofiSelection {
		t.Error("expected RofiSelection to be false on Android")
	}
	if cfg.ImagePreview {
		t.Error("expected ImagePreview to be false on Android")
	}
	if cfg.DiscordPresence {
		t.Error("expected DiscordPresence to be false on Android")
	}
	if cfg.AlternateScreen {
		t.Error("expected AlternateScreen to be false on Android")
	}
	if cfg.AndroidPlayerPackage != "is.xyz.mpv" {
		t.Errorf("expected default AndroidPlayerPackage 'is.xyz.mpv', got %q", cfg.AndroidPlayerPackage)
	}
	if cfg.AndroidPlayerActivity != ".MPVActivity" {
		t.Errorf("expected default AndroidPlayerActivity '.MPVActivity', got %q", cfg.AndroidPlayerActivity)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	origTermux := os.Getenv("TERMUX_VERSION")
	os.Setenv("TERMUX_VERSION", "0.118.0")
	defer func() {
		if origTermux == "" {
			os.Unsetenv("TERMUX_VERSION")
		} else {
			os.Setenv("TERMUX_VERSION", origTermux)
		}
	}()

	path := DefaultConfigPath()
	if !strings.HasSuffix(path, "android.conf") && !strings.HasSuffix(path, "curd.conf") {
		t.Fatalf("expected config path to end in android.conf or curd.conf, got: %s", path)
	}
}

func TestFindAndroidAmBinaryPrefersTermux(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "termux-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	binDir := tempDir + "/bin"
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}

	fakeAm := binDir + "/am"
	if err := os.WriteFile(fakeAm, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	origPrefix := os.Getenv("PREFIX")
	origPath := os.Getenv("PATH")
	os.Setenv("PREFIX", tempDir)
	os.Setenv("PATH", binDir+":"+origPath)
	defer func() {
		if origPrefix == "" {
			os.Unsetenv("PREFIX")
		} else {
			os.Setenv("PREFIX", origPrefix)
		}
		os.Setenv("PATH", origPath)
	}()

	found := FindAndroidAmBinary()
	if found != fakeAm {
		t.Fatalf("expected FindAndroidAmBinary() to return %q, got %q", fakeAm, found)
	}
}

func TestResolveAndroidPlayer(t *testing.T) {
	// 1. Default config -> MPV
	pkg, act := ResolveAndroidPlayer(nil)
	if pkg != "is.xyz.mpv" || act != ".MPVActivity" {
		t.Errorf("expected default MPV player, got %s/%s", pkg, act)
	}

	// 2. Player = "vlc" -> VLC
	vlcCfg := &CurdConfig{Player: "vlc"}
	pkg, act = ResolveAndroidPlayer(vlcCfg)
	if pkg != "org.videolan.vlc" || act != ".gui.video.VideoPlayerActivity" {
		t.Errorf("expected VLC player for Player=vlc, got %s/%s", pkg, act)
	}

	// 3. AndroidPlayerPackage = "vlc" -> VLC
	vlcPkgCfg := &CurdConfig{AndroidPlayerPackage: "vlc"}
	pkg, act = ResolveAndroidPlayer(vlcPkgCfg)
	if pkg != "org.videolan.vlc" || act != ".gui.video.VideoPlayerActivity" {
		t.Errorf("expected VLC player for AndroidPlayerPackage=vlc, got %s/%s", pkg, act)
	}

	// 4. AndroidPlayerPackage = "org.videolan.vlc" -> VLC
	vlcFullCfg := &CurdConfig{AndroidPlayerPackage: "org.videolan.vlc"}
	pkg, act = ResolveAndroidPlayer(vlcFullCfg)
	if pkg != "org.videolan.vlc" || act != ".gui.video.VideoPlayerActivity" {
		t.Errorf("expected VLC player for AndroidPlayerPackage=org.videolan.vlc, got %s/%s", pkg, act)
	}

	// 5. Custom package and activity
	customCfg := &CurdConfig{
		AndroidPlayerPackage:  "com.mxtech.videoplayer.ad",
		AndroidPlayerActivity: ".ActivityScreen",
	}
	pkg, act = ResolveAndroidPlayer(customCfg)
	if pkg != "com.mxtech.videoplayer.ad" || act != ".ActivityScreen" {
		t.Errorf("expected custom player preserved, got %s/%s", pkg, act)
	}
}

func TestSanitizeConfigForPlatform_VLC(t *testing.T) {
	origTermux := os.Getenv("TERMUX_VERSION")
	os.Setenv("TERMUX_VERSION", "0.118.0")
	defer func() {
		if origTermux == "" {
			os.Unsetenv("TERMUX_VERSION")
		} else {
			os.Setenv("TERMUX_VERSION", origTermux)
		}
	}()

	cfg := &CurdConfig{Player: "vlc"}
	SanitizeConfigForPlatform(cfg)

	if cfg.AndroidPlayerPackage != "org.videolan.vlc" {
		t.Errorf("expected AndroidPlayerPackage 'org.videolan.vlc', got %q", cfg.AndroidPlayerPackage)
	}
	if cfg.AndroidPlayerActivity != ".gui.video.VideoPlayerActivity" {
		t.Errorf("expected AndroidPlayerActivity '.gui.video.VideoPlayerActivity', got %q", cfg.AndroidPlayerActivity)
	}
}

func TestResolveDiscordLargeImage(t *testing.T) {
	fallback := "https://anilist.co/img/icons/icon.png"

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: fallback,
		},
		{
			name:     "svg fallback URL",
			input:    "https://anilist.co/img/icons/icon.svg",
			expected: fallback,
		},
		{
			name:     "local cached file path",
			input:    "/home/user/.cache/curd/images/abcdef123456.jpg",
			expected: fallback,
		},
		{
			name:     "valid AniList JPG cover",
			input:    "https://s4.anilist.co/file/anilistcdn/media/anime/cover/large/bx171110-7zOdInS6DQNL.jpg",
			expected: "https://s4.anilist.co/file/anilistcdn/media/anime/cover/large/bx171110-7zOdInS6DQNL.jpg",
		},
		{
			name:     "valid PNG cover with whitespace",
			input:    "  https://example.com/cover.png  ",
			expected: "https://example.com/cover.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveDiscordLargeImage(tt.input)
			if got != tt.expected {
				t.Errorf("ResolveDiscordLargeImage(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestBuildDiscordButtons(t *testing.T) {
	b0 := BuildDiscordButtons(0, 0)
	if len(b0) != 0 {
		t.Errorf("expected 0 buttons, got %d", len(b0))
	}

	b1 := BuildDiscordButtons(171110, 0)
	if len(b1) != 1 || b1[0].Url != "https://anilist.co/anime/171110" {
		t.Errorf("expected AniList button only, got: %v", b1)
	}

	b2 := BuildDiscordButtons(0, 57466)
	if len(b2) != 1 || b2[0].Url != "https://myanimelist.net/anime/57466" {
		t.Errorf("expected MAL button only, got: %v", b2)
	}

	b3 := BuildDiscordButtons(171110, 57466)
	if len(b3) != 2 {
		t.Fatalf("expected 2 buttons, got %d", len(b3))
	}
	if b3[0].Url != "https://anilist.co/anime/171110" || b3[1].Url != "https://myanimelist.net/anime/57466" {
		t.Errorf("button URLs mismatch: %v, %v", b3[0].Url, b3[1].Url)
	}
}

func TestGetAnimeIDAndImage_InvalidID(t *testing.T) {
	_, _, err := GetAnimeIDAndImage(0)
	if err == nil {
		t.Error("expected error for anilistMediaID = 0, got nil")
	}

	_, _, err = GetAnimeIDAndImage(-1)
	if err == nil {
		t.Error("expected error for anilistMediaID = -1, got nil")
	}
}


