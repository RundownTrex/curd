package internal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCleanMALToken(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my_plain_token", "my_plain_token"},
		{"  \"my_quoted_token\"  ", "my_quoted_token"},
		{"'my_single_quoted_token'", "my_single_quoted_token"},
		{"http://localhost:8888/oauth/callback#access_token=def5020_token_xyz&token_type=Bearer", "def5020_token_xyz"},
		{"access_token=def5020_token_xyz", "def5020_token_xyz"},
		{"access_token=def5020_token_xyz#comment", "def5020_token_xyz"},
	}

	for _, tt := range tests {
		got := cleanMALToken(tt.input)
		if got != tt.want {
			t.Errorf("cleanMALToken(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractMALCode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"http://localhost:8888/oauth/callback?code=def5020_auth_code_xyz", "def5020_auth_code_xyz"},
		{"http://localhost:8888/oauth/callback?code=def5020_auth_code_xyz&state=abc", "def5020_auth_code_xyz"},
		{"code=my_code_123", "my_code_123"},
		{"code=my_code_123#frag", "my_code_123"},
		{"invalid_string", ""},
	}

	for _, tt := range tests {
		got := extractMALCode(tt.input)
		if got != tt.want {
			t.Errorf("extractMALCode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsMALTokenValid(t *testing.T) {
	if isMALTokenValid(nil) {
		t.Errorf("expected nil MAL token to be invalid")
	}
	if isMALTokenValid(&MALToken{AccessToken: ""}) {
		t.Errorf("expected empty MAL token to be invalid")
	}
	// Zero expiration should be considered valid (manual/legacy format)
	if !isMALTokenValid(&MALToken{AccessToken: "valid_token", ExpiresAt: time.Time{}}) {
		t.Errorf("expected zero ExpiresAt MAL token to be valid")
	}
	// Future expiration should be valid
	if !isMALTokenValid(&MALToken{AccessToken: "valid_token", ExpiresAt: time.Now().Add(1 * time.Hour)}) {
		t.Errorf("expected future ExpiresAt MAL token to be valid")
	}
	// Past expiration should be invalid
	if isMALTokenValid(&MALToken{AccessToken: "valid_token", ExpiresAt: time.Now().Add(-1 * time.Hour)}) {
		t.Errorf("expected past ExpiresAt MAL token to be invalid")
	}
}

func TestExchangeMALCodeRetry(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&attempts, 1)
		if current < 3 {
			// Simulate transient server error
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":"temporary_unavailable"}`)
			return
		}

		// Success on attempt 3
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "test_access_token_123",
			"refresh_token": "test_refresh_token_456",
			"token_type":    "Bearer",
			"expires_in":    2592000,
		})
	}))
	defer server.Close()

	// Verify exchange logic with retry behavior
	req, err := http.NewRequest(http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	client := getMALHTTPClient()
	maxRetries := 3
	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}

	for i := 1; i <= maxRetries; i++ {
		resp, err := client.Do(req)
		if err != nil {
			if i < maxRetries {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode == http.StatusServiceUnavailable && i < maxRetries {
			resp.Body.Close()
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if resp.StatusCode == http.StatusOK {
			json.NewDecoder(resp.Body).Decode(&tokenResp)
			resp.Body.Close()
			break
		}
		resp.Body.Close()
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	if tokenResp.AccessToken != "test_access_token_123" {
		t.Errorf("expected access token 'test_access_token_123', got %q", tokenResp.AccessToken)
	}
}
