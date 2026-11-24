package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/oklog/ulid/v2"
)

// Device token format:
//
//	lnt.<base64url(server_url)>.<token_id_ulid>.<base64url(secret)>
//
// "." never appears in base64url or Crockford base32, so splitting is
// unambiguous. The URL is advisory: the server ignores it on verification, and
// the CLI uses it to discover where the token belongs.
const (
	TokenScheme      = "lnt"
	TokenSecretBytes = 32
	tokenSeparator   = "."
)

var (
	ErrInvalidToken     = errors.New("invalid token")
	ErrInvalidServerURL = errors.New("invalid server URL")
)

// ParsedToken is the decoded form of a device token.
type ParsedToken struct {
	ServerURL string
	ID        string
	Secret    []byte
}

// ValidateServerURL checks that raw is an absolute http(s) origin and returns it
// normalized without a trailing slash.
func ValidateServerURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidServerURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: scheme must be http or https", ErrInvalidServerURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%w: missing host", ErrInvalidServerURL)
	}
	if u.User != nil {
		return "", fmt.Errorf("%w: must not contain credentials", ErrInvalidServerURL)
	}
	if u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("%w: must be an origin such as https://notes.example.com", ErrInvalidServerURL)
	}
	return u.Scheme + "://" + u.Host, nil
}

// EncodeToken builds the wire format from its parts.
func EncodeToken(serverURL, id string, secret []byte) (string, error) {
	origin, err := ValidateServerURL(serverURL)
	if err != nil {
		return "", err
	}
	if _, err := ulid.Parse(id); err != nil {
		return "", fmt.Errorf("%w: token id must be a ULID", ErrInvalidToken)
	}
	if len(secret) != TokenSecretBytes {
		return "", fmt.Errorf("%w: secret must be %d bytes", ErrInvalidToken, TokenSecretBytes)
	}

	return strings.Join([]string{
		TokenScheme,
		base64.RawURLEncoding.EncodeToString([]byte(origin)),
		id,
		base64.RawURLEncoding.EncodeToString(secret),
	}, tokenSeparator), nil
}

// ParseToken decodes a device token and validates its structure. It does not
// prove the secret is correct; the server compares its hash.
func ParseToken(raw string) (ParsedToken, error) {
	parts := strings.Split(strings.TrimSpace(raw), tokenSeparator)
	if len(parts) != 4 || parts[0] != TokenScheme {
		return ParsedToken{}, ErrInvalidToken
	}

	urlBytes, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return ParsedToken{}, fmt.Errorf("%w: malformed server URL", ErrInvalidToken)
	}
	origin, err := ValidateServerURL(string(urlBytes))
	if err != nil {
		return ParsedToken{}, err
	}

	if _, err := ulid.Parse(parts[2]); err != nil {
		return ParsedToken{}, fmt.Errorf("%w: malformed token id", ErrInvalidToken)
	}

	secret, err := base64.RawURLEncoding.Strict().DecodeString(parts[3])
	if err != nil || len(secret) != TokenSecretBytes {
		return ParsedToken{}, fmt.Errorf("%w: malformed secret", ErrInvalidToken)
	}

	return ParsedToken{ServerURL: origin, ID: parts[2], Secret: secret}, nil
}

// NewToken generates a fresh token and returns both its wire form and parts.
func NewToken(serverURL string) (string, ParsedToken, error) {
	secret := make([]byte, TokenSecretBytes)
	if _, err := io.ReadFull(rand.Reader, secret); err != nil {
		return "", ParsedToken{}, fmt.Errorf("generate token secret: %w", err)
	}
	parsed := ParsedToken{ID: ulid.Make().String(), Secret: secret}

	raw, err := EncodeToken(serverURL, parsed.ID, parsed.Secret)
	if err != nil {
		return "", ParsedToken{}, err
	}
	parsed.ServerURL, err = ValidateServerURL(serverURL)
	if err != nil {
		return "", ParsedToken{}, err
	}
	return raw, parsed, nil
}

// HashSecret returns the hex sha256 stored in api_tokens.token_hash.
func HashSecret(secret []byte) string {
	sum := sha256.Sum256(secret)
	return hex.EncodeToString(sum[:])
}

// VerifySecret compares a stored hash with a presented secret in constant time.
func VerifySecret(encodedHash string, secret []byte) bool {
	expected := HashSecret(secret)
	return subtle.ConstantTimeCompare([]byte(encodedHash), []byte(expected)) == 1
}

// DisplayPrefix is the short, non-secret identifier shown in the token list,
// for example "lnt.aHR0…k8s".
func DisplayPrefix(raw string) string {
	parts := strings.Split(strings.TrimSpace(raw), tokenSeparator)
	if len(parts) != 4 || parts[0] != TokenScheme {
		return ""
	}
	short := func(s string) string {
		if len(s) <= 7 {
			return s
		}
		return s[:4] + "…" + s[len(s)-3:]
	}
	secretTail := parts[3]
	if len(secretTail) > 3 {
		secretTail = secretTail[len(secretTail)-3:]
	}
	return TokenScheme + "." + short(parts[1]) + "…" + secretTail
}

// VerifyCSRF compares a session's stored CSRF token with the submitted header.
func VerifyCSRF(expected, provided string) bool {
	if expected == "" || provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}
