// Package auth owns credentials: password hashing, sessions, device tokens,
// CSRF tokens, and rate limits.
//
// Hashing uses argon2id with parameters stored inside the encoded hash, so
// parameters can be raised later without invalidating existing passwords.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2Params are the argon2id cost parameters. Memory is in KiB.
type Argon2Params struct {
	Memory     uint32
	Time       uint32
	Threads    uint8
	SaltLength uint32
	KeyLength  uint32
}

// DefaultArgon2Params follows the spec: m=64MiB, t=3, p=2, 16-byte salt,
// 32-byte key.
var DefaultArgon2Params = Argon2Params{
	Memory:     64 * 1024,
	Time:       3,
	Threads:    2,
	SaltLength: 16,
	KeyLength:  32,
}

// Password policy. Length is the only v1 rule; the web UI adds a strength
// meter, but the server must not depend on it.
const (
	MinPasswordLength = 10
	MaxPasswordLength = 1024
)

// PasswordPolicyError describes a rejected password in user-presentable terms.
type PasswordPolicyError struct{ Reason string }

func (e *PasswordPolicyError) Error() string { return e.Reason }

// ValidatePassword enforces the password policy.
func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return &PasswordPolicyError{Reason: fmt.Sprintf("Password must be at least %d characters.", MinPasswordLength)}
	}
	if len(password) > MaxPasswordLength {
		return &PasswordPolicyError{Reason: fmt.Sprintf("Password must be at most %d bytes.", MaxPasswordLength)}
	}
	return nil
}

// ErrInvalidHash means a stored hash could not be parsed.
var ErrInvalidHash = errors.New("invalid password hash")

// HashPassword returns a PHC-formatted argon2id hash.
func HashPassword(password string, params Argon2Params) (string, error) {
	if password == "" {
		return "", errors.New("password must not be empty")
	}
	if params.Memory == 0 || params.Time == 0 || params.Threads == 0 || params.SaltLength == 0 || params.KeyLength == 0 {
		return "", errors.New("invalid argon2 parameters")
	}

	salt := make([]byte, params.SaltLength)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, params.Time, params.Memory, params.Threads, params.KeyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		params.Memory, params.Time, params.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

type parsedHash struct {
	params Argon2Params
	salt   []byte
	key    []byte
}

func parseHash(encoded string) (parsedHash, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return parsedHash{}, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return parsedHash{}, ErrInvalidHash
	}

	var params Argon2Params
	for _, field := range strings.Split(parts[3], ",") {
		name, value, ok := strings.Cut(field, "=")
		if !ok {
			return parsedHash{}, ErrInvalidHash
		}
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return parsedHash{}, ErrInvalidHash
		}
		switch name {
		case "m":
			params.Memory = uint32(n)
		case "t":
			params.Time = uint32(n)
		case "p":
			if n > 255 {
				return parsedHash{}, ErrInvalidHash
			}
			params.Threads = uint8(n)
		default:
			return parsedHash{}, ErrInvalidHash
		}
	}

	// Bound the cost so a corrupted or crafted hash cannot exhaust memory.
	if params.Memory == 0 || params.Memory > 1<<20 || params.Time == 0 || params.Time > 10 || params.Threads == 0 {
		return parsedHash{}, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return parsedHash{}, ErrInvalidHash
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(key) == 0 || len(key) > 1024 {
		return parsedHash{}, ErrInvalidHash
	}

	params.SaltLength = uint32(len(salt))
	params.KeyLength = uint32(len(key))
	return parsedHash{params: params, salt: salt, key: key}, nil
}

// VerifyPassword checks a password against a stored hash in constant time.
func VerifyPassword(encoded, password string) (bool, error) {
	parsed, err := parseHash(encoded)
	if err != nil {
		return false, err
	}
	key := argon2.IDKey([]byte(password), parsed.salt, parsed.params.Time,
		parsed.params.Memory, parsed.params.Threads, parsed.params.KeyLength)
	return subtle.ConstantTimeCompare(key, parsed.key) == 1, nil
}
