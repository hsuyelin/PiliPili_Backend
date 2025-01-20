// Package streamer Package stream provides AES-128 CBC encryption and decryption with consistent results.
package streamer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
)

var (
	signatureInstance *Signature
	once              sync.Once
)

// Signature provides methods for signing and verifying data using HMAC-SHA256.
type Signature struct {
	key []byte
}

// InitializeSignature initializes the global Signature instance with the provided AES key.
// The key length must be 16 bytes for AES-128.
func InitializeSignature(encipher string) error {
	var initError error
	once.Do(func() {
		key := []byte(encipher)
		if len(key) != 16 {
			initError = errors.New("AES key must be 16 bytes long for AES-128")
			return
		}
		signatureInstance = &Signature{key: key}
	})
	return initError
}

// GetSignatureInstance returns the global Signature instance.
func GetSignatureInstance() (*Signature, error) {
	if signatureInstance == nil {
		return nil, errors.New("signature instance is not initialized")
	}
	return signatureInstance, nil
}

// Encrypt deterministically generates a signature for the given itemId, mediaId and expireAt using HMAC-SHA256.
// Returns a base64-encoded ciphertext string.
func (s *Signature) Encrypt(itemId, mediaId string, expireAt int64) (string, error) {
	// Create a map with the input data
	data := map[string]interface{}{
		"itemId":   itemId,
		"mediaId":  mediaId,
		"expireAt": expireAt,
	}

	// Serialize the data to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	// Generate HMAC-SHA256 signature
	h := hmac.New(sha256.New, s.key)
	h.Write(jsonData)
	signature := h.Sum(nil)

	// Combine the JSON data and signature
	payload := map[string]string{
		"data":      base64.StdEncoding.EncodeToString(jsonData),
		"signature": base64.StdEncoding.EncodeToString(signature),
	}

	// Serialize the payload to JSON
	payloadJson, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	// Return the base64-encoded payload
	return base64.StdEncoding.EncodeToString(payloadJson), nil
}

// Decrypt verifies the provided base64-encoded signature using HMAC-SHA256.
// Returns the original data as a map if the signature is valid.
func (s *Signature) Decrypt(ciphertext string) (map[string]interface{}, error) {
	// Decode the base64-encoded payload
	payloadJson, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, err
	}

	// Parse the JSON payload
	var payload map[string]string
	if err := json.Unmarshal(payloadJson, &payload); err != nil {
		return nil, err
	}

	// Decode the data and signature
	jsonData, err := base64.StdEncoding.DecodeString(payload["data"])
	if err != nil {
		return nil, err
	}
	signature, err := base64.StdEncoding.DecodeString(payload["signature"])
	if err != nil {
		return nil, err
	}

	// Verify the HMAC-SHA256 signature
	h := hmac.New(sha256.New, s.key)
	h.Write(jsonData)
	computedSignature := h.Sum(nil)
	if !hmac.Equal(signature, computedSignature) {
		return nil, errors.New("signature verification failed")
	}

	// Parse the original data
	var data map[string]interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, err
	}

	return data, nil
}
