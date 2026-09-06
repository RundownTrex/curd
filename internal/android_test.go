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
