package internal

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DownloadEpisode downloads an episode to the configured download path using ffmpeg for m3u8 URLs
func DownloadEpisode(anime *Anime, config *CurdConfig) error {
	// Get the anime name (sanitized for filename)
	animeName := GetAnimeName(*anime)
	animeName = sanitizeFilename(animeName)

	// Get episode number
	epNumber := anime.Ep.Number

	// Get video URL
	if len(anime.Ep.Links) == 0 {
		return fmt.Errorf("no download links available for this episode")
	}

	videoURL := anime.Ep.Links[0] // Use the first available link

	// Construct filename: [Anime_Name_EP_N.mp4]
	filename := fmt.Sprintf("%s_EP_%d.mp4", animeName, epNumber)

	// Get download path from config
	downloadPath := os.ExpandEnv(config.DownloadPath)

	// Create full file path
	fullPath := filepath.Join(downloadPath, filename)

	// Delete existing file if it exists
	if _, err := os.Stat(fullPath); err == nil {
		CurdOut(fmt.Sprintf("File already exists, removing: %s", filename))
		if err := os.Remove(fullPath); err != nil {
			return fmt.Errorf("failed to remove existing file: %w", err)
		}
	}

	CurdOut(fmt.Sprintf("Downloading: %s", filename))
	CurdOut(fmt.Sprintf("Episode: %d", epNumber))
	CurdOut(fmt.Sprintf("Destination: %s", downloadPath))

	// Check if URL is m3u8 (HLS streaming)
	if strings.Contains(videoURL, ".m3u8") || strings.Contains(videoURL, "m3u8") {
		// Use ffmpeg to download m3u8 streams
		err := downloadWithFFmpeg(fullPath, videoURL)
		if err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
	} else {
		// Use regular HTTP download for direct video files
		err := downloadFile(fullPath, videoURL)
		if err != nil {
			return fmt.Errorf("download failed: %w", err)
		}
	}

	CurdOut("")
	CurdOut(fmt.Sprintf("✓ Download complete: %s", filename))
	return nil
}

// downloadWithFFmpeg downloads m3u8 streams using ffmpeg
func downloadWithFFmpeg(outputPath string, url string) error {
	// Check if ffmpeg is available
	_, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found. Please install ffmpeg to download streaming videos")
	}

	// Get video duration first to estimate file size
	CurdOut("")
	CurdOut("Analyzing video...")

	duration, _, err := getVideoInfo(url)
	if err == nil && duration > 0 {
		CurdOut(fmt.Sprintf("Duration: %d minutes %d seconds", duration/60, duration%60))
	}

	CurdOut("")
	CurdOut("Starting download...")
	fmt.Println()

	// Run ffmpeg with minimal output
	cmd := exec.Command("ffmpeg",
		"-i", url,
		"-c", "copy", // Copy streams without re-encoding (faster)
		"-bsf:a", "aac_adtstoasc", // Fix audio for MP4 container
		"-progress", "pipe:1", // Send progress to stdout
		"-loglevel", "error", // Only show errors
		"-nostats", // Don't show default stats
		"-y",       // Overwrite output file
		outputPath,
	)

	// Capture stderr for errors
	cmd.Stderr = os.Stderr

	// Create pipe for progress output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Read and parse progress
	parseFFmpegProgress(stdout, duration)

	// Wait for completion
	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("ffmpeg error: %w", err)
	}

	fmt.Println() // New line after progress
	return nil
}

// getVideoDuration gets the duration of the video using ffprobe
func getVideoDuration(url string) (int, error) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		url,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, err
	}

	return int(duration), nil
}

// getVideoInfo gets duration and file size information
func getVideoInfo(url string) (duration int, fileSize int64, err error) {
	// Try to get duration using ffprobe
	duration, _ = getVideoDuration(url)

	// Try to get file size from HTTP headers (for direct files)
	fileSize, _ = getFileSize(url)

	return duration, fileSize, nil
}

// getFileSize tries to get the content length from HTTP headers
func getFileSize(url string) (int64, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Head(url)
	if err != nil {
		// Try GET request with range if HEAD fails
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Range", "bytes=0-0")

		resp, err = client.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
	} else {
		defer resp.Body.Close()
	}

	// Check if we got content length
	if resp.ContentLength > 0 {
		return resp.ContentLength, nil
	}

	// For m3u8, try to parse the playlist and estimate size
	if strings.Contains(url, ".m3u8") {
		return estimateM3U8Size(url)
	}

	return 0, fmt.Errorf("could not determine file size")
}

// estimateM3U8Size attempts to estimate the total size from m3u8 playlist
func estimateM3U8Size(playlistURL string) (int64, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(playlistURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	content := string(body)

	// Look for master playlist with quality variants
	if strings.Contains(content, "#EXT-X-STREAM-INF") {
		// Parse master playlist to find the best quality variant
		lines := strings.Split(content, "\n")
		var bestBandwidth int
		var bestURL string

		for i, line := range lines {
			if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
				// Extract bandwidth
				if strings.Contains(line, "BANDWIDTH=") {
					parts := strings.Split(line, "BANDWIDTH=")
					if len(parts) > 1 {
						bandwidthStr := strings.Split(parts[1], ",")[0]
						bandwidth, _ := strconv.Atoi(bandwidthStr)
						if bandwidth > bestBandwidth {
							bestBandwidth = bandwidth
							// Next line should be the playlist URL
							if i+1 < len(lines) {
								bestURL = strings.TrimSpace(lines[i+1])
							}
						}
					}
				}
			}
		}

		// If we found a variant playlist, fetch it
		if bestURL != "" {
			// Make URL absolute if it's relative
			if !strings.HasPrefix(bestURL, "http") {
				baseURL := playlistURL[:strings.LastIndex(playlistURL, "/")+1]
				bestURL = baseURL + bestURL
			}
			return estimateM3U8Size(bestURL)
		}
	}

	// Count segments and estimate size based on bandwidth
	segmentCount := 0
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			segmentCount++
		}
	}

	// If we have segment count and found bandwidth info, estimate size
	if segmentCount > 0 {
		// Try to extract target duration
		var targetDuration float64 = 10.0 // Default assumption
		for _, line := range lines {
			if strings.HasPrefix(line, "#EXT-X-TARGETDURATION:") {
				durationStr := strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:")
				if dur, err := strconv.ParseFloat(durationStr, 64); err == nil {
					targetDuration = dur
				}
				break
			}
		}

		// Estimate: assume ~4 Mbps bitrate for 720p/1080p
		estimatedBitrate := 4000000.0 // 4 Mbps in bits per second
		totalSeconds := float64(segmentCount) * targetDuration
		estimatedSize := int64((estimatedBitrate / 8) * totalSeconds) // Convert to bytes

		return estimatedSize, nil
	}

	return 0, fmt.Errorf("could not estimate m3u8 size")
}

// parseFFmpegProgress reads and displays ffmpeg progress in a clean format
func parseFFmpegProgress(reader io.Reader, totalDuration int) {
	scanner := bufio.NewScanner(reader)
	var currentTime, downloadSpeed, sizeMB string
	var lastUpdate time.Time
	var currentSeconds int64
	var lastSize int64
	var lastTime time.Time
	var bytesPerSecond float64

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "out_time_ms=") {
			microseconds := strings.TrimPrefix(line, "out_time_ms=")
			if ms, err := strconv.ParseInt(microseconds, 10, 64); err == nil {
				currentSeconds = ms / 1000000
				minutes := currentSeconds / 60
				secs := currentSeconds % 60
				currentTime = fmt.Sprintf("%02d:%02d", minutes, secs)
			}
		} else if strings.HasPrefix(line, "total_size=") {
			sizeStr := strings.TrimPrefix(line, "total_size=")
			if size, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
				sizeMBFloat := float64(size) / 1024 / 1024
				sizeMB = fmt.Sprintf("%.1f MB", sizeMBFloat)

				// Calculate download speed
				now := time.Now()
				if !lastTime.IsZero() {
					timeDiff := now.Sub(lastTime).Seconds()
					if timeDiff > 0 {
						sizeDiff := size - lastSize
						bytesPerSecond = float64(sizeDiff) / timeDiff

						// Format speed in KB/s or MB/s
						if bytesPerSecond >= 1024*1024 {
							downloadSpeed = fmt.Sprintf("%.2f MB/s", bytesPerSecond/1024/1024)
						} else {
							downloadSpeed = fmt.Sprintf("%.1f KB/s", bytesPerSecond/1024)
						}
					}
				}
				lastSize = size
				lastTime = now
			}
		} else if strings.HasPrefix(line, "progress=") {
			progress := strings.TrimPrefix(line, "progress=")

			// Only update every 2 seconds to avoid spam
			if time.Since(lastUpdate) >= 2*time.Second {
				if progress == "end" {
					fmt.Printf("\r✓ Download complete! Size: %s                              \n", sizeMB)
				} else if currentTime != "" && sizeMB != "" && downloadSpeed != "" {
					// Calculate percentage and ETA if we know total duration
					progressStr := ""
					if totalDuration > 0 && currentSeconds > 0 {
						percentage := float64(currentSeconds) / float64(totalDuration) * 100
						progressStr = fmt.Sprintf("%.0f%%", percentage)

						// Calculate ETA based on actual download speed
						if bytesPerSecond > 0 && lastSize > 0 {
							// Estimate total file size based on current progress
							estimatedTotal := float64(lastSize) / (float64(currentSeconds) / float64(totalDuration))
							remainingBytes := estimatedTotal - float64(lastSize)

							// Calculate ETA using the bytesPerSecond we already calculated
							etaSeconds := int(remainingBytes / bytesPerSecond)
							etaMinutes := etaSeconds / 60
							etaSecs := etaSeconds % 60

							fmt.Printf("\r⏳ Progress: %s | Time: %s | Size: %s | Speed: %s | ETA: %dm %ds     ",
								progressStr, currentTime, sizeMB, downloadSpeed, etaMinutes, etaSecs)
						} else {
							fmt.Printf("\r⏳ Progress: %s | Time: %s | Size: %s | Speed: %s     ",
								progressStr, currentTime, sizeMB, downloadSpeed)
						}
					} else {
						fmt.Printf("\r⏳ Time: %s | Size: %s | Speed: %s     ",
							currentTime, sizeMB, downloadSpeed)
					}
					lastUpdate = time.Now()
				}
			}
		}
	}
}

// downloadFile downloads a file from URL to destination
func downloadFile(filepath string, url string) error {
	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check server response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Get total size for progress tracking
	totalSize := resp.ContentLength

	// Create a progress reader
	progressReader := &ProgressReader{
		Reader: resp.Body,
		Total:  totalSize,
		OnProgress: func(current, total int64) {
			if total > 0 {
				percentage := float64(current) / float64(total) * 100
				fmt.Printf("\rProgress: %.2f%% (%d/%d bytes)", percentage, current, total)
			}
		},
	}

	// Write the body to file
	_, err = io.Copy(out, progressReader)
	fmt.Println() // New line after progress
	return err
}

// ProgressReader tracks download progress
type ProgressReader struct {
	Reader     io.Reader
	Total      int64
	Current    int64
	OnProgress func(current, total int64)
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	pr.Current += int64(n)

	if pr.OnProgress != nil {
		pr.OnProgress(pr.Current, pr.Total)
	}

	return n, err
}

// sanitizeFilename removes invalid characters from filename
func sanitizeFilename(name string) string {
	// Replace invalid characters with underscores
	invalid := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	result := name

	for _, char := range invalid {
		result = strings.ReplaceAll(result, char, "_")
	}

	// Remove leading/trailing spaces
	result = strings.TrimSpace(result)

	// Limit length to 200 characters
	if len(result) > 200 {
		result = result[:200]
	}

	return result
}

// DownloadAnimeMenu handles the complete download flow from main menu
func DownloadAnimeMenu(config *CurdConfig, user *User, databaseAnimes *[]Anime) {
	// Step 1: Select anime from user's list
	animeListOptions := make([]SelectionOption, 0)
	animeListMapPreview := make(map[string]RofiSelectPreview)

	// Add back button
	animeListOptions = append(animeListOptions, SelectionOption{
		Label: "<- Back",
		Key:   "back",
	})

	// Helper function to get anime name from title
	getAnimeNameFromEntry := func(title AnimeTitle) string {
		if title.English != "" && config.AnimeNameLanguage == "english" {
			return title.English
		}
		return title.Romaji
	}

	// Get all anime from watching list
	for _, entry := range user.AnimeList.Watching {
		option := SelectionOption{
			Key:   fmt.Sprintf("%d", entry.Media.ID),
			Label: fmt.Sprintf("%s (%d episodes)", getAnimeNameFromEntry(entry.Media.Title), entry.Media.Episodes),
		}
		animeListOptions = append(animeListOptions, option)
		animeListMapPreview[option.Key] = RofiSelectPreview{
			Title:      option.Label,
			CoverImage: entry.CoverImage,
		}
	}

	// Add all other categories
	for _, entry := range user.AnimeList.Completed {
		option := SelectionOption{
			Key:   fmt.Sprintf("%d", entry.Media.ID),
			Label: fmt.Sprintf("%s (%d episodes)", getAnimeNameFromEntry(entry.Media.Title), entry.Media.Episodes),
		}
		animeListOptions = append(animeListOptions, option)
		animeListMapPreview[option.Key] = RofiSelectPreview{
			Title:      option.Label,
			CoverImage: entry.CoverImage,
		}
	}

	for _, entry := range user.AnimeList.Paused {
		option := SelectionOption{
			Key:   fmt.Sprintf("%d", entry.Media.ID),
			Label: fmt.Sprintf("%s (%d episodes)", getAnimeNameFromEntry(entry.Media.Title), entry.Media.Episodes),
		}
		animeListOptions = append(animeListOptions, option)
		animeListMapPreview[option.Key] = RofiSelectPreview{
			Title:      option.Label,
			CoverImage: entry.CoverImage,
		}
	}

	for _, entry := range user.AnimeList.Planning {
		option := SelectionOption{
			Key:   fmt.Sprintf("%d", entry.Media.ID),
			Label: fmt.Sprintf("%s (%d episodes)", getAnimeNameFromEntry(entry.Media.Title), entry.Media.Episodes),
		}
		animeListOptions = append(animeListOptions, option)
		animeListMapPreview[option.Key] = RofiSelectPreview{
			Title:      option.Label,
			CoverImage: entry.CoverImage,
		}
	}

	var selectedAnime SelectionOption
	var err error

	selectedAnime, err = DynamicSelect(animeListOptions)
	if err != nil {
		Log(fmt.Sprintf("Error selecting anime: %v", err))
		return
	}

	// Handle back button
	if selectedAnime.Key == "back" || selectedAnime.Key == "-1" {
		return
	}

	// Find the selected anime entry
	var animeEntry *Entry
	selectedID := selectedAnime.Key

	for i := range user.AnimeList.Watching {
		if fmt.Sprintf("%d", user.AnimeList.Watching[i].Media.ID) == selectedID {
			animeEntry = &user.AnimeList.Watching[i]
			break
		}
	}
	if animeEntry == nil {
		for i := range user.AnimeList.Completed {
			if fmt.Sprintf("%d", user.AnimeList.Completed[i].Media.ID) == selectedID {
				animeEntry = &user.AnimeList.Completed[i]
				break
			}
		}
	}
	if animeEntry == nil {
		for i := range user.AnimeList.Paused {
			if fmt.Sprintf("%d", user.AnimeList.Paused[i].Media.ID) == selectedID {
				animeEntry = &user.AnimeList.Paused[i]
				break
			}
		}
	}
	if animeEntry == nil {
		for i := range user.AnimeList.Planning {
			if fmt.Sprintf("%d", user.AnimeList.Planning[i].Media.ID) == selectedID {
				animeEntry = &user.AnimeList.Planning[i]
				break
			}
		}
	}

	if animeEntry == nil {
		CurdOut("Error: Could not find selected anime")
		return
	}

	// Create anime object
	anime := &Anime{
		Title:         animeEntry.Media.Title,
		TotalEpisodes: animeEntry.Media.Episodes,
		CoverImage:    animeEntry.CoverImage,
		AnilistId:     animeEntry.Media.ID,
	}

	// Get current progress
	currentEp := animeEntry.Progress + 1
	anime.Ep.Number = currentEp

	// Find AllanimeId from database or search
	animePointer := LocalFindAnime(*databaseAnimes, anime.AnilistId, "")
	if animePointer != nil {
		anime.AllanimeId = animePointer.AllanimeId
	} else {
		// Search for anime
		userQuery := anime.Title.Romaji
		animeList, err := SearchAnime(string(userQuery), config.SubOrDub)
		if err != nil {
			CurdOut(fmt.Sprintf("Error searching anime: %v", err))
			return
		}

		if len(animeList) == 0 {
			CurdOut("No results found for this anime")
			return
		}

		// Try to auto-match
		targetLabel := fmt.Sprintf("%v (%d episodes)", userQuery, anime.TotalEpisodes)
		found := false
		for _, option := range animeList {
			if option.Label == targetLabel {
				anime.AllanimeId = option.Key
				found = true
				break
			}
		}

		if !found {
			// Manual selection
			CurdOut("Could not auto-match anime, please select manually:")
			selectedAllanime, err := DynamicSelect(animeList)
			if err != nil || selectedAllanime.Key == "-1" {
				return
			}
			anime.AllanimeId = selectedAllanime.Key
		}
	}

	// Step 2: Show episode list
	downloadEpisodeSelection(anime, config)
}

// downloadEpisodeSelection handles episode selection and download
func downloadEpisodeSelection(anime *Anime, config *CurdConfig) {
	if anime.TotalEpisodes == 0 {
		CurdOut("Total episodes information not available")
		return
	}

	// Create episode list
	episodes := make([]SelectionOption, 0)

	// Add back button
	episodes = append(episodes, SelectionOption{
		Label: "<- Back",
		Key:   "back",
	})

	// Get current episode from anime progress
	currentEp := anime.Ep.Number

	for i := 1; i <= anime.TotalEpisodes; i++ {
		label := fmt.Sprintf("Episode %d", i)
		if i == currentEp {
			label += " (Current)"
		}
		episodes = append(episodes, SelectionOption{
			Label: label,
			Key:   fmt.Sprintf("%d", i),
		})
	}

	// Show multi-selection menu
	selectedEpisodes, err := DynamicMultiSelect(episodes)
	if err != nil {
		CurdOut(fmt.Sprintf("Selection cancelled: %v", err))
		return
	}

	// Check if no episodes were selected or back was selected
	if len(selectedEpisodes) == 0 {
		return
	}

	// Filter out the back button if it was somehow included
	validEpisodes := make([]SelectionOption, 0)
	for _, ep := range selectedEpisodes {
		if ep.Key != "back" && ep.Key != "-1" {
			validEpisodes = append(validEpisodes, ep)
		}
	}

	if len(validEpisodes) == 0 {
		return
	}

	// Download each episode in order
	successCount := 0
	failCount := 0

	for i, selected := range validEpisodes {
		// Parse episode number
		var epNum int
		fmt.Sscanf(selected.Key, "%d", &epNum)

		if epNum <= 0 {
			CurdOut(fmt.Sprintf("Invalid episode selection: %s", selected.Key))
			failCount++
			continue
		}

		// Show progress
		CurdOut(fmt.Sprintf("\n[%d/%d] Downloading Episode %d...", i+1, len(validEpisodes), epNum))

		// Set the selected episode
		anime.Ep.Number = epNum

		// Get episode links
		links, err := GetEpisodeURL(*config, anime.AllanimeId, epNum)
		if err != nil {
			CurdOut(fmt.Sprintf("Error getting episode links for Episode %d: %v", epNum, err))
			failCount++
			continue
		}

		if len(links) == 0 {
			CurdOut(fmt.Sprintf("No download links available for Episode %d", epNum))
			failCount++
			continue
		}

		anime.Ep.Links = links

		// Download the episode
		err = DownloadEpisode(anime, config)
		if err != nil {
			CurdOut(fmt.Sprintf("Error downloading Episode %d: %v", epNum, err))
			failCount++
		} else {
			successCount++
		}
	}

	// Show summary
	CurdOut(fmt.Sprintf("\n✓ Download complete! Success: %d, Failed: %d", successCount, failCount))

	// Ask if user wants to download more episodes
	continueOptions := []SelectionOption{
		{Key: "yes", Label: "Download more episodes"},
		{Key: "no", Label: "Back to main menu"},
	}

	continueChoice, err := DynamicSelect(continueOptions)
	if err != nil || continueChoice.Key == "no" || continueChoice.Key == "-1" {
		return
	}

	// Loop back to episode selection
	downloadEpisodeSelection(anime, config)
}
