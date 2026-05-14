package internal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type allanimeResponse struct {
	Data struct {
		M          string `json:"_m"`
		Tobeparsed string `json:"tobeparsed"`
		Episode    struct {
			SourceUrls []struct {
				SourceUrl  string `json:"sourceUrl"`
				SourceName string `json:"sourceName"`
			} `json:"sourceUrls"`
		} `json:"episode"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

type result struct {
	index    int
	links    []string
	provider string
	err      error
}

func decodeTobeparsed(blob string) string {
	key := []byte("Xot36i3lK3:v1")
	hash := sha256.Sum256(key)

	data, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		Log(fmt.Sprint("Error decoding base64:", err))
		return ""
	}

	if len(data) < 29 {
		Log(fmt.Sprintf("Data too short to contain tobeparsed payload: %d < 29", len(data)))
		return ""
	}

	// The payload format is: 1-byte header, 12-byte IV, ciphertext, 16-byte trailer.
	iv := data[1:13]
	ctLen := len(data) - 13 - 16
	if ctLen <= 0 {
		Log(fmt.Sprintf("Ciphertext length is invalid in tobeparsed payload: %d", ctLen))
		return ""
	}
	ct := data[13 : 13+ctLen]

	Log(fmt.Sprintf("Decryption params - Data len: %d, IV len: %d, Ciphertext len: %d", len(data), len(iv), len(ct)))

	ctrIV := make([]byte, 16)
	copy(ctrIV, iv)
	binary.BigEndian.PutUint32(ctrIV[12:], uint32(2))

	block, err := aes.NewCipher(hash[:])
	if err != nil {
		Log(fmt.Sprint("Error creating cipher:", err))
		return ""
	}

	stream := cipher.NewCTR(block, ctrIV)
	plain := make([]byte, len(ct))
	stream.XORKeyStream(plain, ct)

	result := string(plain)
	Log(fmt.Sprintf("Decrypted raw: %s", result))
	
	result = strings.ReplaceAll(result, "{", "\n")
	result = strings.ReplaceAll(result, "}", "\n")

	Log(fmt.Sprintf("After replace: %s", result))

	// Try the newer format first (without "--" prefix)
	re := regexp.MustCompile(`"sourceUrl":"([^"]*)"[^}]*?"sourceName":"([^"]+)"`)
	matches := re.FindAllStringSubmatch(result, -1)

	Log(fmt.Sprintf("Regex matches found: %d", len(matches)))
	for i, match := range matches {
		Log(fmt.Sprintf("Match %d: sourceUrl='%s', sourceName='%s'", i, match[1], match[2]))
	}

	var sb strings.Builder
	for _, match := range matches {
		if len(match) == 3 {
			sourceUrl := match[1]
			// Handle both old format (with "--" prefix) and new format
			if strings.HasPrefix(sourceUrl, "--") {
				sourceUrl = sourceUrl[2:]
			}
			sb.WriteString(match[2])
			sb.WriteString(" :")
			sb.WriteString(sourceUrl)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func decodeProviderID(encoded string) string {
// Split the string into pairs of characters (.. equivalent of 'sed s/../&\n/g')
re := regexp.MustCompile("..")
pairs := re.FindAllString(encoded, -1)

// Mapping for the replacements
replacements := map[string]string{
// Uppercase letters
"79": "A", "7a": "B", "7b": "C", "7c": "D", "7d": "E", "7e": "F", "7f": "G",
"70": "H", "71": "I", "72": "J", "73": "K", "74": "L", "75": "M", "76": "N", "77": "O",
"68": "P", "69": "Q", "6a": "R", "6b": "S", "6c": "T", "6d": "U", "6e": "V", "6f": "W",
"60": "X", "61": "Y", "62": "Z",
// Lowercase letters
"59": "a", "5a": "b", "5b": "c", "5c": "d", "5d": "e", "5e": "f", "5f": "g",
"50": "h", "51": "i", "52": "j", "53": "k", "54": "l", "55": "m", "56": "n", "57": "o",
"48": "p", "49": "q", "4a": "r", "4b": "s", "4c": "t", "4d": "u", "4e": "v", "4f": "w",
"40": "x", "41": "y", "42": "z",
// Numbers
"08": "0", "09": "1", "0a": "2", "0b": "3", "0c": "4", "0d": "5", "0e": "6", "0f": "7",
"00": "8", "01": "9",
// Special characters
"15": "-", "16": ".", "67": "_", "46": "~", "02": ":", "17": "/", "07": "?", "1b": "#",
"63": "[", "65": "]", "78": "@", "19": "!", "1c": "$", "1e": "&", "10": "(", "11": ")",
"12": "*", "13": "+", "14": ",", "03": ";", "05": "=", "1d": "%",
}

// Perform the replacement equivalent to sed 's/^../.../'
for i, pair := range pairs {
if val, exists := replacements[pair]; exists {
pairs[i] = val
}
}

// Join the modified pairs back into a single string
result := strings.Join(pairs, "")

// Replace "/clock" with "/clock.json" equivalent of sed "s/\/clock/\/clock\.json/"
result = strings.ReplaceAll(result, "/clock", "/clock.json")

// Print the final result
return result
}

func extractMp4UploadLinks(pageURL string) map[string]interface{} {
	client := &http.Client{}
	req, err := http.NewRequest("GET", pageURL, nil)
	var videoData map[string]interface{}
	if err != nil {
		Log(fmt.Sprint("Error creating mp4upload request:", err))
		return videoData
	}
	req.Header.Set("Referer", "https://youtu-chan.com")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:150.0) Gecko/20100101 Firefox/150.0")

	resp, err := client.Do(req)
	if err != nil {
		Log(fmt.Sprint("Error fetching mp4upload page:", err))
		return videoData
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		Log(fmt.Sprint("Error reading mp4upload response:", err))
		return videoData
	}

	// Extract src from: player.src("...") or src: "..."
	re := regexp.MustCompile(`(?:player\.src|src)\s*[:(]\s*"(https?://[^"]+\.mp4[^"]*)"`) 
	matches := re.FindStringSubmatch(string(body))
	if len(matches) < 2 {
		Log("mp4upload: no video src found in page")
		return videoData
	}

	vidURL := matches[1]
	Log(fmt.Sprintf("mp4upload extracted URL: %s", vidURL))
	return map[string]interface{}{
		"links": []interface{}{
			map[string]interface{}{
				"link": vidURL,
			},
		},
	}
}

func getProviderName(url string) string {
	// Check for known providers
	providerMap := map[string]string{
		"repackager.wixmp.com":    "wixmp (m3u8)",
		"wixmp.com":               "wixmp (m3u8)",
		"fast4speed.rsvp":         "Youtube (mp4)",
		"youtu-chan.com":          "Youtube (mp4)",
		"sharepoint":              "Sharepoint (mp4)",
		"mp4upload":               "Mp4Upload (mp4)",
		"filemoon":                "Filemoon (HLS)",
		"vidplay":                 "Vidplay (HLS)",
		"gogocdn":                 "GogoAnime (m3u8)",
		"dood":                    "Dood (mp4)",
		"streamtape":              "StreamTape (mp4)",
	}

	for domain, providerName := range providerMap {
		if strings.Contains(url, domain) {
			return providerName
		}
	}

	// Extract domain from URL for unknown providers
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		if parts := strings.Split(url, "://"); len(parts) > 1 {
			if domainParts := strings.Split(parts[1], "/"); len(domainParts) > 0 {
				return domainParts[0]
			}
		}
	}

	// For relative paths like "/clock/..."
	if strings.HasPrefix(url, "/clock") {
		return "Allanime (API)"
	}

	return "Unknown"
}

func extractLinks(provider_id string) (map[string]interface{}, string) {
	// Check if provider_id is already a full URL (external link)
	providerName := getProviderName(provider_id)
	
	if strings.HasPrefix(provider_id, "http://") || strings.HasPrefix(provider_id, "https://") {
		// mp4upload: scrape the page for the actual video src
		if strings.Contains(provider_id, "mp4upload") {
			Log(fmt.Sprintf("mp4upload URL detected, scraping page: %s", provider_id))
			linksData := extractMp4UploadLinks(provider_id)
			return linksData, "Mp4Upload (mp4)"
		}

		// It's an external direct video link, return it as-is
		cleanedURL := provider_id
		// Clean up any double slashes in the URL (except after protocol)
		if strings.Contains(cleanedURL, "://") {
			parts := strings.SplitN(cleanedURL, "://", 2)
			if len(parts) == 2 {
				protocol := parts[0]
				rest := parts[1]
				// Replace any double slashes in the rest of the URL
				rest = strings.ReplaceAll(rest, "//", "/")
				cleanedURL = protocol + "://" + rest
			}
		}

		Log(fmt.Sprintf("Direct external link detected: %s -> %s", provider_id, cleanedURL))
		return map[string]interface{}{
			"links": []interface{}{
				map[string]interface{}{
					"link": cleanedURL,
				},
			},
		}, providerName
	}

	// It's a relative path for allanime API
	allanime_base := "https://allanime.day"
	url := allanime_base + provider_id
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	var videoData map[string]interface{}
	if err != nil {
		Log(fmt.Sprint("Error creating request:", err))
		return videoData, providerName
	}

	// Add the headers
	req.Header.Set("Referer", "https://youtu-chan.com")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:150.0) Gecko/20100101 Firefox/150.0")

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		Log(fmt.Sprint("Error sending request:", err))
		return map[string]interface{}{"links": []interface{}{}}, providerName
	}
	defer resp.Body.Close()

	// Check HTTP status code
	if resp.StatusCode != 200 {
		Log(fmt.Sprintf("HTTP error: %d for URL: %s", resp.StatusCode, url))
		return map[string]interface{}{"links": []interface{}{}}, providerName
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		Log(fmt.Sprint("Error reading response:", err))
		return map[string]interface{}{"links": []interface{}{}}, providerName
	}

	// Log the raw response for debugging
	bodyStr := string(body)
	Log(fmt.Sprintf("Raw API response length: %d", len(bodyStr)))
	if len(bodyStr) < 3000 {
		Log(fmt.Sprintf("Raw API response: %s", bodyStr))
	} else {
		Log(fmt.Sprintf("First 2000 chars: %s", bodyStr[:2000]))
	}

	// The /clock.json endpoint returns JSON with link and resolutionStr fields
	// Format: [{"link":"https://...","resolutionStr":"720"},{"link":"https://...","resolutionStr":"480"}]
	// We need to extract the "link" values
	
	var linksArray []interface{}
	
	// Try parsing as a JSON array first
	var videoArray []interface{}
	if err := json.Unmarshal(body, &videoArray); err == nil && len(videoArray) > 0 {
		// Successfully parsed as array
		Log(fmt.Sprintf("Parsed as JSON array with %d elements", len(videoArray)))
		linksArray = videoArray
	} else {
		// Try parsing as a map with "data" or other root fields
		var videoData map[string]interface{}
		if err := json.Unmarshal(body, &videoData); err == nil {
			Log("Parsed as JSON object")
			
			// Check for "data" field
			if dataArray, ok := videoData["data"].([]interface{}); ok {
				Log(fmt.Sprintf("Found 'data' array with %d elements", len(dataArray)))
				linksArray = dataArray
			} else if dataMap, ok := videoData["data"].(map[string]interface{}); ok {
				Log("Found 'data' object")
				// Check for nested links field
				if links, ok := dataMap["links"].([]interface{}); ok {
					linksArray = links
				} else {
					linksArray = []interface{}{dataMap}
				}
			} else {
				// No standard structure found, try to use the whole map as a single link object
				Log("No standard structure found, treating response as single object")
				linksArray = []interface{}{videoData}
			}
		} else {
			Log(fmt.Sprint("Error parsing JSON:", err))
			// Return empty links on parse failure
			return map[string]interface{}{"links": []interface{}{}}, providerName
		}
	}

	Log(fmt.Sprintf("Total link objects to process: %d", len(linksArray)))
	
	// Return formatted response - the caller expects {"links": [...]}
	return map[string]interface{}{
		"links": linksArray,
	}, providerName
}

// Get anime episode url respective to given config
// If the link is found, it returns a list of links. Otherwise, it returns an error.
//
// Parameters:
// - config: Configuration of the anime search.
// - id: Allanime id of the anime to search for.
// - epNo: Anime episode number to get links for.
//
// Returns:
// - []string: a list of links for specified episode.
// - error: an error if the episode is not found or if there is an issue during the search.
func GetEpisodeURL(config CurdConfig, id string, epNo int) ([]string, error) {
	const (
		episodeQueryHash = "d405d0edd690624b66baba3068e0edc3ac90f1597d898a1ec8db4e5c43c00fec"
	)

	variables := map[string]interface{}{
		"showId":          id,
		"translationType": config.SubOrDub,
		"episodeString":   fmt.Sprintf("%d", epNo),
	}

	extensions := map[string]interface{}{
		"persistedQuery": map[string]interface{}{
			"version":    1,
			"sha256Hash": episodeQueryHash,
		},
	}

	variablesJSON, err := json.Marshal(variables)
	if err != nil {
		Log(fmt.Sprintf("Failed to marshal variables: %v", err))
		return nil, fmt.Errorf("failed to marshal variables: %w", err)
	}

	extensionsJSON, err := json.Marshal(extensions)
	if err != nil {
		Log(fmt.Sprintf("Failed to marshal extensions: %v", err))
		return nil, fmt.Errorf("failed to marshal extensions: %w", err)
	}

	persistedURL := "https://api.allanime.day/api?variables=" + url.QueryEscape(string(variablesJSON)) + "&extensions=" + url.QueryEscape(string(extensionsJSON))

	Log(fmt.Sprintf("Fetching episode URL from: %s", persistedURL))

	client := &http.Client{}
	
	// First attempt: GET request with persistedQuery
	req, err := http.NewRequest("GET", persistedURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:150.0) Gecko/20100101 Firefox/150.0")
	req.Header.Set("Referer", "https://youtu-chan.com")
	req.Header.Set("Origin", "https://youtu-chan.com")

	resp, err := client.Do(req)
	var body []byte
	if err == nil {
		defer resp.Body.Close()
		body, _ = io.ReadAll(resp.Body)
		Log(fmt.Sprintf("API Response Status (GET): %d, Body: %s", resp.StatusCode, string(body)))
	}

	var response allanimeResponse
	if len(body) > 0 {
		json.Unmarshal(body, &response)
	}

	// Fallback attempt: POST request with raw GraphQL query
	if response.Data.Tobeparsed == "" {
		Log("Tobeparsed not found in GET response, falling back to POST request")
		
		postPayload := map[string]interface{}{
			"query":     `query ($showId: String!, $translationType: VaildTranslationTypeEnumType!, $episodeString: String!) { episode( showId: $showId translationType: $translationType episodeString: $episodeString ) { episodeString sourceUrls }}`,
			"variables": variables,
		}
		
		postBody, _ := json.Marshal(postPayload)
		req, err = http.NewRequest("POST", "https://api.allanime.day/api", strings.NewReader(string(postBody)))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:150.0) Gecko/20100101 Firefox/150.0")
			req.Header.Set("Referer", "https://youtu-chan.com")
			req.Header.Set("Origin", "https://youtu-chan.com")
			
			resp, err = client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				body, _ = io.ReadAll(resp.Body)
				Log(fmt.Sprintf("API Response Status (POST): %d, Body: %s", resp.StatusCode, string(body)))
				
				response = allanimeResponse{}
				json.Unmarshal(body, &response)
			} else {
				Log(fmt.Sprintf("Error making POST request: %v", err))
			}
		}
	}

	// Check for GraphQL errors only if we still don't have tobeparsed
	if response.Data.Tobeparsed == "" && len(response.Errors) > 0 {
		Log(fmt.Sprintf("GraphQL error in response: %v", response.Errors[0].Message))
		return nil, fmt.Errorf("GraphQL error: %s", response.Errors[0].Message)
	}

	// Check if the response contains encrypted data (tobeparsed field)
	if response.Data.Tobeparsed != "" {
		Log("Found tobeparsed field, using decoded response")
		decoded := decodeTobeparsed(response.Data.Tobeparsed)
		Log(fmt.Sprintf("Decoded tobeparsed result: %s", decoded))
		lines := strings.Split(strings.TrimSpace(decoded), "\n")
		Log(fmt.Sprintf("Decoded lines count: %d", len(lines)))
		var validURLs []string
		for i, line := range lines {
			Log(fmt.Sprintf("Line %d: '%s'", i, line))
			if line == "" {
				continue
			}
			if parts := strings.Split(line, " :"); len(parts) == 2 {
				url := parts[1]
				// Convert /clock to /clock.json (as per ani-cli)
				url = strings.ReplaceAll(url, "/clock", "/clock.json")
				Log(fmt.Sprintf("Extracted URL: %s", url))
				validURLs = append(validURLs, url)
			}
		}

		Log(fmt.Sprintf("Total valid URLs extracted: %d", len(validURLs)))
		if len(validURLs) == 0 {
			Log("No valid source URLs found in decoded tobeparsed, falling back to sourceUrls")
			// Fall through to process sourceUrls below
		} else {
			Log(fmt.Sprintf("Calling getLinksFromURLs with %d URLs", len(validURLs)))
			return getLinksFromURLs(validURLs)
		}
	}

// For sourceUrls responses, collect all URLs and let extractLinks handle them
validURLs := make([]string, 0)

for _, url := range response.Data.Episode.SourceUrls {
if len(url.SourceUrl) == 0 {
	continue
}
validURLs = append(validURLs, url.SourceUrl)
}

if len(validURLs) == 0 {
return nil, fmt.Errorf("no valid source URLs found in response")
}

return getLinksFromURLs(validURLs)
}

func getLinksFromURLs(validURLs []string) ([]string, error) {
	results := make(chan result, len(validURLs))
	orderedResults := make([][]string, len(validURLs))
	highPriorityLink := make(chan []string, 1)

	// Launch all goroutines immediately — no rate limiter
	for i, sourceUrl := range validURLs {
		go func(idx int, url string) {
			var decodedProviderID string
			if len(url) > 2 && unicode.IsDigit(rune(url[0])) {
				rawPath := url[2:]
				if strings.HasPrefix(rawPath, "/") || strings.HasPrefix(rawPath, "http") {
					decodedProviderID = rawPath
				} else {
					decodedProviderID = decodeProviderID(rawPath)
				}
			} else {
				decodedProviderID = url
			}

			extractedLinks, providerName := extractLinks(decodedProviderID)

			if extractedLinks == nil {
				results <- result{index: idx, err: fmt.Errorf("failed to extract links for provider %s", decodedProviderID)}
				return
			}

			linksInterface, ok := extractedLinks["links"].([]interface{})
			if !ok {
				results <- result{index: idx, err: fmt.Errorf("links field is not []interface{} for provider %s", decodedProviderID)}
				return
			}

			var links []string
			for _, linkInterface := range linksInterface {
				linkMap, ok := linkInterface.(map[string]interface{})
				if !ok {
					continue
				}
				link, ok := linkMap["link"].(string)
				if !ok {
					continue
				}
				links = append(links, link)
			}

			// Send high-priority link immediately if found
			for _, link := range links {
				for _, domain := range LinkPriorities[:3] {
					if strings.Contains(link, domain) {
						select {
						case highPriorityLink <- []string{link}:
						default:
						}
						break
					}
				}
			}

			results <- result{index: idx, links: links, provider: providerName}
		}(i, sourceUrl)
	}

	// Wait up to 500ms for a high-priority link, then fall through to collect all
	timeout := time.After(5 * time.Second)
	select {
	case links := <-highPriorityLink:
		// Drain results in background so goroutines don't leak
		go func() {
			for i := 0; i < len(validURLs); i++ {
				select {
				case <-results:
				case <-time.After(8 * time.Second):
					return
				}
			}
		}()
		return links, nil
	case <-time.After(500 * time.Millisecond):
		// No high-priority link yet — fall through to collect all results
	}

	// Collect all results; timeout exits the loop early with whatever we have
	var collectedErrors []error
	processed := 0
collect:
	for processed < len(validURLs) {
		select {
		case res := <-results:
			processed++
			if res.err != nil {
				Log(fmt.Sprintf("Error processing URL %d: %v", res.index+1, res.err))
				collectedErrors = append(collectedErrors, res.err)
			} else {
				orderedResults[res.index] = res.links
			}
		case <-timeout:
			Log(fmt.Sprintf("Timeout reached with %d/%d results", processed, len(validURLs)))
			break collect
		}
	}

	allLinks := flattenResults(orderedResults)
	if len(allLinks) == 0 {
		return nil, fmt.Errorf("no valid links found from %d URLs: %v", len(validURLs), collectedErrors)
	}
	return allLinks, nil
}


// converts the ordered slice of link slices into a single slice
func flattenResults(results [][]string) []string {
var totalLen int
for _, r := range results {
totalLen += len(r)
}

allLinks := make([]string, 0, totalLen)
for _, links := range results {
allLinks = append(allLinks, links...)
}
return allLinks
}
