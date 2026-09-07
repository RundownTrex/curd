package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"

// DownloadEpisode executes a rate-limit safe download for a single episode.
func DownloadEpisode(ctx context.Context, opts DownloadOptions) error {
	opts.URL = strings.TrimSpace(opts.URL)
	if opts.URL == "" {
		return fmt.Errorf("empty stream URL")
	}

	if opts.DestinationDir == "" {
		opts.DestinationDir = "."
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 3
	}
	if opts.UserAgent == "" {
		opts.UserAgent = defaultUserAgent
	}

	finalVideoPath, tempPartPath, subBasePath, err := BuildTargetPaths(opts.DestinationDir, opts.FileName)
	if err != nil {
		return fmt.Errorf("build target paths: %w", err)
	}

	// Determine headers
	ref, origin := RefererOriginFromLink(opts.URL, opts.Referrer)
	if opts.Origin != "" {
		origin = opts.Origin
	}
	headers := map[string]string{
		"User-Agent": opts.UserAgent,
		"Referer":    ref,
		"Origin":     origin,
	}

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	isHLS := strings.Contains(opts.URL, ".m3u8")

	if isHLS {
		playlist, err := FetchHLSPlaylist(ctx, httpClient, opts.URL, headers, opts.Quality)
		if err != nil {
			return fmt.Errorf("fetch HLS playlist: %w", err)
		}

		hasSeparateAudio := len(playlist.AudioSegments) > 0
		totalChunks := len(playlist.Segments)
		if hasSeparateAudio {
			totalChunks += len(playlist.AudioSegments)
		}

		bar := NewTerminalProgressBar(opts.FileName, totalChunks)
		var sharedBytes int64

		if hasSeparateAudio {
			tempVideoPart := tempPartPath + ".video"
			tempAudioPart := tempPartPath + ".audio"

			videoFile, err := os.Create(tempVideoPart)
			if err != nil {
				return fmt.Errorf("create temp video file %q: %w", tempVideoPart, err)
			}

			// 1. Download video segments
			dlErr := DownloadSegmentsWithOffset(ctx, playlist.Segments, playlist.IsAES128, playlist.KeyBytes, videoFile, headers, opts.Concurrency, 0, totalChunks, &sharedBytes, bar.Update)
			_ = videoFile.Close()

			if dlErr != nil {
				_ = os.Remove(tempVideoPart)
				return fmt.Errorf("download video segments: %w", dlErr)
			}

			// 2. Download audio segments
			audioFile, err := os.Create(tempAudioPart)
			if err != nil {
				_ = os.Remove(tempVideoPart)
				return fmt.Errorf("create temp audio file %q: %w", tempAudioPart, err)
			}

			audioErr := DownloadSegmentsWithOffset(ctx, playlist.AudioSegments, playlist.AudioIsAES128, playlist.AudioKeyBytes, audioFile, headers, opts.Concurrency, len(playlist.Segments), totalChunks, &sharedBytes, bar.Update)
			_ = audioFile.Close()

			if audioErr != nil {
				_ = os.Remove(tempVideoPart)
				_ = os.Remove(tempAudioPart)
				return fmt.Errorf("download audio segments: %w", audioErr)
			}

			// 3. Losslessly mux video and audio
			if err := FinalizeAudioVideo(tempVideoPart, tempAudioPart, finalVideoPath); err != nil {
				return fmt.Errorf("finalize audio/video mux %q: %w", finalVideoPath, err)
			}
		} else {
			// Multiplexed single stream (e.g. AniNeko)
			partFile, err := os.Create(tempPartPath)
			if err != nil {
				return fmt.Errorf("create temp file %q: %w", tempPartPath, err)
			}

			dlErr := DownloadSegmentsWithOffset(ctx, playlist.Segments, playlist.IsAES128, playlist.KeyBytes, partFile, headers, opts.Concurrency, 0, totalChunks, &sharedBytes, bar.Update)
			_ = partFile.Close()

			if dlErr != nil {
				_ = os.Remove(tempPartPath)
				return fmt.Errorf("download segments: %w", dlErr)
			}

			if err := FinalizeVideo(tempPartPath, finalVideoPath); err != nil {
				return fmt.Errorf("finalize video file %q: %w", finalVideoPath, err)
			}
		}

		fi, _ := os.Stat(finalVideoPath)
		var totalBytes int64
		if fi != nil {
			totalBytes = fi.Size()
		}
		bar.Finish(finalVideoPath, totalBytes)
	} else {
		// Direct video download (MP4, MKV, etc.)
		if err := downloadDirect(ctx, httpClient, opts.URL, tempPartPath, headers, opts.FileName); err != nil {
			_ = os.Remove(tempPartPath)
			return fmt.Errorf("direct download: %w", err)
		}

		if err := FinalizeVideo(tempPartPath, finalVideoPath); err != nil {
			return fmt.Errorf("finalize direct video: %w", err)
		}
	}

	// Download external subtitles if provided
	if opts.SubtitleURL != "" {
		subPath, subErr := DownloadSubtitle(ctx, httpClient, opts.SubtitleURL, subBasePath, headers)
		if subErr != nil {
			fmt.Printf("Warning: failed to download subtitles: %v\n", subErr)
		} else if subPath != "" {
			fmt.Printf("\033[1;32m✓ Subtitles:\033[0m %s\n", subPath)
		}
	}

	return nil
}

func downloadDirect(ctx context.Context, client *http.Client, targetURL, tempPath string, headers map[string]string, title string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}

	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	out, err := os.Create(tempPath)
	if err != nil {
		return err
	}
	defer out.Close()

	totalBytes := resp.ContentLength
	bar := NewTerminalProgressBar(title, 1)

	var downloaded int64
	buf := make([]byte, 64*1024)
	startTime := time.Now()
	var lastReport time.Time

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			downloaded += int64(n)

			now := time.Now()
			if now.Sub(lastReport) >= 200*time.Millisecond || readErr == io.EOF {
				lastReport = now
				elapsed := now.Sub(startTime).Seconds()
				var speed float64
				if elapsed > 0 {
					speed = float64(downloaded) / elapsed
				}
				var percent float64
				var eta time.Duration
				if totalBytes > 0 {
					percent = (float64(downloaded) / float64(totalBytes)) * 100.0
					if speed > 0 {
						eta = time.Duration(float64(totalBytes-downloaded)/speed) * time.Second
					}
				}

				bar.Update(ProgressStats{
					CurrentChunk:     1,
					TotalChunks:      1,
					DownloadedBytes:  downloaded,
					SpeedBytesPerSec: speed,
					ETA:              eta,
					Percent:          percent,
				})
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return readErr
		}
	}

	bar.Finish(tempPath, downloaded)
	return nil
}
