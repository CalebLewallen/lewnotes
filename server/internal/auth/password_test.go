package auth

import (
	"strings"
	"testing"
)

var testParams = Argon2Params{Memory: 1024, Time: 1, Threads: 1, SaltLength: 16, KeyLength: 32}

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple", testParams)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=1024,t=1,p=1$") {
		t.Errorf("hash = %q, want PHC argon2id prefix", hash)
	}

	ok, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil || !ok {
		t.Fatalf("VerifyPassword() = %v, %v; want true, nil", ok, err)
	}

	ok, err = VerifyPassword(hash, "correct horse battery stapl3")
	if err != nil {
		t.Fatalf("VerifyPassword(wrong) error = %v", err)
	}
	if ok {
		t.Error("VerifyPassword(wrong) = true, want false")
	}

	second, err := HashPassword("correct horse battery staple", testParams)
	if err != nil {
		t.Fatal(err)
	}
	if second == hash {
		t.Error("two hashes are identical; salts are not random")
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"plain":        "not-a-hash",
		"wrong algo":   "$argon2i$v=19$m=1024,t=1,p=1$c2FsdA$a2V5",
		"wrong fields": "$argon2id$v=19$m=1024,t=1$c2FsdA$a2V5",
		"bad version":  "$argon2id$v=16$m=1024,t=1,p=1$c2FsdA$a2V5",
		"bad params":   "$argon2id$v=19$m=zero,t=1,p=1$c2FsdA$a2V5",
		"bad salt":     "$argon2id$v=19$m=1024,t=1,p=1$!!!$a2V5",
		"zero memory":  "$argon2id$v=19$m=0,t=1,p=1$c2FsdA$a2V5",
	}
	for name, hash := range cases {
		if _, err := VerifyPassword(hash, "whatever password"); err == nil {
			t.Errorf("%s: VerifyPassword() error = nil, want ErrInvalidHash", name)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short"); err == nil {
		t.Error("ValidatePassword(short) = nil, want policy error")
	} else if _, ok := err.(*PasswordPolicyError); !ok {
		t.Errorf("error type = %T, want *PasswordPolicyError", err)
	}

	if err := ValidatePassword("long enough"); err != nil {
		t.Errorf("ValidatePassword(valid) = %v, want nil", err)
	}

	if err := ValidatePassword(strings.Repeat("a", MaxPasswordLength+1)); err == nil {
		t.Error("ValidatePassword(too long) = nil, want policy error")
	}
}
