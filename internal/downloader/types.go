package downloader

import "time"

// DownloadOptions holds the parameters required to download an episode.
type DownloadOptions struct {
	URL            string
	Referrer       string
	Origin         string
	UserAgent      string
	SubtitleURL    string
	DestinationDir string // e.g. "."
	FileName       string // e.g. "Frieren - Episode 01.mp4"
	Concurrency    int    // Number of concurrent segment downloads (default: 3)
	Quality        string // Preferred quality: "best", "1080p", "720p", "480p"
}

// ProgressStats carries real-time download metrics.
type ProgressStats struct {
	CurrentChunk     int
	TotalChunks      int
	DownloadedBytes  int64
	SpeedBytesPerSec float64
	ETA              time.Duration
	Percent          float64
}

// ProgressFunc is a callback invoked with progress updates.
type ProgressFunc func(stats ProgressStats)
