// Package handoff encrypts a login payload to a CLI's per-login public key.
//
// The server generates an ephemeral X25519 keypair inside the approve handler,
// derives an AES-256-GCM key with HKDF-SHA256 from the ECDH shared secret, and
// discards the private half immediately. Nothing persisted by the server can
// decrypt a captured handoff; only the waiting CLI process can.
package handoff

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Scheme versions the wire format independently of `lnt.` device tokens.
const Scheme = "lnh1"

const (
	info      = "lewnotes-cli-handoff-v1"
	keyBytes  = 32
	nonceSize = 12
	maxBlob   = 64 << 10
)

var (
	// ErrInvalidHandoff marks malformed, truncated, or unopenable envelopes.
	ErrInvalidHandoff = errors.New("invalid handoff")
)

// Payload is the plaintext a CLI receives after approval.
type Payload struct {
	Version      int    `json:"v"`
	Server       string `json:"server"`
	Token        string `json:"token"`
	TokenID      string `json:"token_id"`
	DeviceName   string `json:"device_name,omitempty"`
	Username     string `json:"username"`
	InstanceName string `json:"instance_name"`
	IssuedAt     string `json:"issued_at"`
}

type envelope struct {
	Version int    `json:"v"`
	EPK     string `json:"epk"`
	Nonce   string `json:"nonce"`
	CT      string `json:"ct"`
}

// Encrypt builds an `lnh1.` handoff addressed to cliPublicKey. requestID and
// serverOrigin are bound into the AAD, so a blob cannot be replayed against a
// different login request or origin.
func Encrypt(cliPublicKey []byte, requestID, serverOrigin string, payload Payload) (string, error) {
	if len(cliPublicKey) != 32 {
		return "", fmt.Errorf("%w: public key must be 32 bytes", ErrInvalidHandoff)
	}
	if requestID == "" || serverOrigin == "" {
		return "", fmt.Errorf("%w: request id and origin are required", ErrInvalidHandoff)
	}

	pub, err := ecdh.X25519().NewPublicKey(cliPublicKey)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidHandoff, err)
	}
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate handoff key: %w", err)
	}
	shared, err := serverKey.ECDH(pub)
	if err != nil {
		return "", fmt.Errorf("derive handoff secret: %w", err)
	}
	defer zero(shared)

	aead, err := newAEAD(shared, requestID)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate handoff nonce: %w", err)
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode handoff payload: %w", err)
	}
	if len(plaintext) > maxBlob {
		return "", fmt.Errorf("%w: payload too large", ErrInvalidHandoff)
	}

	ciphertext := aead.Seal(nil, nonce, plaintext, aad(requestID, serverOrigin))
	body, err := json.Marshal(envelope{
		Version: 1,
		EPK:     base64.RawURLEncoding.EncodeToString(serverKey.PublicKey().Bytes()),
		Nonce:   base64.RawURLEncoding.EncodeToString(nonce),
		CT:      base64.RawURLEncoding.EncodeToString(ciphertext),
	})
	if err != nil {
		return "", fmt.Errorf("encode handoff envelope: %w", err)
	}
	return Scheme + "." + base64.RawURLEncoding.EncodeToString(body), nil
}

// Decrypt opens a handoff with the CLI's private key. It exists for tests and
// documentation of the wire format; production callers are the CLI's own
// client package, which cannot import server code.
func Decrypt(raw string, privateKey *ecdh.PrivateKey, requestID, serverOrigin string) (Payload, error) {
	if len(raw) < len(Scheme)+2 || raw[:len(Scheme)+1] != Scheme+"." {
		return Payload{}, fmt.Errorf("%w: unexpected scheme", ErrInvalidHandoff)
	}
	body, err := base64.RawURLEncoding.Strict().DecodeString(raw[len(Scheme)+1:])
	if err != nil {
		return Payload{}, fmt.Errorf("%w: malformed envelope", ErrInvalidHandoff)
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Payload{}, fmt.Errorf("%w: malformed envelope", ErrInvalidHandoff)
	}
	if env.Version != 1 {
		return Payload{}, fmt.Errorf("%w: unsupported version", ErrInvalidHandoff)
	}

	epk, err := base64.RawURLEncoding.Strict().DecodeString(env.EPK)
	if err != nil || len(epk) != 32 {
		return Payload{}, fmt.Errorf("%w: malformed ephemeral key", ErrInvalidHandoff)
	}
	peer, err := ecdh.X25519().NewPublicKey(epk)
	if err != nil {
		return Payload{}, fmt.Errorf("%w: %v", ErrInvalidHandoff, err)
	}
	shared, err := privateKey.ECDH(peer)
	if err != nil {
		return Payload{}, fmt.Errorf("%w: %v", ErrInvalidHandoff, err)
	}
	defer zero(shared)

	aead, err := newAEAD(shared, requestID)
	if err != nil {
		return Payload{}, err
	}
	nonce, err := base64.RawURLEncoding.Strict().DecodeString(env.Nonce)
	if err != nil || len(nonce) != nonceSize {
		return Payload{}, fmt.Errorf("%w: malformed nonce", ErrInvalidHandoff)
	}
	ciphertext, err := base64.RawURLEncoding.Strict().DecodeString(env.CT)
	if err != nil || len(ciphertext) > maxBlob {
		return Payload{}, fmt.Errorf("%w: malformed ciphertext", ErrInvalidHandoff)
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, aad(requestID, serverOrigin))
	if err != nil {
		return Payload{}, fmt.Errorf("%w: cannot open envelope", ErrInvalidHandoff)
	}
	var payload Payload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return Payload{}, fmt.Errorf("%w: malformed payload", ErrInvalidHandoff)
	}
	if payload.Version != 1 {
		return Payload{}, fmt.Errorf("%w: unsupported payload version", ErrInvalidHandoff)
	}
	return payload, nil
}

func newAEAD(shared []byte, requestID string) (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, shared, []byte(requestID), info, keyBytes)
	if err != nil {
		return nil, fmt.Errorf("derive handoff key: %w", err)
	}
	defer zero(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create handoff cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create handoff gcm: %w", err)
	}
	return aead, nil
}

func aad(requestID, serverOrigin string) []byte {
	return []byte(requestID + "\x00" + serverOrigin)
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
