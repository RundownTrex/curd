package internal

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/browser"
)

const (
	malOAuthURL     = "https://myanimelist.net/v1/oauth2"
	malAPIURL       = "https://api.myanimelist.net/v2"
	malClientID     = "08953e60f5b60209867d7f535bbba356"
	malClientSecret = "73e137cc11ed13aa4da434c0642a2dfd44a98a4b9450b76caf9ea64af2effe31"
	malRedirectURI  = "http://localhost:8888/oauth/callback"
	malServerPort   = 8888
)

// MALToken represents the OAuth token response from MyAnimeList
type MALToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// MALAnimeListEntry represents a single anime entry from MAL
type MALAnimeListEntry struct {
	Node struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		MainPicture struct {
			Large  string `json:"large"`
			Medium string `json:"medium"`
		} `json:"main_picture"`
		NumEpisodes int `json:"num_episodes"`
	} `json:"node"`
	ListStatus struct {
		Status             string `json:"status"`
		Score              int    `json:"score"`
		NumEpisodesWatched int    `json:"num_episodes_watched"`
		IsRewatching       bool   `json:"is_rewatching"`
	} `json:"list_status"`
}

// MALAnimeList represents the user's anime list from MAL
type MALAnimeList struct {
	Data   []MALAnimeListEntry `json:"data"`
	Paging struct {
		Next string `json:"next"`
	} `json:"paging"`
}

// MALAnimeDetails represents detailed anime information from MAL
type MALAnimeDetails struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	MainPicture struct {
		Large  string `json:"large"`
		Medium string `json:"medium"`
	} `json:"main_picture"`
	AlternativeTitles struct {
		Synonyms []string `json:"synonyms"`
		En       string   `json:"en"`
		Ja       string   `json:"ja"`
	} `json:"alternative_titles"`
	NumEpisodes  int    `json:"num_episodes"`
	Status       string `json:"status"`
	MyListStatus struct {
		Status             string `json:"status"`
		Score              int    `json:"score"`
		NumEpisodesWatched int    `json:"num_episodes_watched"`
		IsRewatching       bool   `json:"is_rewatching"`
	} `json:"my_list_status,omitempty"`
}

// MALUserInfo represents MAL user information
type MALUserInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// generateCodeVerifier generates a random code verifier for PKCE
func generateCodeVerifier() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// generateCodeChallenge generates a code challenge from the verifier
func generateCodeChallenge(verifier string) string {
	// MAL uses plain challenge method
	return verifier
}

// authenticateWithBrowserMAL performs OAuth authentication using browser
func authenticateWithBrowserMAL(tokenPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Try to load existing token first
	if token, err := loadMALToken(tokenPath); err == nil && isMALTokenValid(token) {
		return token.AccessToken, nil
	}

	// Generate PKCE code verifier and challenge
	codeVerifier := generateCodeVerifier()
	codeChallenge := generateCodeChallenge(codeVerifier)

	// Start local server to handle OAuth callback
	callbackCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", malServerPort),
		Handler: mux,
	}

	// Handle OAuth callback
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		errorParam := r.URL.Query().Get("error")

		w.Header().Set("Content-Type", "text/html")

		if errorParam != "" {
			w.WriteHeader(http.StatusBadRequest)
			html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>Curd MAL Authentication</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 50px; text-align: center; background: #2e51a2; color: white; }
        .error { color: #f44336; font-size: 18px; margin-bottom: 20px; }
    </style>
</head>
<body>
    <div class="error">Authentication failed: %s</div>
    <p>You can close this window and try again.</p>
</body>
</html>`, errorParam)
			fmt.Fprint(w, html)
			errCh <- fmt.Errorf("oauth error: %s", errorParam)
			return
		}

		if code == "" {
			w.WriteHeader(http.StatusBadRequest)
			html := `<!DOCTYPE html>
<html>
<head>
    <title>Curd MAL Authentication</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 50px; text-align: center; background: #2e51a2; color: white; }
        .error { color: #f44336; font-size: 18px; margin-bottom: 20px; }
    </style>
</head>
<body>
    <div class="error">No authorization code received</div>
    <p>You can close this window and try again.</p>
</body>
</html>`
			fmt.Fprint(w, html)
			errCh <- fmt.Errorf("no authorization code received")
			return
		}

		// Exchange authorization code for access token
		go func() {
			tokenURL := fmt.Sprintf("%s/token", malOAuthURL)
			data := url.Values{
				"client_id":     {malClientID},
				"client_secret": {malClientSecret},
				"grant_type":    {"authorization_code"},
				"code":          {code},
				"redirect_uri":  {malRedirectURI},
				"code_verifier": {codeVerifier},
			}

			resp, err := http.PostForm(tokenURL, data)
			if err != nil {
				errCh <- fmt.Errorf("failed to exchange code for token: %w", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				errCh <- fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
				return
			}

			var tokenResponse MALToken
			if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
				errCh <- fmt.Errorf("failed to parse token response: %w", err)
				return
			}

			if tokenResponse.AccessToken == "" {
				errCh <- fmt.Errorf("no access token in response")
				return
			}

			callbackCh <- tokenResponse.AccessToken
		}()

		// Show success page immediately
		html := `<!DOCTYPE html>
<html>
<head>
    <title>Curd MAL Authentication</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 50px; text-align: center; background: #2e51a2; color: white; }
        .loading { color: #4CAF50; font-size: 18px; margin-bottom: 20px; }
    </style>
</head>
<body>
    <div class="loading">Processing authentication...</div>
    <p>Exchanging authorization code for token. You can close this window.</p>
</body>
</html>`
		fmt.Fprint(w, html)
	})

	// Start server in background
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("failed to start server: %w", err)
		}
	}()
	defer srv.Shutdown(ctx)

	// Give server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Open browser for authentication
	authURL := fmt.Sprintf("%s/authorize?response_type=code&client_id=%s&redirect_uri=%s&code_challenge=%s&code_challenge_method=plain",
		malOAuthURL,
		malClientID,
		url.QueryEscape(malRedirectURI),
		codeChallenge)

	fmt.Println("Opening browser for MyAnimeList authentication...")
	fmt.Printf("If the browser doesn't open automatically, visit: %s\n", authURL)

	if err := browser.OpenURL(authURL); err != nil {
		fmt.Printf("Failed to open browser automatically: %v\n", err)
		fmt.Println("Please copy and paste the URL above into your browser")
	}

	// Wait for token
	var accessToken string
	select {
	case accessToken = <-callbackCh:
	case err := <-errCh:
		return "", fmt.Errorf("authentication failed: %w", err)
	case <-ctx.Done():
		return "", fmt.Errorf("authentication timeout after 5 minutes")
	}

	// Create token object and save
	token := &MALToken{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   2592000, // MAL tokens are valid for 30 days
		ExpiresAt:   time.Now().Add(30 * 24 * time.Hour),
	}

	// Save token to file
	if err := saveMALToken(tokenPath, token); err != nil {
		return "", fmt.Errorf("failed to save token: %w", err)
	}

	fmt.Println("MyAnimeList authentication successful!")
	return token.AccessToken, nil
}

// loadMALToken loads the token from the token file
func loadMALToken(tokenPath string) (*MALToken, error) {
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	var token MALToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	return &token, nil
}

// saveMALToken saves the token to the token file
func saveMALToken(tokenPath string, token *MALToken) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	return os.WriteFile(tokenPath, data, 0600)
}

// isMALTokenValid checks if the token is still valid
func isMALTokenValid(token *MALToken) bool {
	return token != nil && token.AccessToken != "" && time.Now().Before(token.ExpiresAt)
}

// GetMALTokenFromFile loads the token from the token file
func GetMALTokenFromFile(tokenPath string) (string, error) {
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", fmt.Errorf("failed to read token from file: %w", err)
	}

	var token MALToken
	if err := json.Unmarshal(data, &token); err != nil {
		return "", fmt.Errorf("failed to parse token file: %w", err)
	}

	if !isMALTokenValid(&token) {
		return "", fmt.Errorf("token has expired")
	}

	return token.AccessToken, nil
}

// ChangeMALToken handles MAL OAuth token creation/change
func ChangeMALToken(config *CurdConfig, user *User) {
	var err error
	tokenPath := filepath.Join(os.ExpandEnv(config.StoragePath), "mal_token.json")

	// Try browser-based OAuth first
	fmt.Println("Starting MyAnimeList browser-based authentication...")
	user.Token, err = authenticateWithBrowserMAL(tokenPath)

	if err != nil {
		Log("MAL browser authentication failed: " + err.Error())
		fmt.Printf("MAL browser authentication failed: %v\n", err)
		ExitCurd(fmt.Errorf("MAL authentication failed"))
	}

	if user.Token == "" {
		ExitCurd(fmt.Errorf("no MAL token provided"))
	}

	fmt.Println("MAL token saved successfully!")
}

// GetMALUserInfo retrieves MAL user information
func GetMALUserInfo(token string) (int, string, error) {
	apiURL := fmt.Sprintf("%s/users/@me", malAPIURL)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return 0, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, "", fmt.Errorf("failed to get user info. Status Code: %d, Response: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", fmt.Errorf("failed to read response body: %w", err)
	}

	var userInfo MALUserInfo
	err = json.Unmarshal(body, &userInfo)
	if err != nil {
		return 0, "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return userInfo.ID, userInfo.Name, nil
}

// GetMALUserAnimeList retrieves the user's anime list from MAL
func GetMALUserAnimeList(token string) (map[string]interface{}, error) {
	allData := []MALAnimeListEntry{}
	nextURL := fmt.Sprintf("%s/users/@me/animelist?fields=list_status,num_episodes&limit=1000", malAPIURL)

	for nextURL != "" {
		req, err := http.NewRequest("GET", nextURL, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Authorization", "Bearer "+token)

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to make request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("failed to get anime list. Status Code: %d, Response: %s", resp.StatusCode, string(body))
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}

		var animeList MALAnimeList
		err = json.Unmarshal(body, &animeList)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}

		allData = append(allData, animeList.Data...)
		nextURL = animeList.Paging.Next
	}

	// Convert to the format expected by ParseAnimeList
	result := map[string]interface{}{
		"data": map[string]interface{}{
			"MediaListCollection": map[string]interface{}{
				"lists": []interface{}{
					map[string]interface{}{
						"entries": convertMALToAnilistFormat(allData),
					},
				},
			},
		},
	}

	return result, nil
}

// convertMALToAnilistFormat converts MAL anime list to Anilist-compatible format
func convertMALToAnilistFormat(malEntries []MALAnimeListEntry) []interface{} {
	entries := make([]interface{}, 0, len(malEntries))

	for _, malEntry := range malEntries {
		// Map MAL status to AniList status
		status := "CURRENT"
		switch malEntry.ListStatus.Status {
		case "watching":
			status = "CURRENT"
		case "completed":
			status = "COMPLETED"
		case "on_hold":
			status = "PAUSED"
		case "dropped":
			status = "DROPPED"
		case "plan_to_watch":
			status = "PLANNING"
		}

		if malEntry.ListStatus.IsRewatching {
			status = "REPEATING"
		}

		entry := map[string]interface{}{
			"media": map[string]interface{}{
				"id":       malEntry.Node.ID,
				"episodes": malEntry.Node.NumEpisodes,
				"duration": 24, // Default duration, MAL doesn't provide this easily
				"title": map[string]interface{}{
					"romaji":  malEntry.Node.Title,
					"english": malEntry.Node.Title,
					"native":  malEntry.Node.Title,
				},
				"coverImage": map[string]interface{}{
					"large": malEntry.Node.MainPicture.Large,
				},
			},
			"status":   status,
			"score":    float64(malEntry.ListStatus.Score),
			"progress": malEntry.ListStatus.NumEpisodesWatched,
		}

		entries = append(entries, entry)
	}

	return entries
}

// SearchAnimeMAL searches for anime on MAL
func SearchAnimeMAL(query, token string) ([]SelectionOption, error) {
	apiURL := fmt.Sprintf("%s/anime?q=%s&limit=10", malAPIURL, url.QueryEscape(query))

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to search for anime. Status Code: %d, Response: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var searchResult struct {
		Data []struct {
			Node struct {
				ID    int    `json:"id"`
				Title string `json:"title"`
			} `json:"node"`
		} `json:"data"`
	}

	err = json.Unmarshal(body, &searchResult)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	var results []SelectionOption
	type scoredAnime struct {
		id    string
		title string
		score int
	}
	var scored []scoredAnime

	for _, anime := range searchResult.Data {
		idStr := strconv.Itoa(anime.Node.ID)
		title := anime.Node.Title
		score := levenshtein(title, query)
		scored = append(scored, scoredAnime{idStr, title, score})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score < scored[j].score
	})

	for _, s := range scored {
		results = append(results, SelectionOption{
			Key:   s.id,
			Label: s.title,
		})
	}

	return results, nil
}

// SearchAnimeMALPreview searches for anime on MAL with image preview
func SearchAnimeMALPreview(query, token string) (map[string]RofiSelectPreview, error) {
	apiURL := fmt.Sprintf("%s/anime?q=%s&limit=10&fields=main_picture", malAPIURL, url.QueryEscape(query))

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to search for anime. Status Code: %d, Response: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var searchResult struct {
		Data []struct {
			Node struct {
				ID          int    `json:"id"`
				Title       string `json:"title"`
				MainPicture struct {
					Large  string `json:"large"`
					Medium string `json:"medium"`
				} `json:"main_picture"`
			} `json:"node"`
		} `json:"data"`
	}

	err = json.Unmarshal(body, &searchResult)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	animeDict := make(map[string]RofiSelectPreview)
	type scoredAnime struct {
		id    string
		title string
		cover string
		score int
	}
	var scored []scoredAnime

	for _, anime := range searchResult.Data {
		idStr := strconv.Itoa(anime.Node.ID)
		title := anime.Node.Title
		cover := anime.Node.MainPicture.Large
		if cover == "" {
			cover = anime.Node.MainPicture.Medium
		}
		score := levenshtein(title, query)
		scored = append(scored, scoredAnime{idStr, title, cover, score})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score < scored[j].score
	})

	for _, s := range scored {
		animeDict[s.id] = RofiSelectPreview{
			Title:      s.title,
			CoverImage: s.cover,
		}
	}

	return animeDict, nil
}

// UpdateMALAnimeProgress updates anime progress on MAL
func UpdateMALAnimeProgress(token string, mediaID, progress int) error {
	apiURL := fmt.Sprintf("%s/anime/%d/my_list_status", malAPIURL, mediaID)

	data := url.Values{
		"num_watched_episodes": {strconv.Itoa(progress)},
	}

	req, err := http.NewRequest("PATCH", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to update progress. Status Code: %d, Response: %s", resp.StatusCode, string(body))
	}

	CurdOut(fmt.Sprint("MyAnimeList progress updated! Latest watched episode: ", progress))
	return nil
}

// UpdateMALAnimeStatus updates anime status on MAL
func UpdateMALAnimeStatus(token string, mediaID int, status string) error {
	apiURL := fmt.Sprintf("%s/anime/%d/my_list_status", malAPIURL, mediaID)

	// Map AniList status to MAL status
	malStatus := ""
	switch status {
	case "CURRENT":
		malStatus = "watching"
	case "COMPLETED":
		malStatus = "completed"
	case "PAUSED":
		malStatus = "on_hold"
	case "DROPPED":
		malStatus = "dropped"
	case "PLANNING":
		malStatus = "plan_to_watch"
	case "REPEATING":
		malStatus = "watching"
	}

	data := url.Values{
		"status": {malStatus},
	}

	// Set completion date when marking as completed
	if status == "COMPLETED" {
		// Get current date in YYYY-MM-DD format
		currentDate := time.Now().Format("2006-01-02")
		data.Set("finish_date", currentDate)
	}

	if status == "REPEATING" {
		data.Set("is_rewatching", "true")
	}

	req, err := http.NewRequest("PATCH", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to update status. Status Code: %d, Response: %s", resp.StatusCode, string(body))
	}

	statusMap := map[string]string{
		"CURRENT":   "Currently Watching",
		"COMPLETED": "Completed",
		"PAUSED":    "On Hold",
		"DROPPED":   "Dropped",
		"PLANNING":  "Plan to Watch",
		"REPEATING": "Rewatching",
	}

	CurdOut(fmt.Sprintf("MyAnimeList status updated to: %s", statusMap[status]))
	return nil
}

// RateAnimeMAL rates an anime on MAL (score 0-10)
func RateAnimeMAL(token string, mediaID int) error {
	var score int
	var err error

	userCurdConfig := GetGlobalConfig()
	if userCurdConfig == nil {
		return fmt.Errorf("failed to get curd config")
	}

	if userCurdConfig.RofiSelection {
		userInput, err := GetUserInputFromRofi("Enter a score for the anime (0-10)")
		if err != nil {
			return err
		}
		score, err = strconv.Atoi(userInput)
		if err != nil {
			return err
		}
	} else {
		fmt.Println("Rate this anime (0-10): ")
		fmt.Scanln(&score)
	}

	if score < 0 || score > 10 {
		return fmt.Errorf("score must be between 0 and 10")
	}

	apiURL := fmt.Sprintf("%s/anime/%d/my_list_status", malAPIURL, mediaID)

	data := url.Values{
		"score": {strconv.Itoa(score)},
	}

	req, err := http.NewRequest("PATCH", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to rate anime. Status Code: %d, Response: %s", resp.StatusCode, string(body))
	}

	CurdOut(fmt.Sprintf("Successfully rated anime (mediaId: %d) with score: %d", mediaID, score))
	return nil
}

// AddAnimeToMALWatchingList adds an anime to MAL watching list
func AddAnimeToMALWatchingList(animeID int, token string) error {
	apiURL := fmt.Sprintf("%s/anime/%d/my_list_status", malAPIURL, animeID)

	data := url.Values{
		"status": {"watching"},
	}

	req, err := http.NewRequest("PATCH", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to add anime. Status Code: %d, Response: %s", resp.StatusCode, string(body))
	}

	CurdOut(fmt.Sprintf("Anime with ID %d has been added to your MyAnimeList watching list.", animeID))
	return nil
}

// GetMALAnimeDetails gets detailed information about an anime from MAL
func GetMALAnimeDetails(malID int, token string) (Anime, error) {
	apiURL := fmt.Sprintf("%s/anime/%d?fields=num_episodes,status,my_list_status", malAPIURL, malID)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return Anime{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return Anime{}, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return Anime{}, fmt.Errorf("failed to get anime details. Status Code: %d, Response: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Anime{}, fmt.Errorf("failed to read response body: %w", err)
	}

	var details MALAnimeDetails
	err = json.Unmarshal(body, &details)
	if err != nil {
		return Anime{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	anime := Anime{
		MalId:         details.ID,
		TotalEpisodes: details.NumEpisodes,
		IsAiring:      details.Status == "currently_airing",
	}

	return anime, nil
}

// ConvertAnilistIDToMAL converts an AniList ID to MAL ID using Jikan/other methods
// This is a helper function to bridge between AniList and MAL
func ConvertAnilistIDToMAL(anilistID int) (int, error) {
	// Use the existing GetAnimeMalID function from anilist.go
	return GetAnimeMalID(anilistID)
}

// ConvertMALIDToAnilist converts a MAL ID to AniList ID
// Note: This requires querying AniList API with MAL ID
func ConvertMALIDToAnilist(malID int, token string) (int, error) {
	url := "https://graphql.anilist.co"
	query := `
	query ($malId: Int) {
		Media(idMal: $malId, type: ANIME) {
			id
		}
	}`

	variables := map[string]interface{}{
		"malId": malID,
	}

	requestBody, err := json.Marshal(map[string]interface{}{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("failed with status %d: %s", resp.StatusCode, body)
	}

	var responseData map[string]interface{}
	err = json.Unmarshal(body, &responseData)
	if err != nil {
		return 0, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	data, ok := responseData["data"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("invalid response format")
	}

	media, ok := data["Media"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("anime not found")
	}

	anilistID := int(media["id"].(float64))
	return anilistID, nil
}
