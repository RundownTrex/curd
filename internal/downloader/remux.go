package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var invalidFileNameRE = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)

// SanitizeFileName cleans a string to make it safe for filenames.
func SanitizeFileName(name string) string {
	cleaned := invalidFileNameRE.ReplaceAllString(name, "")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		cleaned = "download"
	}
	return cleaned
}

// DownloadSubtitle downloads a subtitle file and saves it with matching base name.
func DownloadSubtitle(ctx context.Context, client *http.Client, subtitleURL, targetBasePath string, headers map[string]string) (string, error) {
	subtitleURL = strings.TrimSpace(subtitleURL)
	if subtitleURL == "" {
		return "", nil
	}

	ext := ".vtt"
	lowerURL := strings.ToLower(subtitleURL)
	if strings.Contains(lowerURL, ".ass") {
		ext = ".ass"
	} else if strings.Contains(lowerURL, ".srt") {
		ext = ".srt"
	}

	subFilePath := targetBasePath + ext

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subtitleURL, nil)
	if err != nil {
		return "", err
	}

	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("subtitle HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(subFilePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", err
	}

	return subFilePath, nil
}

// FinalizeVideo converts or renames the downloaded temp .part file to the final destination.
func FinalizeVideo(tempPartPath, finalVideoPath string) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err == nil && ffmpegPath != "" {
		// Use ffmpeg stream copy to cleanly package into MP4 container
		tempRemuxPath := finalVideoPath + ".remux.tmp"
		cmd := exec.Command(ffmpegPath, "-y", "-hide_banner", "-loglevel", "error", "-i", tempPartPath, "-c", "copy", tempRemuxPath)
		if remuxErr := cmd.Run(); remuxErr == nil {
			// Successful remux: replace target and remove temp
			_ = os.Remove(tempPartPath)
			if err := os.Rename(tempRemuxPath, finalVideoPath); err == nil {
				return nil
			}
		}
		_ = os.Remove(tempRemuxPath)
	}

	// Fallback or if ffmpeg not installed: directly move part file to final destination
	return os.Rename(tempPartPath, finalVideoPath)
}

// FinalizeAudioVideo combines separate video and audio part files into the final MP4/MKV.
func FinalizeAudioVideo(tempVideoPart, tempAudioPart, finalVideoPath string) error {
	if tempAudioPart == "" {
		return FinalizeVideo(tempVideoPart, finalVideoPath)
	}

	if _, err := os.Stat(tempAudioPart); err != nil {
		return FinalizeVideo(tempVideoPart, finalVideoPath)
	}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err == nil && ffmpegPath != "" {
		tempRemuxPath := finalVideoPath + ".remux.tmp"
		cmd := exec.Command(ffmpegPath, "-y", "-hide_banner", "-loglevel", "error",
			"-i", tempVideoPart, "-i", tempAudioPart, "-c", "copy", tempRemuxPath)
		if remuxErr := cmd.Run(); remuxErr == nil {
			_ = os.Remove(tempVideoPart)
			_ = os.Remove(tempAudioPart)
			if err := os.Rename(tempRemuxPath, finalVideoPath); err == nil {
				return nil
			}
		}
		_ = os.Remove(tempRemuxPath)
	}

	// Fallback if ffmpeg is not available: write video part directly
	_ = os.Remove(tempAudioPart)
	return FinalizeVideo(tempVideoPart, finalVideoPath)
}

// BuildTargetPaths resolves final video and temporary file paths.
func BuildTargetPaths(destDir, fileName string) (finalVideoPath, tempPartPath, subBasePath string, err error) {
	if destDir == "" {
		destDir = "."
	}

	cleanFileName := SanitizeFileName(fileName)
	if !strings.HasSuffix(strings.ToLower(cleanFileName), ".mp4") && !strings.HasSuffix(strings.ToLower(cleanFileName), ".mkv") {
		cleanFileName += ".mp4"
	}

	finalVideoPath = filepath.Join(destDir, cleanFileName)
	tempPartPath = filepath.Join(destDir, "."+cleanFileName+".part")

	ext := filepath.Ext(cleanFileName)
	subBaseName := strings.TrimSuffix(cleanFileName, ext)
	subBasePath = filepath.Join(destDir, subBaseName)

	return finalVideoPath, tempPartPath, subBasePath, nil
}

// RefererOriginFromLink extracts Origin and Referer defaults if not set.
func RefererOriginFromLink(link, fallbackReferrer string) (referer, origin string) {
	referer = strings.TrimSpace(fallbackReferrer)
	if referer == "" {
		referer = link
	}

	parsed, err := url.Parse(referer)
	if err == nil && parsed.Scheme != "" && parsed.Host != "" {
		origin = fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host)
	}

	return referer, origin
}
