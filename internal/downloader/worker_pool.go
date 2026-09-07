package downloader

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type segmentResult struct {
	index int
	data  []byte
	err   error
}

type liveProgressReader struct {
	reader io.Reader
	onRead func(n int64)
}

func (lr *liveProgressReader) Read(p []byte) (int, error) {
	n, err := lr.reader.Read(p)
	if n > 0 && lr.onRead != nil {
		lr.onRead(int64(n))
	}
	return n, err
}

// DownloadSegments downloads all segments using a controlled worker pool and writes them in order.
func DownloadSegments(ctx context.Context, playlist *HLSPlaylist, writer io.Writer, headers map[string]string, concurrency int, progressCb ProgressFunc) error {
	total := len(playlist.Segments)
	return DownloadSegmentsWithOffset(ctx, playlist.Segments, playlist.IsAES128, playlist.KeyBytes, writer, headers, concurrency, 0, total, nil, progressCb)
}

// DownloadSegmentsWithOffset downloads a list of segments with an offset and total count (for video + audio tracks).
func DownloadSegmentsWithOffset(ctx context.Context, segments []HLSSegment, isAES128 bool, keyBytes []byte, writer io.Writer, headers map[string]string, concurrency int, chunkOffset int, totalAllChunks int, sharedByteCounter *int64, progressCb ProgressFunc) error {
	return DownloadSegmentsResume(ctx, segments, isAES128, keyBytes, writer, headers, concurrency, 0, chunkOffset, totalAllChunks, sharedByteCounter, 0, nil, progressCb)
}

// DownloadSegmentsResume downloads a list of segments starting from startIndex (for resuming interrupted downloads).
func DownloadSegmentsResume(ctx context.Context, segments []HLSSegment, isAES128 bool, keyBytes []byte, writer io.Writer, headers map[string]string, concurrency int, startIndex int, chunkOffset int, totalAllChunks int, sharedByteCounter *int64, initialBytes int64, onProgressSave func(committedIndex int, totalBytes int64), progressCb ProgressFunc) error {
	if concurrency <= 0 {
		concurrency = 3
	}
	if totalAllChunks <= 0 {
		totalAllChunks = len(segments)
	}
	if startIndex < 0 {
		startIndex = 0
	}

	totalSegments := len(segments)
	if totalSegments == 0 || startIndex >= totalSegments {
		return nil
	}

	transport := &http.Transport{
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   concurrency + 2,
		IdleConnTimeout:       60 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		DisableCompression:   false,
	}
	client := &http.Client{
		Transport: transport,
	}

	var aesBlock cipher.Block
	if isAES128 && len(keyBytes) == 16 {
		var err error
		aesBlock, err = aes.NewCipher(keyBytes)
		if err != nil {
			return fmt.Errorf("create AES cipher: %w", err)
		}
	}

	taskCh := make(chan HLSSegment, concurrency*2)
	resultCh := make(chan segmentResult, concurrency*2)

	var localDownloadedBytes int64 = initialBytes
	byteCallback := func(n int64) {
		atomic.AddInt64(&localDownloadedBytes, n)
		if sharedByteCounter != nil {
			atomic.AddInt64(sharedByteCounter, n)
		}
	}

	var workerWG sync.WaitGroup

	// Spawn worker pool
	for i := 0; i < concurrency; i++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			for seg := range taskCh {
				select {
				case <-ctx.Done():
					return
				default:
				}

				data, err := fetchSegmentWithRetry(ctx, client, seg, headers, aesBlock, byteCallback)
				select {
				case <-ctx.Done():
					return
				case resultCh <- segmentResult{index: seg.Index, data: data, err: err}:
				}
			}
		}()
	}

	// Feed tasks to workers starting from startIndex
	go func() {
		defer close(taskCh)
		for _, seg := range segments[startIndex:] {
			select {
			case <-ctx.Done():
				return
			case taskCh <- seg:
			}
		}
	}()

	// Close results channel when workers complete
	go func() {
		workerWG.Wait()
		close(resultCh)
	}()

	// Real-time progress ticker for live byte streaming updates
	stopTicker := make(chan struct{})
	startTime := time.Now()
	var currentCommittedChunks int32 = int32(startIndex)

	if progressCb != nil {
		go func() {
			ticker := time.NewTicker(150 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stopTicker:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					var totalBytes int64
					if sharedByteCounter != nil {
						totalBytes = atomic.LoadInt64(sharedByteCounter)
					} else {
						totalBytes = atomic.LoadInt64(&localDownloadedBytes)
					}

					elapsed := time.Since(startTime).Seconds()
					var speed float64
					if elapsed > 0 {
						// Calculate speed based on bytes downloaded in this session
						sessionBytes := totalBytes - initialBytes
						if sessionBytes > 0 {
							speed = float64(sessionBytes) / elapsed
						}
					}

					doneChunks := int(atomic.LoadInt32(&currentCommittedChunks)) + chunkOffset
					var eta time.Duration
					if speed > 0 && doneChunks > startIndex+chunkOffset && doneChunks < totalAllChunks {
						remChunks := totalAllChunks - doneChunks
						sessionChunksDone := doneChunks - (startIndex + chunkOffset)
						sessionBytes := totalBytes - initialBytes
						avgBytes := float64(sessionBytes) / float64(sessionChunksDone)
						eta = time.Duration((float64(remChunks)*avgBytes)/speed) * time.Second
					}

					percent := 0.0
					if totalAllChunks > 0 {
						percent = (float64(doneChunks) / float64(totalAllChunks)) * 100.0
					}

					progressCb(ProgressStats{
						CurrentChunk:     doneChunks,
						TotalChunks:      totalAllChunks,
						DownloadedBytes:  totalBytes,
						SpeedBytesPerSec: speed,
						ETA:              eta,
						Percent:          percent,
					})
				}
			}
		}()
	}

	// Ordered writer loop
	pending := make(map[int][]byte)
	nextExpected := startIndex

	for res := range resultCh {
		if res.err != nil {
			close(stopTicker)
			if onProgressSave != nil {
				var b int64
				if sharedByteCounter != nil {
					b = atomic.LoadInt64(sharedByteCounter)
				} else {
					b = atomic.LoadInt64(&localDownloadedBytes)
				}
				onProgressSave(nextExpected, b)
			}
			return fmt.Errorf("segment %d failed: %w", res.index, res.err)
		}

		pending[res.index] = res.data

		// Write all contiguous available segments in sequence
		for {
			data, found := pending[nextExpected]
			if !found {
				break
			}
			delete(pending, nextExpected)

			if _, err := writer.Write(data); err != nil {
				close(stopTicker)
				return fmt.Errorf("write segment %d to output: %w", nextExpected, err)
			}

			nextExpected++
			atomic.StoreInt32(&currentCommittedChunks, int32(nextExpected))
			if onProgressSave != nil {
				var b int64
				if sharedByteCounter != nil {
					b = atomic.LoadInt64(sharedByteCounter)
				} else {
					b = atomic.LoadInt64(&localDownloadedBytes)
				}
				onProgressSave(nextExpected, b)
			}
		}
	}

	close(stopTicker)

	if onProgressSave != nil {
		var b int64
		if sharedByteCounter != nil {
			b = atomic.LoadInt64(sharedByteCounter)
		} else {
			b = atomic.LoadInt64(&localDownloadedBytes)
		}
		onProgressSave(nextExpected, b)
	}

	if nextExpected < totalSegments {
		return fmt.Errorf("download incomplete: received %d/%d segments", nextExpected, totalSegments)
	}

	// Final progress report
	if progressCb != nil {
		var totalBytes int64
		if sharedByteCounter != nil {
			totalBytes = atomic.LoadInt64(sharedByteCounter)
		} else {
			totalBytes = atomic.LoadInt64(&localDownloadedBytes)
		}
		doneChunks := nextExpected + chunkOffset
		elapsed := time.Since(startTime).Seconds()
		var speed float64
		if elapsed > 0 {
			sessionBytes := totalBytes - initialBytes
			if sessionBytes > 0 {
				speed = float64(sessionBytes) / elapsed
			}
		}
		percent := 100.0
		if totalAllChunks > 0 {
			percent = (float64(doneChunks) / float64(totalAllChunks)) * 100.0
		}
		progressCb(ProgressStats{
			CurrentChunk:     doneChunks,
			TotalChunks:      totalAllChunks,
			DownloadedBytes:  totalBytes,
			SpeedBytesPerSec: speed,
			ETA:              0,
			Percent:          percent,
		})
	}

	return nil
}


func fetchSegmentWithRetry(ctx context.Context, client *http.Client, seg HLSSegment, headers map[string]string, aesBlock cipher.Block, byteProgress func(n int64)) ([]byte, error) {
	const maxRetries = 4
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if attempt > 0 {
			// Exponential backoff with random jitter: (1<<attempt)*600ms + (0..300ms)
			backoff := time.Duration(1<<attempt)*600*time.Millisecond + time.Duration(rand.Intn(300))*time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		// Use 120s timeout per chunk on slow connections instead of strict 35s
		reqCtx, reqCancel := context.WithTimeout(ctx, 120*time.Second)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, seg.URL, nil)
		if err != nil {
			reqCancel()
			return nil, err
		}

		for k, v := range headers {
			if v != "" {
				req.Header.Set(k, v)
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			reqCancel()
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout {
			resp.Body.Close()
			reqCancel()
			lastErr = fmt.Errorf("HTTP %d (rate limit/server busy)", resp.StatusCode)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			reqCancel()
			return nil, fmt.Errorf("HTTP error %d", resp.StatusCode)
		}

		// Read body with live byte tracking
		reader := &liveProgressReader{
			reader: resp.Body,
			onRead: byteProgress,
		}
		data, err := io.ReadAll(reader)
		resp.Body.Close()
		reqCancel()

		if err != nil {
			lastErr = err
			continue
		}

		if len(data) == 0 {
			lastErr = fmt.Errorf("empty segment response")
			continue
		}

		// Decrypt if AES-128
		if aesBlock != nil && len(seg.IV) == 16 {
			data, err = decryptAES128CBC(aesBlock, seg.IV, data)
			if err != nil {
				return nil, fmt.Errorf("AES-128 decrypt segment %d: %w", seg.Index, err)
			}
		}

		return data, nil
	}

	return nil, fmt.Errorf("failed after %d attempts: %w", maxRetries, lastErr)
}

func decryptAES128CBC(block cipher.Block, iv, ciphertext []byte) ([]byte, error) {
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length %d not a multiple of AES block size", len(ciphertext))
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	// Strip PKCS7 padding if present
	if len(plaintext) > 0 {
		padLen := int(plaintext[len(plaintext)-1])
		if padLen > 0 && padLen <= aes.BlockSize && padLen <= len(plaintext) {
			valid := true
			for i := len(plaintext) - padLen; i < len(plaintext); i++ {
				if plaintext[i] != byte(padLen) {
					valid = false
					break
				}
			}
			if valid {
				plaintext = plaintext[:len(plaintext)-padLen]
			}
		}
	}

	return plaintext, nil
}
