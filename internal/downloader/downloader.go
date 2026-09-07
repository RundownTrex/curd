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
	stateFilePath := StateFilePath(tempPartPath)

	if isHLS {
		playlist, err := FetchHLSPlaylist(ctx, httpClient, opts.URL, headers, opts.Quality)
		if err != nil {
			return fmt.Errorf("fetch HLS playlist: %w", err)
		}

		hasSeparateAudio := len(playlist.AudioSegments) > 0
		totalVideoChunks := len(playlist.Segments)
		totalAudioChunks := len(playlist.AudioSegments)
		totalChunks := totalVideoChunks + totalAudioChunks

		bar := NewTerminalProgressBar(opts.FileName, totalChunks)
		var sharedBytes int64

		if hasSeparateAudio {
			tempVideoPart := tempPartPath + ".video"
			tempAudioPart := tempPartPath + ".audio"

			state, _ := LoadDownloadState(stateFilePath)
			resumeVideoFrom := 0
			resumeAudioFrom := 0

			if state != nil && state.TotalChunks == totalChunks && state.IsSeparateAudio {
				resumeVideoFrom = state.CommittedChunks
				resumeAudioFrom = state.AudioCommitted
				sharedBytes = state.DownloadedBytes
				if resumeVideoFrom > totalVideoChunks {
					resumeVideoFrom = totalVideoChunks
				}
				if resumeAudioFrom > totalAudioChunks {
					resumeAudioFrom = totalAudioChunks
				}
				if resumeVideoFrom > 0 || resumeAudioFrom > 0 {
					fmt.Printf("\033[1;36m[Resume]\033[0m Found partial download for %s (resuming from chunk %d/%d)...\n", opts.FileName, resumeVideoFrom+resumeAudioFrom, totalChunks)
				}
			} else if state != nil {
				// Mismatched state (e.g. source changed)
				_ = os.Remove(tempVideoPart)
				_ = os.Remove(tempAudioPart)
				RemoveDownloadState(stateFilePath)
			}

			// 1. Download video segments
			if resumeVideoFrom < totalVideoChunks {
				var videoFile *os.File
				if resumeVideoFrom > 0 {
					videoFile, err = os.OpenFile(tempVideoPart, os.O_APPEND|os.O_WRONLY, 0644)
				} else {
					videoFile, err = os.Create(tempVideoPart)
				}
				if err != nil {
					return fmt.Errorf("open temp video file %q: %w", tempVideoPart, err)
				}

				saveVideoState := func(committed int, totalBytes int64) {
					_ = SaveDownloadState(stateFilePath, &DownloadState{
						PlaylistURL:     opts.URL,
						TotalChunks:     totalChunks,
						CommittedChunks: committed,
						DownloadedBytes: totalBytes,
						Quality:         opts.Quality,
						IsSeparateAudio: true,
						AudioTotal:      totalAudioChunks,
						AudioCommitted:  resumeAudioFrom,
					})
				}

				dlErr := DownloadSegmentsResume(ctx, playlist.Segments, playlist.IsAES128, playlist.KeyBytes, videoFile, headers, opts.Concurrency, resumeVideoFrom, 0, totalChunks, &sharedBytes, sharedBytes, saveVideoState, bar.Update)
				_ = videoFile.Close()

				if dlErr != nil {
					// Preserve files on error so user can resume
					return fmt.Errorf("download video segments: %w", dlErr)
				}
				resumeVideoFrom = totalVideoChunks
			}

			// 2. Download audio segments
			var audioFile *os.File
			if resumeAudioFrom > 0 {
				audioFile, err = os.OpenFile(tempAudioPart, os.O_APPEND|os.O_WRONLY, 0644)
			} else {
				audioFile, err = os.Create(tempAudioPart)
			}
			if err != nil {
				return fmt.Errorf("open temp audio file %q: %w", tempAudioPart, err)
			}

			saveAudioState := func(committed int, totalBytes int64) {
				_ = SaveDownloadState(stateFilePath, &DownloadState{
					PlaylistURL:     opts.URL,
					TotalChunks:     totalChunks,
					CommittedChunks: totalVideoChunks,
					DownloadedBytes: totalBytes,
					Quality:         opts.Quality,
					IsSeparateAudio: true,
					AudioTotal:      totalAudioChunks,
					AudioCommitted:  committed,
				})
			}

			audioErr := DownloadSegmentsResume(ctx, playlist.AudioSegments, playlist.AudioIsAES128, playlist.AudioKeyBytes, audioFile, headers, opts.Concurrency, resumeAudioFrom, totalVideoChunks, totalChunks, &sharedBytes, sharedBytes, saveAudioState, bar.Update)
			_ = audioFile.Close()

			if audioErr != nil {
				// Preserve files on error so user can resume
				return fmt.Errorf("download audio segments: %w", audioErr)
			}

			// 3. Losslessly mux video and audio
			if err := FinalizeAudioVideo(tempVideoPart, tempAudioPart, finalVideoPath); err != nil {
				return fmt.Errorf("finalize audio/video mux %q: %w", finalVideoPath, err)
			}
			RemoveDownloadState(stateFilePath)
		} else {
			// Multiplexed single stream (e.g. AniNeko)
			state, _ := LoadDownloadState(stateFilePath)
			resumeFrom := 0
			var initialBytes int64

			if state != nil && state.TotalChunks == totalChunks && !state.IsSeparateAudio {
				if fi, err := os.Stat(tempPartPath); err == nil && fi.Size() > 0 {
					resumeFrom = state.CommittedChunks
					initialBytes = state.DownloadedBytes
					sharedBytes = initialBytes
					if resumeFrom > totalChunks {
						resumeFrom = totalChunks
					}
					if resumeFrom > 0 {
						fmt.Printf("\033[1;36m[Resume]\033[0m Found partial download for %s (resuming from chunk %d/%d)...\n", opts.FileName, resumeFrom, totalChunks)
					}
				}
			} else if state != nil {
				// Mismatched state (e.g. source changed)
				_ = os.Remove(tempPartPath)
				RemoveDownloadState(stateFilePath)
			}

			var partFile *os.File
			if resumeFrom > 0 {
				partFile, err = os.OpenFile(tempPartPath, os.O_APPEND|os.O_WRONLY, 0644)
			} else {
				partFile, err = os.Create(tempPartPath)
			}
			if err != nil {
				return fmt.Errorf("open temp file %q: %w", tempPartPath, err)
			}

			saveState := func(committed int, totalBytes int64) {
				_ = SaveDownloadState(stateFilePath, &DownloadState{
					PlaylistURL:     opts.URL,
					TotalChunks:     totalChunks,
					CommittedChunks: committed,
					DownloadedBytes: totalBytes,
					Quality:         opts.Quality,
					IsSeparateAudio: false,
				})
			}

			dlErr := DownloadSegmentsResume(ctx, playlist.Segments, playlist.IsAES128, playlist.KeyBytes, partFile, headers, opts.Concurrency, resumeFrom, 0, totalChunks, &sharedBytes, initialBytes, saveState, bar.Update)
			_ = partFile.Close()

			if dlErr != nil {
				// Preserve file on error so user can resume
				return fmt.Errorf("download segments: %w", dlErr)
			}

			if err := FinalizeVideo(tempPartPath, finalVideoPath); err != nil {
				return fmt.Errorf("finalize video file %q: %w", finalVideoPath, err)
			}
			RemoveDownloadState(stateFilePath)
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
	var existingBytes int64
	if fi, err := os.Stat(tempPath); err == nil {
		existingBytes = fi.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}

	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	if existingBytes > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingBytes))
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var out *os.File
	var totalBytes int64

	if existingBytes > 0 && resp.StatusCode == http.StatusPartialContent {
		// 206 Partial Content: resume appending
		out, err = os.OpenFile(tempPath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		totalBytes = existingBytes + resp.ContentLength
		fmt.Printf("\033[1;36m[Resume]\033[0m Continuing direct download from byte %d...\n", existingBytes)
	} else {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("HTTP status %d", resp.StatusCode)
		}
		out, err = os.Create(tempPath)
		if err != nil {
			return err
		}
		existingBytes = 0
		totalBytes = resp.ContentLength
	}
	defer out.Close()

	bar := NewTerminalProgressBar(title, 1)

	downloaded := existingBytes
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
					sessionBytes := downloaded - existingBytes
					if sessionBytes > 0 {
						speed = float64(sessionBytes) / elapsed
					}
				}
				var percent float64
				var eta time.Duration
				if totalBytes > 0 {
					percent = (float64(downloaded) / float64(totalBytes)) * 100.0
					if speed > 0 && downloaded < totalBytes {
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

