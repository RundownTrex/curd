package downloader

import (
	"fmt"
	"strings"
	"time"
)

// TerminalProgressBar renders real-time progress to stdout.
type TerminalProgressBar struct {
	Title       string
	TotalChunks int
	lastUpdate  time.Time
}

// NewTerminalProgressBar creates a new progress bar renderer.
func NewTerminalProgressBar(title string, totalChunks int) *TerminalProgressBar {
	fmt.Printf("\033[1;36mDownloading:\033[0m %s\n", title)
	return &TerminalProgressBar{
		Title:       title,
		TotalChunks: totalChunks,
		lastUpdate:  time.Now(),
	}
}

// Update renders the current progress metrics to the terminal.
func (p *TerminalProgressBar) Update(stats ProgressStats) {
	barWidth := 24
	filledWidth := int((stats.Percent / 100.0) * float64(barWidth))
	if filledWidth > barWidth {
		filledWidth = barWidth
	}
	if filledWidth < 0 {
		filledWidth = 0
	}

	bar := strings.Repeat("█", filledWidth) + strings.Repeat("░", barWidth-filledWidth)

	speedStr := FormatBytes(int64(stats.SpeedBytesPerSec)) + "/s"
	downloadedStr := FormatBytes(stats.DownloadedBytes)
	etaStr := FormatDuration(stats.ETA)

	// Format: \r\033[K[██████░░░░░░] 50.0% (100/200) | 15.2 MB/s | ETA 00:10 | 120.5 MB
	fmt.Printf("\r\033[K[%s] %5.1f%% (%d/%d) | %s | ETA %s | %s",
		bar,
		stats.Percent,
		stats.CurrentChunk,
		stats.TotalChunks,
		speedStr,
		etaStr,
		downloadedStr,
	)
}

// Finish finishes the progress bar line and prints completion message.
func (p *TerminalProgressBar) Finish(finalPath string, totalBytes int64) {
	fmt.Printf("\r\033[K\033[1;32m✓ Downloaded:\033[0m %s (%s)\n", finalPath, FormatBytes(totalBytes))
}

// FormatBytes formats byte counts into human readable strings.
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// FormatDuration formats duration as MM:SS.
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "--:--"
	}
	totalSecs := int(d.Seconds())
	mins := totalSecs / 60
	secs := totalSecs % 60
	if mins >= 60 {
		hours := mins / 60
		mins = mins % 60
		return fmt.Sprintf("%02d:%02d:%02d", hours, mins, secs)
	}
	return fmt.Sprintf("%02d:%02d", mins, secs)
}
