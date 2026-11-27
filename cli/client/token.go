package client

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Device token format (shared with the server):
//
//	lnt.<base64url(server_url)>.<token_id_ulid>.<base64url(secret)>
//
// The URL is advisory: the CLI uses it to discover where the token belongs, but
// the server verifies only the token id and secret.
const TokenScheme = "lnt"

// TokenSecretBytes is the decoded length of the secret.
const TokenSecretBytes = 32

var (
	// ErrInvalidToken marks malformed or structurally invalid device tokens.
	ErrInvalidToken = errors.New("invalid token")
	// ErrInvalidServerURL marks URLs that are not plain http(s) origins.
	ErrInvalidServerURL = errors.New("invalid server URL")

	tokenIDPattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
)

// ParsedToken is the decoded form of a device token.
type ParsedToken struct {
	ServerURL string
	ID        string
	Secret    []byte
}

// ValidateServerURL checks that raw is an absolute http(s) origin and returns
// it normalized without a trailing slash.
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

// ParseToken decodes a device token and validates its structure. It does not
// prove the secret is correct; only the server can do that.
func ParseToken(raw string) (ParsedToken, error) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
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
	if !tokenIDPattern.MatchString(parts[2]) {
		return ParsedToken{}, fmt.Errorf("%w: malformed token id", ErrInvalidToken)
	}
	secret, err := base64.RawURLEncoding.Strict().DecodeString(parts[3])
	if err != nil || len(secret) != TokenSecretBytes {
		return ParsedToken{}, fmt.Errorf("%w: malformed secret", ErrInvalidToken)
	}
	return ParsedToken{ServerURL: origin, ID: parts[2], Secret: secret}, nil
}
