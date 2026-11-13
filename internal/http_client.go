package internal

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

var sharedHTTPClient *http.Client

func httpStatusOK(statusCode int) bool {
	return statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
}

func httpStatusError(context string, statusCode int, body []byte) error {
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 300 {
		snippet = snippet[:300] + "..."
	}
	if snippet != "" {
		return fmt.Errorf("%s failed with status %d: %s", context, statusCode, snippet)
	}
	return fmt.Errorf("%s failed with status %d", context, statusCode)
}

func init() {
	jar, _ := cookiejar.New(nil)
	sharedHTTPClient = &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        50,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  false,
		},
		Timeout: 15 * time.Second,
		Jar:     jar,
	}
}
