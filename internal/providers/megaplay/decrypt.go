package megaplay

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	aesKey = "i?LMTAx0Q6,:}50U"
	aesIV  = "W0;27ToaUpl_P%'c"
)

// decodeBase64Safe decodes standard, url-encoded, unpadded, and raw base64 strings.
func decodeBase64Safe(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
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
	return nil, fmt.Errorf("unable to decode base64")
}

// decryptMegaPlayEnc decodes and AES-256-CBC decrypts MegaPlay's encrypted source blob.
func decryptMegaPlayEnc(enc string) (string, error) {
	enc = strings.TrimSpace(enc)
	if enc == "" {
		return "", fmt.Errorf("empty enc payload")
	}

	ciphertext, err := decodeBase64Safe(enc)
	if err != nil {
		return "", fmt.Errorf("decode base64 enc: %w", err)
	}

	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("invalid ciphertext length %d (must be multiple of %d)", len(ciphertext), aes.BlockSize)
	}

	keyBytes := make([]byte, 32)
	copy(keyBytes, []byte(aesKey))

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", fmt.Errorf("create AES cipher: %w", err)
	}

	mode := cipher.NewCBCDecrypter(block, []byte(aesIV))
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	// PKCS#7 unpadding
	if len(plaintext) == 0 {
		return "", fmt.Errorf("empty decrypted plaintext")
	}
	padLen := int(plaintext[len(plaintext)-1])
	if padLen > 0 && padLen <= aes.BlockSize && padLen <= len(plaintext) {
		valid := true
		for i := len(plaintext) - padLen; i < len(plaintext); i++ {
			if int(plaintext[i]) != padLen {
				valid = false
				break
			}
		}
		if valid {
			plaintext = plaintext[:len(plaintext)-padLen]
		}
	}

	// Try unmarshaling as single JSON object {"file": "..."}
	var singleObj struct {
		File string `json:"file"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(plaintext, &singleObj); err == nil {
		if f := strings.TrimSpace(singleObj.File); f != "" {
			return f, nil
		}
		if u := strings.TrimSpace(singleObj.URL); u != "" {
			return u, nil
		}
	}

	// Try unmarshaling as array of objects [{"file": "..."}]
	var arrObj []struct {
		File string `json:"file"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(plaintext, &arrObj); err == nil && len(arrObj) > 0 {
		if f := strings.TrimSpace(arrObj[0].File); f != "" {
			return f, nil
		}
		if u := strings.TrimSpace(arrObj[0].URL); u != "" {
			return u, nil
		}
	}

	// Try plain URL string (possibly quoted)
	strPlain := strings.Trim(string(plaintext), `"'\r\n `)
	if strings.HasPrefix(strPlain, "http://") || strings.HasPrefix(strPlain, "https://") {
		return strPlain, nil
	}

	return "", fmt.Errorf("could not extract stream file from decrypted payload: %s", string(plaintext))
}
