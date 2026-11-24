package auth

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
)

func TestTokenRoundTrip(t *testing.T) {
	raw, parsed, err := NewToken("https://notes.example.com/")
	if err != nil {
		t.Fatalf("NewToken() error = %v", err)
	}
	if !strings.HasPrefix(raw, "lnt.") {
		t.Errorf("token = %q, want lnt. prefix", raw)
	}
	if parsed.ServerURL != "https://notes.example.com" {
		t.Errorf("server URL = %q, want normalized origin", parsed.ServerURL)
	}

	decoded, err := ParseToken(raw)
	if err != nil {
		t.Fatalf("ParseToken() error = %v", err)
	}
	if decoded.ServerURL != parsed.ServerURL || decoded.ID != parsed.ID {
		t.Errorf("decoded = %+v, want %+v", decoded, parsed)
	}
	if len(decoded.Secret) != TokenSecretBytes {
		t.Errorf("secret length = %d, want %d", len(decoded.Secret), TokenSecretBytes)
	}

	if VerifySecret(HashSecret(parsed.Secret), parsed.Secret) != true {
		t.Error("VerifySecret(correct) = false")
	}
	tampered := append([]byte(nil), parsed.Secret...)
	tampered[0] ^= 0xff
	if VerifySecret(HashSecret(parsed.Secret), tampered) {
		t.Error("VerifySecret(tampered) = true")
	}
}

func TestDisplayPrefix(t *testing.T) {
	raw, _, err := NewToken("https://notes.example.com")
	if err != nil {
		t.Fatal(err)
	}
	prefix := DisplayPrefix(raw)
	if !strings.HasPrefix(prefix, "lnt.") || !strings.Contains(prefix, "…") {
		t.Errorf("prefix = %q, want lnt.… shape", prefix)
	}
	if len(prefix) >= len(raw) {
		t.Errorf("prefix %q is not shorter than the token", prefix)
	}
	if DisplayPrefix("nonsense") != "" {
		t.Errorf("DisplayPrefix(nonsense) = %q, want empty", DisplayPrefix("nonsense"))
	}
}

func TestParseTokenRejectsMalformedTokens(t *testing.T) {
	raw, parsed, err := NewToken("https://notes.example.com")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(raw, ".")

	badURL := base64.RawURLEncoding.EncodeToString([]byte("ftp://notes.example.com"))
	shortSecret := base64.RawURLEncoding.EncodeToString(make([]byte, 8))

	cases := map[string]string{
		"empty":        "",
		"three parts":  "lnt." + parts[1] + "." + parts[2],
		"wrong scheme": "xyz." + parts[1] + "." + parts[2] + "." + parts[3],
		"bad url":      "lnt." + badURL + "." + parts[2] + "." + parts[3],
		"bad id":       "lnt." + parts[1] + ".not-a-ulid." + parts[3],
		"short secret": "lnt." + parts[1] + "." + parts[2] + "." + shortSecret,
		"bad base64":   "lnt." + parts[1] + "." + parts[2] + ".!!!",
	}
	for name, token := range cases {
		if _, err := ParseToken(token); err == nil {
			t.Errorf("%s: ParseToken() error = nil, want rejection", name)
		}
	}

	if _, err := EncodeToken("ftp://notes.example.com", parsed.ID, parsed.Secret); err == nil {
		t.Error("EncodeToken(ftp URL) error = nil")
	}
	if _, err := EncodeToken("https://notes.example.com", "not-a-ulid", parsed.Secret); err == nil {
		t.Error("EncodeToken(bad id) error = nil")
	}
	if _, err := EncodeToken("https://notes.example.com", parsed.ID, []byte("short")); err == nil {
		t.Error("EncodeToken(short secret) error = nil")
	}

	if _, err := ParseToken("lnt." + parts[1] + "." + ulid.Make().String() + "." + parts[3]); err != nil {
		t.Errorf("ParseToken(valid shape) error = %v", err)
	}
}

func TestVerifyCSRF(t *testing.T) {
	if !VerifyCSRF("abc", "abc") {
		t.Error("VerifyCSRF(equal) = false")
	}
	for _, pair := range [][2]string{{"abc", "abd"}, {"", "abc"}, {"abc", ""}} {
		if VerifyCSRF(pair[0], pair[1]) {
			t.Errorf("VerifyCSRF(%q, %q) = true", pair[0], pair[1])
		}
	}
}
