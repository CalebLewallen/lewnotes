package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var envKeys = []string{
	"LEWNOTES_LISTEN", "LEWNOTES_DATA_DIR", "LEWNOTES_LOG_LEVEL", "LEWNOTES_PUBLIC_URL",
	"LEWNOTES_TRUST_PROXY", "LEWNOTES_VAULT", "LEWNOTES_COOKIE_SECURE", "LEWNOTES_SESSION_TTL",
	"LEWNOTES_TOKEN_LAST_USED_INTERVAL",
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range envKeys {
		key := key
		old, ok := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
		t.Cleanup(func() {
			if ok {
				_ = os.Setenv(key, old)
			}
		})
	}
}

func TestDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Listen != ":8080" {
		t.Errorf("listen = %q, want :8080", cfg.Server.Listen)
	}
	if cfg.Vault.Inbox != "inbox" || cfg.Vault.Attachments != "attachments" {
		t.Errorf("vault dirs = %q/%q", cfg.Vault.Inbox, cfg.Vault.Attachments)
	}
	if !filepath.IsAbs(cfg.Vault.Path) || !filepath.IsAbs(cfg.Server.DataDir) {
		t.Errorf("paths are not absolute: %q %q", cfg.Vault.Path, cfg.Server.DataDir)
	}
	if cfg.Auth.SessionTTL.Std() != 720*time.Hour {
		t.Errorf("session ttl = %v", cfg.Auth.SessionTTL.Std())
	}
	if cfg.Auth.TokenLastUsedInterval.Std() != time.Hour {
		t.Errorf("token last-used interval = %v", cfg.Auth.TokenLastUsedInterval.Std())
	}
}

func TestPublicURLMustBeAnOrigin(t *testing.T) {
	clearEnv(t)

	path := filepath.Join(t.TempDir(), "lewnotes.toml")
	if err := os.WriteFile(path, []byte("[server]\npublic_url = \"notes.example.com\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "server.public_url") {
		t.Fatalf("Load() error = %v, want a server.public_url validation error", err)
	}

	if err := os.WriteFile(path, []byte("[server]\npublic_url = \"https://notes.example.com/\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.PublicURL != "https://notes.example.com" {
		t.Errorf("public_url = %q, want the trailing slash stripped", cfg.Server.PublicURL)
	}
}

func TestFileAndEnvPrecedence(t *testing.T) {
	clearEnv(t)

	path := filepath.Join(t.TempDir(), "lewnotes.toml")
	body := `
[server]
listen = ":9000"
data_dir = "./data-dir"

[vault]
path = "./vault-dir"

[auth]
session_ttl = "90m"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEWNOTES_LISTEN", ":9100")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Listen != ":9100" {
		t.Errorf("listen = %q, want env override :9100", cfg.Server.Listen)
	}
	if got := filepath.Base(cfg.Vault.Path); got != "vault-dir" {
		t.Errorf("vault = %q, want .../vault-dir", cfg.Vault.Path)
	}
	if got := filepath.Base(cfg.Server.DataDir); got != "data-dir" {
		t.Errorf("data dir = %q, want .../data-dir", cfg.Server.DataDir)
	}
	if cfg.Auth.SessionTTL.Std() != 90*time.Minute {
		t.Errorf("session ttl = %v, want 90m", cfg.Auth.SessionTTL.Std())
	}
	if cfg.Source != path {
		t.Errorf("source = %q, want %q", cfg.Source, path)
	}
}

func TestInvalidValueNamesKeyAndSource(t *testing.T) {
	clearEnv(t)

	path := filepath.Join(t.TempDir(), "bad.toml")
	if err := os.WriteFile(path, []byte("[server]\nlog_level = \"loud\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want validation error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "server.log_level") {
		t.Errorf("error %q does not name server.log_level", msg)
	}
	if !strings.Contains(msg, "bad.toml") {
		t.Errorf("error %q does not name its source", msg)
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	clearEnv(t)

	path := filepath.Join(t.TempDir(), "unknown.toml")
	if err := os.WriteFile(path, []byte("[server]\nbogus = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want unknown-key error")
	}
}

func TestInvalidEnvValueNamesVariable(t *testing.T) {
	clearEnv(t)
	t.Setenv("LEWNOTES_TRUST_PROXY", "sometimes")

	_, err := Load("")
	if err == nil {
		t.Fatal("Load() error = nil, want parse error")
	}
	if !strings.Contains(err.Error(), "LEWNOTES_TRUST_PROXY") {
		t.Errorf("error %q does not name the env var", err)
	}
}
