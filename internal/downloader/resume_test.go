package downloader

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloadStateSaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "test.mp4.part.state")

	orig := &DownloadState{
		PlaylistURL:     "https://example.com/stream.m3u8",
		TotalChunks:     100,
		CommittedChunks: 45,
		DownloadedBytes: 45000000,
		Quality:         "1080p",
		IsSeparateAudio: true,
		AudioTotal:      100,
		AudioCommitted:  20,
		UpdatedAt:       time.Now(),
	}

	if err := SaveDownloadState(statePath, orig); err != nil {
		t.Fatalf("SaveDownloadState failed: %v", err)
	}

	loaded, err := LoadDownloadState(statePath)
	if err != nil {
		t.Fatalf("LoadDownloadState failed: %v", err)
	}

	if loaded.TotalChunks != orig.TotalChunks || loaded.CommittedChunks != orig.CommittedChunks {
		t.Errorf("state mismatch: got %+v, want %+v", loaded, orig)
	}
	if loaded.AudioCommitted != orig.AudioCommitted {
		t.Errorf("audio committed mismatch: got %d, want %d", loaded.AudioCommitted, orig.AudioCommitted)
	}

	RemoveDownloadState(statePath)
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("expected state file to be removed, but still exists")
	}
}

func TestDownloadSegmentsResumeSimulation(t *testing.T) {
	var requestedSegments [6]int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 6; i++ {
			if r.URL.Path == fmt.Sprintf("/seg%d.ts", i) {
				atomic.AddInt32(&requestedSegments[i], 1)
				w.Write([]byte(fmt.Sprintf("CHUNK_%d_", i)))
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	segments := make([]HLSSegment, 6)
	for i := 0; i < 6; i++ {
		segments[i] = HLSSegment{
			Index: i,
			URL:   fmt.Sprintf("%s/seg%d.ts", server.URL, i),
		}
	}

	var outputBuf bytes.Buffer

	// Phase 1: Download segments 0 through 2 (simulate partial download)
	subSegs1 := segments[0:3]
	err := DownloadSegmentsResume(context.Background(), subSegs1, false, nil, &outputBuf, nil, 2, 0, 0, 6, nil, 0, nil, nil)
	if err != nil {
		t.Fatalf("Phase 1 download failed: %v", err)
	}

	expectedPart1 := "CHUNK_0_CHUNK_1_CHUNK_2_"
	if outputBuf.String() != expectedPart1 {
		t.Fatalf("Phase 1 data mismatch: got %q, want %q", outputBuf.String(), expectedPart1)
	}

	// Phase 2: Resume download from segment index 3 to 5
	err = DownloadSegmentsResume(context.Background(), segments, false, nil, &outputBuf, nil, 2, 3, 0, 6, nil, int64(outputBuf.Len()), nil, nil)
	if err != nil {
		t.Fatalf("Phase 2 resume download failed: %v", err)
	}

	expectedFull := "CHUNK_0_CHUNK_1_CHUNK_2_CHUNK_3_CHUNK_4_CHUNK_5_"
	if outputBuf.String() != expectedFull {
		t.Fatalf("Resumed full data mismatch: got %q, want %q", outputBuf.String(), expectedFull)
	}

	// Verify segments 0-2 were NOT re-requested in Phase 2
	for i := 0; i < 6; i++ {
		reqCount := atomic.LoadInt32(&requestedSegments[i])
		if reqCount != 1 {
			t.Errorf("segment %d was requested %d times (expected 1)", i, reqCount)
		}
	}
}

func TestDownloadDirectResume(t *testing.T) {
	fullData := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "bytes=10-" {
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte(fullData[10:]))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fullData))
	}))
	defer server.Close()

	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "direct.mp4.part")

	// Pre-populate first 10 bytes (simulating interrupted download)
	if err := os.WriteFile(tempFile, []byte(fullData[:10]), 0644); err != nil {
		t.Fatalf("failed to write initial partial file: %v", err)
	}

	// Run downloadDirect on the server URL
	client := server.Client()
	err := downloadDirect(context.Background(), client, server.URL, tempFile, nil, "DirectResumeTest")
	if err != nil {
		t.Fatalf("downloadDirect failed: %v", err)
	}

	result, err := os.ReadFile(tempFile)
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}

	if string(result) != fullData {
		t.Errorf("direct resume mismatch: got %q, want %q", string(result), fullData)
	}
}
