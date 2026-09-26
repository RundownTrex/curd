package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"

	// "io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	anilistOAuthURL     = "https://anilist.co/api/v2/oauth"
	anilistClientID     = "20686"
	anilistClientSecret = "APfx41cOgSQVMvi88v7PbN7g6kzed2ZQRcxmACod"
	anilistRedirectURI  = "http://localhost:8000/oauth/callback"
	anilistServerPort   = 8000
)

// AnilistToken represents the OAuth token response from Anilist
type AnilistToken struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// CurdConfig struct with field names that match the config keys
type CurdConfig struct {
	Player                   string   `config:"Player"`
	MpvArgs                  []string `config:MpvArgs`
	SubsLanguage             string   `config:"SubsLanguage"`
	SubOrDub                 string   `config:"SubOrDub"`
	StoragePath              string   `config:"StoragePath"`
	AnimeNameLanguage        string   `config:"AnimeNameLanguage"`
	MenuOrder                string   `config:"MenuOrder"`
	TrackingService          string   `config:"TrackingService"`
	DualTracking             bool     `config:"DualTracking"`
	PercentageToMarkComplete int      `config:"PercentageToMarkComplete"`
	NextEpisodePrompt        bool     `config:"NextEpisodePrompt"`
	SkipOp                   bool     `config:"SkipOp"`
	SkipEd                   bool     `config:"SkipEd"`
	SkipFiller               bool     `config:"SkipFiller"`
	ImagePreview             bool     `config:"ImagePreview"`
	SkipRecap                bool     `config:"SkipRecap"`
	RofiSelection            bool     `config:"RofiSelection"`
	CurrentCategory          bool     `config:"CurrentCategory"`
	ScoreOnCompletion        bool     `config:"ScoreOnCompletion"`
	SaveMpvSpeed             bool     `config:"SaveMpvSpeed"`
	AddMissingOptions        bool     `config:"AddMissingOptions"`
	AlternateScreen          bool     `config:"AlternateScreen"`
	DiscordPresence          bool     `config:"DiscordPresence"`
	DiscordClientId          string   `config:"DiscordClientId"`
	Provider                 string   `config:"Provider"`
	DisabledProviders        string   `config:"DisabledProviders"`
	ManualProviderSearch     bool     `config:"ManualProviderSearch"`
	SubStyle                 string   `config:"SubStyle"`
	AndroidPlayerPackage     string   `config:"AndroidPlayerPackage"`
	AndroidPlayerActivity    string   `config:"AndroidPlayerActivity"`
	AndroidUseTermuxAPI      bool     `config:"AndroidUseTermuxAPI"`
	DownloadQuality          string   `config:"DownloadQuality"`
	DownloadConcurrency      int      `config:"DownloadConcurrency"`
}

// Default configuration values as a map
func defaultConfigMap() map[string]string {
	defaults := map[string]string{
		"Player":                   "mpv",
		"MpvArgs":                  "[]",
		"StoragePath":              "$HOME/.local/share/curd",
		"AnimeNameLanguage":        "english",
		"SubsLanguage":             "english",
		"MenuOrder":                "CURRENT,ALL,UNTRACKED,DOWNLOAD,UPDATE,CONTINUE_LAST,PROVIDER",
		"TrackingService":          "mal",
		"DualTracking":             "true",
		"SubOrDub":                 "sub",
		"PercentageToMarkComplete": "85",
		"NextEpisodePrompt":        "false",
		"SkipOp":                   "true",
		"SkipEd":                   "true",
		"SkipFiller":               "true",
		"SkipRecap":                "true",
		"RofiSelection":            "false",
		"ImagePreview":             "false",
		"ScoreOnCompletion":        "true",
		"SaveMpvSpeed":             "true",
		"AddMissingOptions":        "true",
		"AlternateScreen":          "true",
		"DiscordPresence":          "true",
		"DiscordClientId":          "1287457464148820089",
		"Provider":                 "[\"anineko\"]",
		"DisabledProviders":        "[]",
		"ManualProviderSearch":     "false",
		"SubStyle":                 "ask",
		"AndroidPlayerPackage":     "is.xyz.mpv",
		"AndroidPlayerActivity":    ".MPVActivity",
		"AndroidUseTermuxAPI":      "true",
		"DownloadQuality":          "best",
		"DownloadConcurrency":      "3",
	}

	if IsAndroid() {
		defaults["RofiSelection"] = "false"
		defaults["ImagePreview"] = "false"
		defaults["DiscordPresence"] = "false"
		defaults["AlternateScreen"] = "false"
	}

	return defaults
}

var globalConfig *CurdConfig
var GlobalConfigPath string

func SetGlobalConfig(config *CurdConfig) {
	globalConfig = config
}

func GetGlobalConfig() *CurdConfig {
	return globalConfig
}

// Helper function to parse string array from config
func parseStringArray(value string) []string {
	// Remove brackets and split by comma
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")
	if value == "" {
		return nil
	}

	// Split by comma and trim spaces and quotes from each element
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		// Trim spaces and quotes
		part = strings.TrimSpace(part)
		part = strings.Trim(part, "\"")
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// DefaultConfigPath returns the default configuration file path based on the operating platform.
// On Android/Termux, it prefers ~/.config/curd/android.conf, falling back to curd.conf if present.
func DefaultConfigPath() string {
	var homeDir string
	if runtime.GOOS == "windows" {
		homeDir = os.Getenv("USERPROFILE")
	} else {
		homeDir = os.Getenv("HOME")
	}

	curdDir := filepath.Join(homeDir, ".config", "curd")
	if IsAndroid() {
		androidPath := filepath.Join(curdDir, "android.conf")
		curdPath := filepath.Join(curdDir, "curd.conf")
		if _, err := os.Stat(androidPath); err == nil {
			return androidPath
		}
		if _, err := os.Stat(curdPath); err == nil {
			return curdPath
		}
		return androidPath
	}

	return filepath.Join(curdDir, "curd.conf")
}

// SanitizeConfigForPlatform enforces platform-specific invariants.
// On Android/Termux, desktop-only features (Rofi, Image Preview, Discord RPC) are strictly disabled.
func SanitizeConfigForPlatform(config *CurdConfig) {
	if config == nil {
		return
	}
	if IsAndroid() {
		config.RofiSelection = false
		config.ImagePreview = false
		config.DiscordPresence = false
		config.AlternateScreen = false
		pkg, act := ResolveAndroidPlayer(config)
		config.AndroidPlayerPackage = pkg
		config.AndroidPlayerActivity = act
	}
}

// LoadConfig reads or creates the config file, adds missing fields, and returns the populated CurdConfig struct
func LoadConfig(configPath string) (CurdConfig, error) {
	configPath = os.ExpandEnv(configPath) // Substitute environment variables like $HOME

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create the config file with default values if it doesn't exist
		CurdOut("Config file not found. Creating default config...")
		if err := createDefaultConfig(configPath); err != nil {
			return CurdConfig{}, fmt.Errorf("error creating default config file: %v", err)
		}
	}

	// Load the config from file
	configMap, err := LoadConfigFromFile(configPath)
	if err != nil {
		return CurdConfig{}, fmt.Errorf("error loading config file: %v", err)
	}

	// Check AddMissingOptions setting first
	addMissing := true
	if val, exists := configMap["AddMissingOptions"]; exists {
		addMissing, _ = strconv.ParseBool(val)
	}

	// Add missing fields to the config map
	updated := false
	defaultConfigMap := defaultConfigMap()
	for key, defaultValue := range defaultConfigMap {
		if _, exists := configMap[key]; !exists {
			configMap[key] = defaultValue
			updated = true
		}
	}

	// Ensure DOWNLOAD is in MenuOrder
	if menuOrderVal, exists := configMap["MenuOrder"]; exists {
		if !strings.Contains(menuOrderVal, "DOWNLOAD") {
			if strings.Contains(menuOrderVal, "UPDATE") {
				configMap["MenuOrder"] = strings.Replace(menuOrderVal, "UPDATE", "DOWNLOAD,UPDATE", 1)
			} else {
				configMap["MenuOrder"] = menuOrderVal + ",DOWNLOAD"
			}
			updated = true
		}
	}

	// Write updated config back to file only if AddMissingOptions is true
	if addMissing && updated {
		if err := SaveConfigToFile(configPath, configMap); err != nil {
			return CurdConfig{}, fmt.Errorf("error saving updated config file: %v", err)
		}
	}

	// Parse string arrays
	if mpvArgs, exists := configMap["MpvArgs"]; exists {
		configMap["MpvArgs"] = mpvArgs
	}

	if providerValue, exists := configMap["Provider"]; exists {
		normalizedProviderValue := canonicalProviderConfigValue(providerValue)
		if normalizedProviderValue != providerValue {
			configMap["Provider"] = normalizedProviderValue
			if addMissing {
				_ = SaveConfigToFile(configPath, configMap)
			}
		}
	}

	// Populate the CurdConfig struct from the config map
	config := PopulateConfig(configMap)
	SanitizeConfigForPlatform(&config)

	return config, nil
}

// Create a config file with default values in key=value format
// Ensure the directory exists before creating the file
func createDefaultConfig(path string) error {
	defaultConfig := defaultConfigMap()

	// Ensure the directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("error creating directory: %v", err)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("error creating file: %v", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for key, value := range defaultConfig {
		line := fmt.Sprintf("%s=%s\n", key, value)
		if _, err := writer.WriteString(line); err != nil {
			return fmt.Errorf("error writing to file: %v", err)
		}
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("error flushing writer: %v", err)
	}
	return nil
}

// authenticateWithBrowser performs OAuth authentication using browser
func authenticateWithBrowser(tokenPath string, forceReauth bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Try to load existing token first (skip if forcing re-authentication)
	if !forceReauth {
		if token, err := loadToken(tokenPath); err == nil && isTokenValid(token) {
			return token.AccessToken, nil
		}
	}

	// Start local server to handle OAuth callback
	callbackCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", anilistServerPort),
		Handler: mux,
	}

	// Endpoint called by browser JavaScript to deliver the access token from URL hash fragment
	mux.HandleFunc("/oauth/save_token", func(w http.ResponseWriter, r *http.Request) {
		token := cleanAccessToken(r.URL.Query().Get("token"))
		if token != "" {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "OK")
			select {
			case callbackCh <- token:
			default:
			}
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	})

	// Handle OAuth callback
	mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		errorParam := r.URL.Query().Get("error")
		w.Header().Set("Content-Type", "text/html")

		if errorParam != "" {
			w.WriteHeader(http.StatusBadRequest)
			html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <title>Curd Authentication</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 50px; text-align: center; background: #1a1a1a; color: white; }
        .error { color: #f44336; font-size: 18px; margin-bottom: 20px; }
    </style>
</head>
<body>
    <div class="error">Authentication failed: %s</div>
    <p>You can close this window and try again.</p>
</body>
</html>`, errorParam)
			fmt.Fprint(w, html)
			select {
			case errCh <- fmt.Errorf("oauth error: %s", errorParam):
			default:
			}
			return
		}

		// Also support code grant fallback if present
		if code := r.URL.Query().Get("code"); code != "" {
			go func() {
				tokenURL := fmt.Sprintf("%s/token", anilistOAuthURL)
				data := url.Values{
					"grant_type":    {"authorization_code"},
					"client_id":     {anilistClientID},
					"client_secret": {anilistClientSecret},
					"redirect_uri":  {anilistRedirectURI},
					"code":          {code},
				}

				req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
				if err != nil {
					return
				}
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Header.Set("Accept", "application/json")
				req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36")

				client := sharedHTTPClient
				if client == nil {
					client = &http.Client{Timeout: 15 * time.Second}
				}
				resp, err := client.Do(req)
				if err != nil {
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode == http.StatusOK {
					var tokenResponse struct {
						AccessToken string `json:"access_token"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err == nil && tokenResponse.AccessToken != "" {
						select {
						case callbackCh <- tokenResponse.AccessToken:
						default:
						}
					}
				}
			}()
		}

		// Return page that extracts token from hash fragment and sends to /oauth/save_token
		html := `<!DOCTYPE html>
<html>
<head>
    <title>Curd Authentication</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 50px; text-align: center; background: #1a1a1a; color: white; }
        .success { color: #4CAF50; font-size: 20px; margin-bottom: 20px; }
        .loading { color: #2196F3; font-size: 20px; margin-bottom: 20px; }
        .error { color: #f44336; font-size: 20px; margin-bottom: 20px; }
    </style>
</head>
<body>
    <div id="status" class="loading">Processing authentication...</div>
    <p id="msg">Please wait while Curd connects to your AniList account.</p>
    <script>
        const hash = window.location.hash.substring(1);
        const params = new URLSearchParams(hash);
        const token = params.get('access_token');
        if (token) {
            fetch('/oauth/save_token?token=' + encodeURIComponent(token))
                .then(r => {
                    if (r.ok) {
                        document.getElementById('status').className = 'success';
                        document.getElementById('status').innerText = 'Authentication Successful!';
                        document.getElementById('msg').innerText = 'You can close this tab and return to Curd.';
                    } else {
                        document.getElementById('status').className = 'error';
                        document.getElementById('status').innerText = 'Failed to save token.';
                    }
                })
                .catch(() => {
                    document.getElementById('status').className = 'error';
                    document.getElementById('status').innerText = 'Error communicating with Curd.';
                });
        }
    </script>
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

	// Open browser for authentication using Implicit Grant flow (response_type=token)
	authURL := fmt.Sprintf("%s/authorize?client_id=%s&redirect_uri=%s&response_type=token",
		anilistOAuthURL,
		anilistClientID,
		url.QueryEscape(anilistRedirectURI))

	fmt.Println("Opening browser for AniList authentication...")
	fmt.Printf("If the browser doesn't open automatically, visit: %s\n", authURL)

	if err := OpenURL(authURL); err != nil {
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
	token := &AnilistToken{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   31536000, // AniList tokens are valid for 1 year
		ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
	}

	// Save token to file
	if err := saveToken(tokenPath, token); err != nil {
		return "", fmt.Errorf("failed to save token: %w", err)
	}

	fmt.Println("Authentication successful!")
	return token.AccessToken, nil
}

// loadToken loads the token from the token file
func loadToken(tokenPath string) (*AnilistToken, error) {
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read token file: %w", err)
	}

	var token AnilistToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to parse token file: %w", err)
	}

	return &token, nil
}

// saveToken saves the token to the token file
func saveToken(tokenPath string, token *AnilistToken) error {
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

// isTokenValid checks if the token is still valid
func isTokenValid(token *AnilistToken) bool {
	return token != nil && token.AccessToken != "" && time.Now().Before(token.ExpiresAt)
}

// GetTokenFromFile loads the token from the token file (supports both old text format and new JSON format)
func GetTokenFromFile(tokenPath string) (string, error) {
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", fmt.Errorf("failed to read token from file: %w", err)
	}

	// Try to parse as JSON first (new format)
	var token AnilistToken
	if err := json.Unmarshal(data, &token); err == nil {
		// It's JSON format, check if token is valid
		if isTokenValid(&token) {
			return token.AccessToken, nil
		}
		return "", fmt.Errorf("token has expired")
	}

	// Fall back to plain text format (old format)
	plainToken := strings.TrimSpace(string(data))
	if plainToken == "" {
		return "", fmt.Errorf("empty token file")
	}

	return plainToken, nil
}

// cleanAccessToken extracts the token if a full URL or fragment was pasted
func cleanAccessToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if idx := strings.Index(raw, "access_token="); idx != -1 {
		raw = raw[idx+len("access_token="):]
		if amp := strings.Index(raw, "&"); amp != -1 {
			raw = raw[:amp]
		}
	}
	return strings.Trim(strings.TrimSpace(raw), "\"'")
}

func ChangeToken(config *CurdConfig, user *User) {
	var err error
	tokenPath := filepath.Join(os.ExpandEnv(config.StoragePath), "anilist_token.json")

	// Try browser-based OAuth (force re-authentication since user explicitly wants to change token)
	fmt.Println("Starting browser-based authentication...")
	user.Token, err = authenticateWithBrowser(tokenPath, true)

	if err != nil {
		Log("Browser authentication failed: " + err.Error())
		fmt.Printf("Browser authentication failed: %v\n", err)
		fmt.Println("Falling back to manual token entry...")

		// Simple CLI fallback
		fmt.Println("\nTo get your token manually:")
		fmt.Println("1. Open this URL in your browser:")
		fmt.Println("   https://anilist.co/api/v2/oauth/authorize?client_id=20686&response_type=token&redirect_uri=http://localhost:8000/oauth/callback")
		fmt.Println("2. Click 'Authorize'.")
		fmt.Println("3. Your browser will redirect to a URL like: http://localhost:8000/oauth/callback#access_token=... (it is normal if the page says 'Unable to connect').")
		fmt.Println("4. Copy the access token (or copy the entire redirect URL from your browser's address bar).")
		fmt.Print("\nPaste your access token (or redirect URL) here: ")

		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		user.Token = cleanAccessToken(input)

		if user.Token == "" {
			ExitCurd(fmt.Errorf("no token provided"))
		}

		// Save the manually entered token as JSON format
		token := &AnilistToken{
			AccessToken: user.Token,
			TokenType:   "Bearer",
			ExpiresIn:   31536000, // AniList tokens are valid for 1 year
			ExpiresAt:   time.Now().Add(365 * 24 * time.Hour),
		}

		if err := saveToken(tokenPath, token); err != nil {
			ExitCurd(fmt.Errorf("failed to save token: %w", err))
		}
	}

	if user.Token == "" {
		ExitCurd(fmt.Errorf("no token provided"))
	}

	fmt.Println("Token saved successfully!")
}

// LoadConfigFromFile loads config file from disk into a map (key=value format)
func LoadConfigFromFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	configMap := make(map[string]string)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // Skip empty lines and comments
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			configMap[key] = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return configMap, nil
}

// SaveConfigToFile saves updated config map to file in key=value format
func SaveConfigToFile(path string, configMap map[string]string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for key, value := range configMap {
		line := fmt.Sprintf("%s=%s\n", key, value)
		if _, err := writer.WriteString(line); err != nil {
			return err
		}
	}
	return writer.Flush()
}

// PopulateConfig populates the CurdConfig struct from a map
func PopulateConfig(configMap map[string]string) CurdConfig {
	config := CurdConfig{}
	configValue := reflect.ValueOf(&config).Elem()

	for i := 0; i < configValue.NumField(); i++ {
		field := configValue.Type().Field(i)
		tag := field.Tag.Get("config")

		if value, exists := configMap[tag]; exists {
			fieldValue := configValue.FieldByName(field.Name)

			if fieldValue.CanSet() {
				switch fieldValue.Kind() {
				case reflect.String:
					fieldValue.SetString(value)
				case reflect.Int:
					intVal, _ := strconv.Atoi(value)
					fieldValue.SetInt(int64(intVal))
				case reflect.Bool:
					boolVal, _ := strconv.ParseBool(value)
					fieldValue.SetBool(boolVal)
				}
			}
		}
	}

	// Handle MpvArgs specially
	if mpvArgs, exists := configMap["MpvArgs"]; exists {
		config.MpvArgs = parseStringArray(mpvArgs)
	}

	return config
}

func getOrderedCategories(userCurdConfig *CurdConfig) []SelectionOption {
	isRofi := userCurdConfig != nil && userCurdConfig.RofiSelection

	// Define the default categories and their labels
	defaultOrder := []string{"CURRENT", "ALL", "UNTRACKED", "DOWNLOAD", "UPDATE", "CONTINUE_LAST", "PROVIDER"}
	if isRofi {
		defaultOrder = []string{"CURRENT", "ALL", "UNTRACKED", "UPDATE", "CONTINUE_LAST", "PROVIDER"}
	}
	defaultLabels := map[string]string{
		"CURRENT":        "Currently Watching",
		"ALL":            "Show All",
		"UNTRACKED":      "Untracked Watching",
		"DOWNLOAD":       "Download Episodes",
		"UPDATE":         "Update (Episode, Status, Score)",
		"CONTINUE_LAST":  "Continue Last Session",
		"PROVIDER":       "Change Provider",
	}

	// Create ordered list to store final result
	finalOrder := make([]string, 0)
	seen := make(map[string]bool)

	// If no menu order specified, use default order
	if userCurdConfig == nil || userCurdConfig.MenuOrder == "" {
		finalOrder = defaultOrder
	} else {
		// First, process user-specified order
		menuItems := strings.Split(userCurdConfig.MenuOrder, ",")
		for _, key := range menuItems {
			key = strings.TrimSpace(key)
			if isRofi && key == "DOWNLOAD" {
				continue
			}
			if _, exists := defaultLabels[key]; exists && !seen[key] {
				finalOrder = append(finalOrder, key)
				seen[key] = true
			}
		}

		// Add remaining default items at the end
		for _, key := range defaultOrder {
			if isRofi && key == "DOWNLOAD" {
				continue
			}
			if !seen[key] {
				finalOrder = append(finalOrder, key)
				seen[key] = true
			}
		}
	}

	// Create the final ordered slice of SelectionOptions
	orderedCategories := make([]SelectionOption, 0, len(finalOrder))
	for _, key := range finalOrder {
		orderedCategories = append(orderedCategories, SelectionOption{
			Key:   key,
			Label: defaultLabels[key],
		})
	}

	return orderedCategories
}
