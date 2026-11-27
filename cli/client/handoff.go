package client

import (
	"context"
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
	"net/http"
	"net/url"
	"strings"
)

// Handoff scheme (shared with the server): lnh1.<base64url(envelope)>.
//
// The CLI generates an ephemeral X25519 keypair per login. The server encrypts
// the payload (server origin + device token) to that public key, so a captured
// handoff is useless to anyone who is not this process.
const HandoffScheme = "lnh1"

const handoffInfo = "lewnotes-cli-handoff-v1"

var (
	// ErrInvalidHandoff marks malformed or unopenable login handoffs.
	ErrInvalidHandoff = errors.New("invalid login handoff")
)

// HandoffPayload is the plaintext carried inside an approved login handoff.
type HandoffPayload struct {
	Version      int    `json:"v"`
	Server       string `json:"server"`
	Token        string `json:"token"`
	TokenID      string `json:"token_id"`
	DeviceName   string `json:"device_name,omitempty"`
	Username     string `json:"username"`
	InstanceName string `json:"instance_name"`
	IssuedAt     string `json:"issued_at"`
}

// LoginKey is a per-login ephemeral keypair. The private half never leaves
// memory and is discarded when the login finishes.
type LoginKey struct {
	private *ecdh.PrivateKey
	Public  string
}

// NewLoginKey generates a fresh X25519 keypair and its base64url public key.
func NewLoginKey() (*LoginKey, error) {
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate login key: %w", err)
	}
	return &LoginKey{
		private: private,
		Public:  base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()),
	}, nil
}

// NewLoginState returns a random state nonce for the poll header.
func NewLoginState() (string, error) {
	buf := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("generate login state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Decrypt opens a handoff and verifies it was bound to this request and origin.
func (k *LoginKey) Decrypt(handoff, requestID, serverOrigin string) (HandoffPayload, error) {
	fail := func(format string, args ...any) (HandoffPayload, error) {
		return HandoffPayload{}, fmt.Errorf("%w: %s", ErrInvalidHandoff, fmt.Sprintf(format, args...))
	}
	if !strings.HasPrefix(handoff, HandoffScheme+".") {
		return fail("unexpected scheme")
	}
	body, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(handoff, HandoffScheme+"."))
	if err != nil {
		return fail("malformed envelope")
	}
	var envelope struct {
		Version int    `json:"v"`
		EPK     string `json:"epk"`
		Nonce   string `json:"nonce"`
		CT      string `json:"ct"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fail("malformed envelope")
	}
	if envelope.Version != 1 {
		return fail("unsupported version")
	}

	epk, err := base64.RawURLEncoding.Strict().DecodeString(envelope.EPK)
	if err != nil || len(epk) != 32 {
		return fail("malformed ephemeral key")
	}
	peer, err := ecdh.X25519().NewPublicKey(epk)
	if err != nil {
		return fail("malformed ephemeral key")
	}
	shared, err := k.private.ECDH(peer)
	if err != nil {
		return fail("key agreement failed")
	}
	defer zeroBytes(shared)

	key, err := hkdf.Key(sha256.New, shared, []byte(requestID), handoffInfo, 32)
	if err != nil {
		return fail("key derivation failed")
	}
	defer zeroBytes(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return fail("cipher setup failed")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return fail("cipher setup failed")
	}
	nonce, err := base64.RawURLEncoding.Strict().DecodeString(envelope.Nonce)
	if err != nil || len(nonce) != aead.NonceSize() {
		return fail("malformed nonce")
	}
	ciphertext, err := base64.RawURLEncoding.Strict().DecodeString(envelope.CT)
	if err != nil {
		return fail("malformed ciphertext")
	}

	aad := []byte(requestID + "\x00" + serverOrigin)
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return fail("cannot open envelope")
	}
	var payload HandoffPayload
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return fail("malformed payload")
	}
	if payload.Version != 1 {
		return fail("unsupported payload version")
	}
	return payload, nil
}

// CreateLoginInput is the untrusted body of POST /api/v1/cli/login.
type CreateLoginInput struct {
	PublicKey   string `json:"public_key"`
	State       string `json:"state"`
	DeviceName  string `json:"device_name"`
	Client      string `json:"client"`
	RedirectURI string `json:"redirect_uri,omitempty"`
}

// LoginCreated mirrors the response to POST /api/v1/cli/login.
type LoginCreated struct {
	ID          string `json:"id"`
	ApproveURL  string `json:"approve_url"`
	DeviceURL   string `json:"device_url"`
	UserCode    string `json:"user_code"`
	ExpiresAt   string `json:"expires_at"`
	PollAfterMS int    `json:"poll_after_ms"`
}

// LoginPoll mirrors GET /api/v1/cli/login/{id}.
type LoginPoll struct {
	Status    string `json:"status"`
	Handoff   string `json:"handoff,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// LoginPreview mirrors the authorize page's view of a request.
type LoginPreview struct {
	ID         string `json:"id"`
	DeviceName string `json:"device_name"`
	Client     string `json:"client"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at"`
	Server     string `json:"server"`
	Status     string `json:"status"`
	ApproveURL string `json:"approve_url,omitempty"`
}

// CreateLogin registers a login request with the server.
func (c *Client) CreateLogin(ctx context.Context, in CreateLoginInput) (LoginCreated, error) {
	var out LoginCreated
	err := c.do(ctx, http.MethodPost, "/api/v1/cli/login", in, &out, nil)
	return out, err
}

// GetLogin polls a login request. The state is the per-request secret.
func (c *Client) GetLogin(ctx context.Context, id, state string) (LoginPoll, error) {
	headers := http.Header{"X-Lewnotes-Handoff": {state}}
	var out LoginPoll
	err := c.do(ctx, http.MethodGet, "/api/v1/cli/login/"+url.PathEscape(id), nil, &out, headers)
	return out, err
}

// CancelLogin best-effort cancels a request (used on Ctrl-C).
func (c *Client) CancelLogin(ctx context.Context, id, state string) error {
	headers := http.Header{"X-Lewnotes-Handoff": {state}}
	return c.do(ctx, http.MethodDelete, "/api/v1/cli/login/"+url.PathEscape(id), nil, nil, headers)
}

// PreviewLogin is used by tests and the authorize page's data loader.
func (c *Client) PreviewLogin(ctx context.Context, id string) (LoginPreview, error) {
	var out LoginPreview
	err := c.do(ctx, http.MethodGet, "/api/v1/cli/login/"+url.PathEscape(id)+"/preview", nil, &out, nil)
	return out, err
}

// ApproveLogin is used by tests; the CLI never approves its own request.
func (c *Client) ApproveLogin(ctx context.Context, id, deviceName string) (map[string]string, error) {
	body := map[string]string{}
	if deviceName != "" {
		body["device_name"] = deviceName
	}
	var out map[string]string
	err := c.do(ctx, http.MethodPost, "/api/v1/cli/login/"+url.PathEscape(id)+"/approve", body, &out, nil)
	return out, err
}

// DenyLogin is used by tests and tooling.
func (c *Client) DenyLogin(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/api/v1/cli/login/"+url.PathEscape(id)+"/deny", map[string]string{}, nil, nil)
}

// ResolveLogin exchanges a headless user code for the request preview.
func (c *Client) ResolveLogin(ctx context.Context, userCode string) (LoginPreview, error) {
	var out LoginPreview
	err := c.do(ctx, http.MethodPost, "/api/v1/cli/login/resolve", map[string]string{"user_code": userCode}, &out, nil)
	return out, err
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
