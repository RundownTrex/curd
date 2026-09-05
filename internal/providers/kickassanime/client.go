package kickassanime

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wraient/curd/internal/curdhost"
)

const (
	baseURL      = "https://kaa.lt"
	kaaUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

var defaultHTTPClient = &http.Client{Timeout: 15 * time.Second}

func httpClient() *http.Client {
	if curdhost.HTTPClient != nil {
		if c := curdhost.HTTPClient(); c != nil {
			return c
		}
	}
	return defaultHTTPClient
}

func fetchBytes(u string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", kaaUserAgent)
	req.Header.Set("Accept", "*/*")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if !curdhost.HTTPStatusOK(resp.StatusCode) {
		return nil, curdhost.HTTPStatusError("kickassanime request", resp.StatusCode, body)
	}
	return body, nil
}

func postJSON(u string, payload any, headers map[string]string) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", u, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", kaaUserAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if !curdhost.HTTPStatusOK(resp.StatusCode) {
		return nil, curdhost.HTTPStatusError("kickassanime search", resp.StatusCode, body)
	}
	return body, nil
}

func sanitizeMediaURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, `"'`)
	if strings.HasPrefix(raw, "https:////") {
		raw = "https://" + strings.TrimPrefix(raw, "https:////")
	} else if strings.HasPrefix(raw, "http:////") {
		raw = "http://" + strings.TrimPrefix(raw, "http:////")
	} else if strings.HasPrefix(raw, "https:///") {
		raw = "https://" + strings.TrimPrefix(raw, "https:///")
	} else if strings.HasPrefix(raw, "http:///") {
		raw = "http://" + strings.TrimPrefix(raw, "http:///")
	} else if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	return raw
}
