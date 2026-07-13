package megaplay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/wraient/curd/internal/curdhost"
)

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"

var (
	megaplayBaseURL = "https://megaplay.buzz"
	anilistGQLURL   = "https://graphql.anilist.co"
)

func newRequest(method, rawURL, referer string) (*http.Request, error) {
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	return req, nil
}

func fetchString(rawURL, referer string) (string, error) {
	req, err := newRequest(http.MethodGet, rawURL, referer)
	if err != nil {
		return "", err
	}
	resp, err := curdhost.HTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if !curdhost.HTTPStatusOK(resp.StatusCode) {
		return "", curdhost.HTTPStatusError("megaplay request", resp.StatusCode, body)
	}
	return string(body), nil
}

func fetchJSON(rawURL, referer string, dest any) error {
	req, err := newRequest(http.MethodGet, rawURL, referer)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := curdhost.HTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if !curdhost.HTTPStatusOK(resp.StatusCode) {
		return curdhost.HTTPStatusError("megaplay request", resp.StatusCode, raw)
	}
	if dest == nil {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("parse megaplay response: %w", err)
	}
	return nil
}

func searchAniList(query string) (*anilistResponse, error) {
	queryEscaped, err := json.Marshal(query)
	if err != nil {
		return nil, err
	}
	// queryEscaped includes quotes, e.g. "Naruto"
	// We want to embed it into the GraphQL string, which itself is inside a JSON string.
	// Actually, the safest way is to construct a proper struct and json.Marshal the whole payload!
	payload := map[string]string{
		"query": fmt.Sprintf(`query{Page(perPage:20){media(search:%s,type:ANIME,sort:SEARCH_MATCH){id title{english romaji}idMal episodes}}}`, string(queryEscaped)),
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, anilistGQLURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := curdhost.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if !curdhost.HTTPStatusOK(resp.StatusCode) {
		return nil, curdhost.HTTPStatusError("anilist graphql", resp.StatusCode, raw)
	}

	var result anilistResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse anilist response: %w", err)
	}
	return &result, nil
}

func fetchAniListByMalID(malID int) (*anilistMedia, error) {
	gql := fmt.Sprintf(
		`{"query":"query{Media(idMal:%d,type:ANIME){title{english romaji}idMal episodes}}"}`,
		malID,
	)

	req, err := http.NewRequest(http.MethodPost, anilistGQLURL, bytes.NewReader([]byte(gql)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := curdhost.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if !curdhost.HTTPStatusOK(resp.StatusCode) {
		return nil, curdhost.HTTPStatusError("anilist graphql", resp.StatusCode, raw)
	}

	var result struct {
		Data struct {
			Media *anilistMedia `json:"Media"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse anilist response: %w", err)
	}
	if result.Data.Media == nil {
		return nil, fmt.Errorf("no anilist media found for mal id %d", malID)
	}
	return result.Data.Media, nil
}
