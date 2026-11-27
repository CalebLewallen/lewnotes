package handoff

import (
	"crypto/ecdh"
	"crypto/rand"
	"strings"
	"testing"
)

func TestHandoffRoundTrip(t *testing.T) {
	cliKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := Payload{
		Version:      1,
		Server:       "https://notes.example.com",
		Token:        "lnt.aHR0cHM6Ly9ub3Rlcy5leGFtcGxlLmNvbQ.01JC8000000000000000000000.c2VjcmV0",
		TokenID:      "01JC8000000000000000000000",
		Username:     "owner",
		InstanceName: "home",
		IssuedAt:     "2026-11-25T18:04:12Z",
	}
	blob, err := Encrypt(cliKey.PublicKey().Bytes(), "01JC8REQUEST", payload.Server, payload)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(blob, "lnh1.") {
		t.Fatalf("handoff = %q, want lnh1. prefix", blob)
	}

	got, err := Decrypt(blob, cliKey, "01JC8REQUEST", payload.Server)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != payload {
		t.Fatalf("payload = %+v, want %+v", got, payload)
	}
}

func TestHandoffRejectsTamperingAndContext(t *testing.T) {
	cliKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := Payload{Version: 1, Server: "https://notes.example.com", Token: "lnt.x"}
	blob, err := Encrypt(cliKey.PublicKey().Bytes(), "req-1", payload.Server, payload)
	if err != nil {
		t.Fatal(err)
	}

	otherKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		blob    string
		key     *ecdh.PrivateKey
		request string
		origin  string
	}{
		"wrong key":      {blob, otherKey, "req-1", payload.Server},
		"wrong request":  {blob, cliKey, "req-2", payload.Server},
		"wrong origin":   {blob, cliKey, "req-1", "https://evil.example.com"},
		"bad scheme":     {"lnt.abc", cliKey, "req-1", payload.Server},
		"tampered bytes": {blob[:len(blob)-2] + "AA", cliKey, "req-1", payload.Server},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decrypt(tc.blob, tc.key, tc.request, tc.origin); err == nil {
				t.Fatal("expected decryption to fail")
			}
		})
	}
}

func TestEncryptRejectsBadPublicKey(t *testing.T) {
	if _, err := Encrypt([]byte("short"), "req", "https://notes.example.com", Payload{Version: 1}); err == nil {
		t.Fatal("expected an error for a non-X25519 public key")
	}
}
