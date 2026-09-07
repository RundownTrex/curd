package downloader

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDownloadSegmentsWithRateLimitRetry(t *testing.T) {
	var chunk1Attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/seg0.ts":
			w.Write([]byte("DATA_SEG_0_"))
		case "/seg1.ts":
			// Simulate HTTP 429 Too Many Requests on first try, then 200 on retry
			attempt := atomic.AddInt32(&chunk1Attempts, 1)
			if attempt == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte("Rate limited"))
				return
			}
			w.Write([]byte("DATA_SEG_1_"))
		case "/seg2.ts":
			w.Write([]byte("DATA_SEG_2"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	playlist := &HLSPlaylist{
		Segments: []HLSSegment{
			{Index: 0, URL: server.URL + "/seg0.ts"},
			{Index: 1, URL: server.URL + "/seg1.ts"},
			{Index: 2, URL: server.URL + "/seg2.ts"},
		},
	}

	var outputBuf bytes.Buffer
	var updates int

	err := DownloadSegments(context.Background(), playlist, &outputBuf, nil, 2, func(s ProgressStats) {
		updates++
	})
	if err != nil {
		t.Fatalf("DownloadSegments failed: %v", err)
	}

	expected := "DATA_SEG_0_DATA_SEG_1_DATA_SEG_2"
	if outputBuf.String() != expected {
		t.Errorf("expected assembled data %q, got %q", expected, outputBuf.String())
	}

	if atomic.LoadInt32(&chunk1Attempts) < 2 {
		t.Errorf("expected chunk 1 to be retried after rate limit, attempts = %d", chunk1Attempts)
	}
}
