package downloader

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// DownloadState tracks progress of an in-flight download for seamless continuation.
type DownloadState struct {
	PlaylistURL     string    `json:"playlist_url"`
	TotalChunks     int       `json:"total_chunks"`
	CommittedChunks int       `json:"committed_chunks"`
	DownloadedBytes int64     `json:"downloaded_bytes"`
	Quality         string    `json:"quality"`
	IsSeparateAudio bool      `json:"is_separate_audio"`
	AudioTotal      int       `json:"audio_total"`
	AudioCommitted  int       `json:"audio_committed"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// StateFilePath returns the companion metadata state file path for a .part file.
func StateFilePath(partFilePath string) string {
	return partFilePath + ".state"
}

// LoadDownloadState reads and unmarshals the download state file.
func LoadDownloadState(statePath string) (*DownloadState, error) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, err
	}
	var state DownloadState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	return &state, nil
}

// SaveDownloadState serializes and atomically writes the state file.
func SaveDownloadState(statePath string, state *DownloadState) error {
	if state == nil {
		return nil
	}
	state.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", statePath, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := os.Rename(tmpPath, statePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("commit state: %w", err)
	}
	return nil
}

// RemoveDownloadState deletes the state file if it exists.
func RemoveDownloadState(statePath string) {
	if statePath != "" {
		_ = os.Remove(statePath)
	}
}
