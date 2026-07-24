package allanime

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wraient/curd/internal/curdhost"
)

const (
	allanimeAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:109.0) Gecko/20100101 Firefox/121.0"
	allanimeRefr      = "https://mkissa.to"
	allanimeCDN       = "https://cdn.mkissa.net/all/mk/_app/immutable"
	allanimeAPI       = "https://api.mkissa.net/api"
	allanimeQueryHash = "f4662f4b7510b26795dd53ef824a0bf1740fbbc5d1273fab18222ac831bca8d0"
)

type allanimeKeys struct {
	Epoch int
	Key   string // 64-char hex string (32 bytes)
}

var (
	keysLock       sync.Mutex
	cachedKeys     *allanimeKeys
	cachedKeysTime time.Time
)

func getAllanimeKeys() (*allanimeKeys, error) {
	keysLock.Lock()
	defer keysLock.Unlock()

	if cachedKeys != nil && time.Since(cachedKeysTime) < 10*time.Minute {
		return cachedKeys, nil
	}

	keys, err := fetchAllanimeKeys()
	if err != nil {
		if cachedKeys != nil {
			return cachedKeys, nil
		}
		return nil, err
	}

	cachedKeys = keys
	cachedKeysTime = time.Now()
	return cachedKeys, nil
}

func invalidateAllanimeKeys() {
	keysLock.Lock()
	defer keysLock.Unlock()
	cachedKeys = nil
}

func httpClient() *http.Client {
	if curdhost.HTTPClient != nil {
		if c := curdhost.HTTPClient(); c != nil {
			return c
		}
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func fetchAllanimeKeys() (*allanimeKeys, error) {
	client := httpClient()

	req, err := http.NewRequest("GET", allanimeRefr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for mkissa home: %w", err)
	}
	req.Header.Set("User-Agent", allanimeAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch mkissa home: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read mkissa home body: %w", err)
	}

	epochRe := regexp.MustCompile(`"epoch":(\d+)`)
	epochMatch := epochRe.FindSubmatch(body)
	if len(epochMatch) < 2 {
		return nil, fmt.Errorf("epoch not found in mkissa home page")
	}
	epoch, err := strconv.Atoi(string(epochMatch[1]))
	if err != nil {
		return nil, fmt.Errorf("invalid epoch value: %w", err)
	}

	partBRe := regexp.MustCompile(`"partB":"([^"]+)"`)
	partBMatch := partBRe.FindSubmatch(body)
	if len(partBMatch) < 2 {
		return nil, fmt.Errorf("partB not found in mkissa home page")
	}

	partBBytes, err := base64.StdEncoding.DecodeString(string(partBMatch[1]))
	if err != nil {
		return nil, fmt.Errorf("failed to decode partB base64: %w", err)
	}

	appURLRe := regexp.MustCompile(regexp.QuoteMeta(allanimeCDN) + `/entry/app\.[A-Za-z0-9_.-]+\.js`)
	appURLMatch := appURLRe.Find(body)
	if appURLMatch == nil {
		return nil, fmt.Errorf("app js URL not found in mkissa home page")
	}

	reqApp, err := http.NewRequest("GET", string(appURLMatch), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for app js: %w", err)
	}
	reqApp.Header.Set("User-Agent", allanimeAgent)

	respApp, err := client.Do(reqApp)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch app js: %w", err)
	}
	defer respApp.Body.Close()

	appBody, err := io.ReadAll(respApp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read app js body: %w", err)
	}

	chunkRe := regexp.MustCompile(`"([^"]*chunks/[A-Za-z0-9_.-]+\.js)"`)
	chunkMatches := chunkRe.FindAllSubmatch(appBody, 5)
	if len(chunkMatches) == 0 {
		return nil, fmt.Errorf("no chunks found in app js")
	}

	var maskHex string
	var wg sync.WaitGroup
	var maskLock sync.Mutex

	for _, match := range chunkMatches {
		chunkRel := string(match[1])
		chunkRel = strings.TrimPrefix(chunkRel, "../")
		chunkRel = strings.TrimPrefix(chunkRel, "./")
		chunkURL := allanimeCDN + "/" + chunkRel

		wg.Add(1)
		go func(urlStr string) {
			defer wg.Done()
			reqC, err := http.NewRequest("GET", urlStr, nil)
			if err != nil {
				return
			}
			reqC.Header.Set("User-Agent", allanimeAgent)
			respC, err := client.Do(reqC)
			if err != nil {
				return
			}
			defer respC.Body.Close()
			cBody, err := io.ReadAll(respC.Body)
			if err != nil {
				return
			}

			hexRe := regexp.MustCompile(`[0-9a-f]{64}`)
			hexMatch := hexRe.Find(cBody)
			if hexMatch != nil {
				maskLock.Lock()
				if maskHex == "" {
					maskHex = string(hexMatch)
				}
				maskLock.Unlock()
			}
		}(chunkURL)
	}
	wg.Wait()

	if maskHex == "" {
		return nil, fmt.Errorf("aa_mask_hex not found in chunks")
	}

	maskBytes, err := hex.DecodeString(maskHex)
	if err != nil {
		return nil, fmt.Errorf("invalid mask hex: %w", err)
	}

	if len(maskBytes) != len(partBBytes) {
		return nil, fmt.Errorf("length mismatch between mask (%d) and partB (%d)", len(maskBytes), len(partBBytes))
	}

	keyBytes := make([]byte, len(maskBytes))
	for i := 0; i < len(maskBytes); i++ {
		keyBytes[i] = maskBytes[i] ^ partBBytes[i]
	}

	return &allanimeKeys{
		Epoch: epoch,
		Key:   hex.EncodeToString(keyBytes),
	}, nil
}

func getAAReq(epoch int, keyHex, queryHash string) (string, error) {
	ts := (time.Now().Unix() / 300) * 300 * 1000

	payloadIVStr := fmt.Sprintf("%d:%s:%d", epoch, queryHash, ts)
	hash := sha256.Sum256([]byte(payloadIVStr))
	iv := hash[:12]

	payload := fmt.Sprintf(`{"v":1,"ts":%d,"epoch":%d,"qh":"%s"}`, ts, epoch, queryHash)

	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", fmt.Errorf("invalid key hex: %w", err)
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM mode: %w", err)
	}

	ciphertext := gcm.Seal(nil, iv, []byte(payload), nil)

	result := make([]byte, 1+len(iv)+len(ciphertext))
	result[0] = 0x01
	copy(result[1:], iv)
	copy(result[1+len(iv):], ciphertext)

	return base64.StdEncoding.EncodeToString(result), nil
}

func decryptTobeparsed(blob string, keyHex string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return nil, fmt.Errorf("error decoding base64: %w", err)
	}

	if len(data) < 13+16 {
		return nil, fmt.Errorf("data too short to contain tobeparsed payload")
	}

	iv := data[1:13]
	ciphertext := data[13:]

	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid key hex: %w", err)
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("error creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("error creating GCM cipher mode: %w", err)
	}

	plain, err := gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("error decrypting tobeparsed payload: %w", err)
	}

	return plain, nil
}
