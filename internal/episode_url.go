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
index int
links []string
err   error
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

	re := regexp.MustCompile(`"sourceUrl":"--([^"]+)".*"sourceName":"([^"]+)"`)
	matches := re.FindAllStringSubmatch(result, -1)

	Log(fmt.Sprintf("Regex matches found: %d", len(matches)))
	for i, match := range matches {
		Log(fmt.Sprintf("Match %d: sourceUrl='%s', sourceName='%s'", i, match[1], match[2]))
	}

	var sb strings.Builder
	for _, match := range matches {
		if len(match) == 3 {
			sb.WriteString(match[2])
			sb.WriteString(" :")
			sb.WriteString(match[1])
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

func extractLinks(provider_id string) map[string]interface{} {
// Check if provider_id is already a full URL (external link)
if strings.HasPrefix(provider_id, "http://") || strings.HasPrefix(provider_id, "https://") {
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
}
}

// It's a relative path for allanime API
allanime_base := "https://allanime.day"
url := allanime_base + provider_id
client := &http.Client{}
req, err := http.NewRequest("GET", url, nil)
var videoData map[string]interface{}
if err != nil {
Log(fmt.Sprint("Error creating request:", err))
return videoData
}

// Add the headers
req.Header.Set("Referer", "https://allanime.to")
req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/121.0")

// Send the request
resp, err := client.Do(req)
if err != nil {
Log(fmt.Sprint("Error sending request:", err))
return videoData
}
defer resp.Body.Close()

// Read the response body
body, err := io.ReadAll(resp.Body)
if err != nil {
Log(fmt.Sprint("Error reading response:", err))
return videoData
}

// Parse the JSON response
err = json.Unmarshal(body, &videoData)
if err != nil {
Log(fmt.Sprint("Error parsing JSON:", err))
return videoData
}

// Process the data as needed
return videoData
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
	req, err := http.NewRequest("GET", persistedURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/121.0")
	req.Header.Set("Referer", "https://allanime.to")

	resp, err := client.Do(req)
	if err != nil {
		Log(fmt.Sprintf("Error making request: %v", err))
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		Log(fmt.Sprintf("Error reading response body: %v", err))
		return nil, err
	}

	Log(fmt.Sprintf("API Response Status: %d, Body: %s", resp.StatusCode, string(body)))

	var response allanimeResponse
	err = json.Unmarshal(body, &response)
	if err != nil {
		Log(fmt.Sprintf("Error parsing JSON: %v", err))
		Log(fmt.Sprintf("Response body: %s", string(body)))
		return nil, err
	}

	// Check for GraphQL errors
	if len(response.Errors) > 0 {
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
				url := "--" + parts[1]
				Log(fmt.Sprintf("Extracted URL: %s", url))
				validURLs = append(validURLs, url)
			}
		}

		Log(fmt.Sprintf("Total valid URLs extracted: %d", len(validURLs)))
		if len(validURLs) == 0 {
			Log("No valid source URLs found in decoded tobeparsed")
			return nil, fmt.Errorf("no valid source URLs found in decoded tobeparsed")
		}

		Log(fmt.Sprintf("Calling getLinksFromURLs with %d URLs", len(validURLs)))
		return getLinksFromURLs(validURLs)
	}

// Pre-count valid URLs and create slice to preserve order
validURLs := make([]string, 0)
highestPriority := -1
var highestPriorityURL string

	for _, url := range response.Data.Episode.SourceUrls {
		if len(url.SourceUrl) == 0 {
			continue
		}

		if len(url.SourceUrl) > 2 && unicode.IsDigit(rune(url.SourceUrl[2])) {
			decodedURL := decodeProviderID(url.SourceUrl[2:])
			if strings.Contains(decodedURL, LinkPriorities[0]) {
				priority := int(url.SourceUrl[2] - '0')
				if priority > highestPriority {
					highestPriority = priority
					highestPriorityURL = url.SourceUrl
				}
			} else {
				validURLs = append(validURLs, url.SourceUrl)
			}
		} else {
			// Fallback: add URLs that don't match the expected encoded format
			validURLs = append(validURLs, url.SourceUrl)
		}
	}

// If we found a highest priority URL, use only that
if highestPriorityURL != "" {
validURLs = []string{highestPriorityURL}
}

if len(validURLs) == 0 {
return nil, fmt.Errorf("no valid source URLs found in response")
}

return getLinksFromURLs(validURLs)
}

func getLinksFromURLs(validURLs []string) ([]string, error) {
// Create channels for results and a slice to store ordered results
results := make(chan result, len(validURLs))
orderedResults := make([][]string, len(validURLs))

// Add a channel for high priority links
highPriorityLink := make(chan []string, 1)

// Create rate limiter
rateLimiter := time.NewTicker(50 * time.Millisecond)
defer rateLimiter.Stop()

// Launch goroutines
remainingURLs := len(validURLs)
for i, sourceUrl := range validURLs {
go func(idx int, url string) {
<-rateLimiter.C // Rate limit the requests

decodedProviderID := decodeProviderID(url[2:])
Log(fmt.Sprintf("Processing URL %d/%d with provider ID: %s", idx+1, len(validURLs), decodedProviderID))

extractedLinks := extractLinks(decodedProviderID)

if extractedLinks == nil {
results <- result{
index: idx,
err:   fmt.Errorf("failed to extract links for provider %s", decodedProviderID),
}
return
}

linksInterface, ok := extractedLinks["links"].([]interface{})
if !ok {
results <- result{
index: idx,
err:   fmt.Errorf("links field is not []interface{} for provider %s", decodedProviderID),
}
return
}

var links []string
for _, linkInterface := range linksInterface {
linkMap, ok := linkInterface.(map[string]interface{})
if !ok {
Log(fmt.Sprintf("Warning: invalid link format for provider %s", decodedProviderID))
continue
}

link, ok := linkMap["link"].(string)
if !ok {
Log(fmt.Sprintf("Warning: link field is not string for provider %s", decodedProviderID))
continue
}

links = append(links, link)
}

// Check if any of the extracted links are high priority
for _, link := range links {
for _, domain := range LinkPriorities[:3] { // Check only top 3 priority domains
if strings.Contains(link, domain) {
// Found high priority link, send it immediately
select {
case highPriorityLink <- []string{link}:
default:
// Channel already has a high priority link
}
break
}
}
}

results <- result{
index: idx,
links: links,
}
}(i, sourceUrl)
}

// Collect results with timeout
timeout := time.After(10 * time.Second)
var collectedErrors []error
successCount := 0

// First, try to get a high priority link
select {
case links := <-highPriorityLink:
// Continue extracting other links in background
go collectRemainingResults(results, orderedResults, &successCount, &collectedErrors, remainingURLs)
return links, nil
case <-time.After(2 * time.Second): // Wait only briefly for high priority link
// No high priority link found quickly, proceed with normal collection
}

// Continue with existing result collection logic
// Collect results maintaining order
for successCount < len(validURLs) {
select {
case res := <-results:
if res.err != nil {
Log(fmt.Sprintf("Error processing URL %d: %v", res.index+1, res.err))
collectedErrors = append(collectedErrors, fmt.Errorf("URL %d: %w", res.index+1, res.err))
} else {
orderedResults[res.index] = res.links
successCount++
Log(fmt.Sprintf("Successfully processed URL %d/%d", res.index+1, len(validURLs)))
}
case <-timeout:
if successCount > 0 {
Log(fmt.Sprintf("Timeout reached with %d/%d successful results", successCount, len(validURLs)))
// Flatten available results
return flattenResults(orderedResults), nil
}
return nil, fmt.Errorf("timeout waiting for results after %d successful responses", successCount)
}
}

// If we have any errors but also some successes, log errors but continue
if len(collectedErrors) > 0 {
Log(fmt.Sprintf("Completed with %d errors: %v", len(collectedErrors), collectedErrors))
}

// Flatten and return results
allLinks := flattenResults(orderedResults)
if len(allLinks) == 0 {
return nil, fmt.Errorf("no valid links found from %d URLs: %v", len(validURLs), collectedErrors)
}

return allLinks, nil
}

// Helper function to collect remaining results in background
func collectRemainingResults(results chan result, orderedResults [][]string, successCount *int, collectedErrors *[]error, remainingURLs int) {
for *successCount < remainingURLs {
select {
case res := <-results:
if res.err != nil {
Log(fmt.Sprintf("Error processing URL %d: %v", res.index+1, res.err))
*collectedErrors = append(*collectedErrors, fmt.Errorf("URL %d: %w", res.index+1, res.err))
} else {
orderedResults[res.index] = res.links
*successCount++
Log(fmt.Sprintf("Successfully processed URL %d/%d", res.index+1, remainingURLs))
}
case <-time.After(10 * time.Second):
return
}
}
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
