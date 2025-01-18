package streamer

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

var (
	signatureInstance *Signature
	once              sync.Once
)

// Signature provides methods for encrypting and decrypting data using AES-GCM.
type Signature struct {
	key []byte
}

// InitializeSignature initializes the global Signature instance with the provided AES key.
// The key length must be 16, 24, or 32 bytes, corresponding to AES-128, AES-192, or AES-256.
func InitializeSignature(encipher string) error {
	var initError error
	once.Do(func() {
		key := []byte(encipher)
		if len(key) != 16 && len(key) != 24 && len(key) != 32 {
			initError = errors.New("AES key must be 16, 24, or 32 bytes long")
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

// Encrypt encrypts the given mediaId and expireAt values using AES-GCM.
// Returns a base64-encoded ciphertext string.
func (s *Signature) Encrypt(mediaId string, expireAt int64) (string, error) {
	// Prepare data to be encrypted as a JSON object.
	data := map[string]interface{}{
		"mediaId":  mediaId,
		"expireAt": expireAt,
	}
	plaintext, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	// Create a new AES cipher block using the provided key.
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}

	// Create a GCM (Galois/Counter Mode) cipher mode instance.
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// Generate a random nonce.
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// Encrypt the plaintext using AES-GCM.
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	// Encode the result as a base64 string for safe transmission or storage.
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts a base64-encoded ciphertext string and returns the original data as a map.
// Returns an error if the ciphertext is invalid or decryption fails.
func (s *Signature) Decrypt(ciphertext string) (map[string]interface{}, error) {
	// Decode the base64-encoded ciphertext.
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, err
	}

	// Create a new AES cipher block using the provided key.
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}

	// Create a GCM cipher mode instance.
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// Extract the nonce and the encrypted data.
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, errors.New("invalid ciphertext")
	}
	nonce := data[:nonceSize]
	ciphertextBytes := data[nonceSize:]

	// Decrypt the data using AES-GCM.
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return nil, err
	}

	// Parse the decrypted plaintext into a map.
	var result map[string]interface{}
	if err := json.Unmarshal(plaintext, &result); err != nil {
		return nil, err
	}

	return result, nil
}
