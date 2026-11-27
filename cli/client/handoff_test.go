package client

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestLoginKeyDecryptsServerHandoff(t *testing.T) {
	// Build the envelope the same way the server does, using the client's
	// public key, then confirm the client can open it.
	key, err := NewLoginKey()
	if err != nil {
		t.Fatal(err)
	}
	pubBytes, err := decodePublicKey(key.Public)
	if err != nil {
		t.Fatal(err)
	}
	serverPrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := serverPrivate.ECDH(pubBytes)
	if err != nil {
		t.Fatal(err)
	}
	blob := sealForTest(t, serverPrivate, shared, "req-1", "https://notes.example.com", HandoffPayload{
		Version:  1,
		Server:   "https://notes.example.com",
		Token:    "lnt.aHR0cHM6Ly9ub3Rlcy5leGFtcGxlLmNvbQ.01JC8000000000000000000000.c2VjcmV0",
		TokenID:  "01JC8000000000000000000000",
		Username: "owner",
	})

	payload, err := key.Decrypt(blob, "req-1", "https://notes.example.com")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if payload.Server != "https://notes.example.com" || payload.Username != "owner" {
		t.Fatalf("payload = %+v", payload)
	}

	if _, err := key.Decrypt(blob, "req-2", "https://notes.example.com"); err == nil {
		t.Fatal("expected a different request id to fail")
	}
	if _, err := key.Decrypt(blob, "req-1", "https://other.example.com"); err == nil {
		t.Fatal("expected a different origin to fail")
	}
}

func decodePublicKey(encoded string) (*ecdh.PublicKey, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	return ecdh.X25519().NewPublicKey(raw)
}

func sealForTest(t *testing.T, serverPrivate *ecdh.PrivateKey, shared []byte, requestID, origin string, payload HandoffPayload) string {
	t.Helper()
	key, err := hkdf.Key(sha256.New, shared, []byte(requestID), handoffInfo, 32)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatal(err)
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, []byte(requestID+"\x00"+origin))

	body, err := json.Marshal(map[string]any{
		"v":     1,
		"epk":   base64.RawURLEncoding.EncodeToString(serverPrivate.PublicKey().Bytes()),
		"nonce": base64.RawURLEncoding.EncodeToString(nonce),
		"ct":    base64.RawURLEncoding.EncodeToString(ciphertext),
	})
	if err != nil {
		t.Fatal(err)
	}
	return HandoffScheme + "." + base64.RawURLEncoding.EncodeToString(body)
}

func TestDecryptRejectsMalformedHandoff(t *testing.T) {
	key, err := NewLoginKey()
	if err != nil {
		t.Fatal(err)
	}
	for _, blob := range []string{"", "lnh1", "lnt.abc", "lnh1.!!!", "lnh1." + strings.Repeat("A", 40)} {
		if _, err := key.Decrypt(blob, "req", "https://notes.example.com"); err == nil {
			t.Fatalf("expected %q to fail", blob)
		}
	}
}
