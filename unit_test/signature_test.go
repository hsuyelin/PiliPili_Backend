package unit_test

import (
	"PiliPili_Backend/streamer"
	"testing"
	"time"
)

func TestSignatureEncryptDecrypt(t *testing.T) {
	encipher := "fCpNkRyE9eeqPzra"
	mediaId := "test_media_001"
	expireAt := time.Now().UTC().Add(2 * time.Hour).Unix()

	if err := streamer.InitializeSignature(encipher); err != nil {
		t.Fatalf("Failed to initialize signature: %v", err)
	}

	sig, err := streamer.GetSignatureInstance()
	if err != nil {
		t.Fatalf("Failed to get signature instance: %v", err)
	}

	encrypted, err := sig.Encrypt(mediaId, expireAt)
	if err != nil {
		t.Fatalf("Failed to encrypt data: %v", err)
	}
	t.Logf("Encrypted Signature: %s", encrypted)

	decrypted, err := sig.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Failed to decrypt data: %v", err)
	} else {
		t.Logf("Decrypted Signature: %s", decrypted)
	}

	if decrypted["mediaId"] != mediaId {
		t.Errorf("Expected mediaId %s, got %v", mediaId, decrypted["mediaId"])
	}
	if int64(decrypted["expireAt"].(float64)) != expireAt {
		t.Errorf("Expected expireAt %d, got %v", expireAt, decrypted["expireAt"])
	}

	t.Log("Encrypt/Decrypt test passed")
}
