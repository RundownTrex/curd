package hianime

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/wraient/curd/internal/curdhost"
)

const (
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"
	xorKey    = "otaku-embed-v1"
)

var baseURL = "https://hianime.at"

func newRequest(method, rawURL, referer string) (*http.Request, error) {
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", userAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	return req, nil
}

func fetchString(rawURL, referer string) (string, error) {
	client := curdhost.HTTPClient()
	if client == nil {
		return "", fmt.Errorf("curdhost HTTPClient is not initialized")
	}

	req, err := newRequest(http.MethodGet, rawURL, referer)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("hianime request to %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response from %s: %w", rawURL, err)
	}

	if !curdhost.HTTPStatusOK(resp.StatusCode) {
		return "", curdhost.HTTPStatusError("hianime request", resp.StatusCode, body)
	}

	bodyStr := string(body)
	if strings.Contains(strings.ToLower(bodyStr), "<title>just a moment") ||
		strings.Contains(strings.ToLower(bodyStr), "challenge-running") {
		return "", fmt.Errorf("hianime request blocked by Cloudflare challenge")
	}

	return bodyStr, nil
}

func fetchJSON(rawURL, referer string, dest any) error {
	client := curdhost.HTTPClient()
	if client == nil {
		return fmt.Errorf("curdhost HTTPClient is not initialized")
	}

	req, err := newRequest(http.MethodGet, rawURL, referer)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("hianime JSON request to %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read JSON response from %s: %w", rawURL, err)
	}

	if !curdhost.HTTPStatusOK(resp.StatusCode) {
		return curdhost.HTTPStatusError("hianime JSON request", resp.StatusCode, body)
	}

	if dest != nil {
		if err := json.Unmarshal(body, dest); err != nil {
			return fmt.Errorf("parse hianime JSON from %s: %w", rawURL, err)
		}
	}
	return nil
}

// decodeBase64Safe decodes standard, url-encoded, and unpadded base64 strings.
func decodeBase64Safe(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if pad := len(s) % 4; pad > 0 {
		sPadded := s + strings.Repeat("=", 4-pad)
		if b, err := base64.StdEncoding.DecodeString(sPadded); err == nil {
			return b, nil
		}
		if b, err := base64.URLEncoding.DecodeString(sPadded); err == nil {
			return b, nil
		}
	}
	return base64.RawStdEncoding.DecodeString(s)
}

// deobfuscateBlob decodes and XOR-decrypts the ZokoAnime player config blob.
func deobfuscateBlob(blob string) ([]byte, error) {
	blob = strings.TrimSpace(blob)
	if blob == "" {
		return nil, fmt.Errorf("empty obfuscated blob")
	}

	decoded, err := decodeBase64Safe(blob)
	if err != nil {
		return nil, fmt.Errorf("base64 decode blob: %w", err)
	}

	key := []byte(xorKey)
	plain := make([]byte, len(decoded))
	for i := range decoded {
		plain[i] = decoded[i] ^ key[i%len(key)]
	}
	return plain, nil
}
