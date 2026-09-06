package internal

import (
	"os"
	"runtime"
	"strings"
)

type Platform string

const (
	PlatformLinux   Platform = "linux"
	PlatformDarwin  Platform = "darwin"
	PlatformWindows Platform = "windows"
	PlatformAndroid Platform = "android"
)

// DetectPlatform determines the current execution platform.
// It detects Android either via GOOS=="android" or through Termux environment markers.
func DetectPlatform() Platform {
	if runtime.GOOS == "android" {
		return PlatformAndroid
	}
	if os.Getenv("TERMUX_VERSION") != "" || strings.Contains(os.Getenv("PREFIX"), "com.termux") {
		return PlatformAndroid
	}
	switch runtime.GOOS {
	case "darwin":
		return PlatformDarwin
	case "windows":
		return PlatformWindows
	default:
		return PlatformLinux
	}
}

// IsAndroid returns true if running on an Android/Termux device.
func IsAndroid() bool {
	return DetectPlatform() == PlatformAndroid
}
