// Package config loads lewnotes configuration from TOML and LEWNOTES_* environment variables.
//
// Precedence: defaults < TOML file < environment. Validation errors name both the
// offending key and where its value came from.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/pelletier/go-toml/v2"
)

// Duration is a time.Duration that decodes from TOML strings like "90m".
type Duration time.Duration

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// Std returns the underlying time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// Config is the fully resolved configuration.
type Config struct {
	Source string `toml:"-"` // path of the file that was loaded, if any

	Server      Server      `toml:"server"`
	Vault       Vault       `toml:"vault"`
	Index       Index       `toml:"index"`
	Todos       Todos       `toml:"todos"`
	Search      Search      `toml:"search"`
	Attachments Attachments `toml:"attachments"`
	Auth        Auth        `toml:"auth"`
}

type Server struct {
	Listen     string `toml:"listen"`
	DataDir    string `toml:"data_dir"`
	TrustProxy bool   `toml:"trust_proxy"`
	PublicURL  string `toml:"public_url"`
	LogLevel   string `toml:"log_level"`
}

type Vault struct {
	Path        string   `toml:"path"`
	Inbox       string   `toml:"inbox"`
	Attachments string   `toml:"attachments"`
	Ignore      []string `toml:"ignore"`
}

type Index struct {
	// ReconcileInterval of 0 disables the periodic drift check.
	ReconcileInterval Duration `toml:"reconcile_interval"`
}

type Todos struct {
	Globs            []string `toml:"globs"`
	ArchiveCompleted bool     `toml:"archive_completed"`
}

type Search struct {
	SnippetLength int `toml:"snippet_length"`
}

type Attachments struct {
	MaxSizeMB    int      `toml:"max_size_mb"`
	AllowedMIME  []string `toml:"allowed_mime"`
	NameStrategy string   `toml:"name_strategy"`
}

type Auth struct {
	SessionTTL            Duration `toml:"session_ttl"`
	TokenLastUsedInterval Duration `toml:"token_last_used_interval"`
	CookieSecure          bool     `toml:"cookie_secure"`
	AllowedOrigins        []string `toml:"allowed_origins"`
}

// Default returns the built-in defaults. These are valid on their own so that a
// missing config file is never an error.
func Default() *Config {
	return &Config{
		Server: Server{
			Listen:   ":8080",
			DataDir:  "./data",
			LogLevel: "info",
		},
		Vault: Vault{
			Path:        "./vault",
			Inbox:       "inbox",
			Attachments: "attachments",
			Ignore:      []string{".obsidian/**", ".trash/**", "node_modules/**"},
		},
		Todos: Todos{
			Globs: []string{"todo.md", "todos/**/*.md"},
		},
		Search: Search{SnippetLength: 32},
		Attachments: Attachments{
			MaxSizeMB:    50,
			AllowedMIME:  []string{"image/*", "application/pdf", "text/*"},
			NameStrategy: "original",
		},
		Auth: Auth{
			SessionTTL:            Duration(720 * time.Hour),
			TokenLastUsedInterval: Duration(time.Hour),
			CookieSecure:          true,
		},
	}
}

// Load reads defaults, then the TOML file at path (if any), then environment
// overrides, and validates the result.
func Load(path string) (*Config, error) {
	cfg := Default()
	sources := map[string]string{}

	usedPath := path
	if usedPath == "" {
		if _, err := os.Stat("lewnotes.toml"); err == nil {
			usedPath = "lewnotes.toml"
		}
	}

	if usedPath != "" {
		data, err := os.ReadFile(usedPath)
		if err != nil {
			return nil, fmt.Errorf("read config %s: %w", usedPath, err)
		}
		dec := toml.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", usedPath, err)
		}
		cfg.Source = usedPath
		sources["file"] = usedPath
	}

	if err := applyEnv(cfg, sources); err != nil {
		return nil, err
	}
	if err := cfg.validate(sources); err != nil {
		return nil, err
	}
	if err := cfg.Normalize(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Normalize makes filesystem paths absolute so later code never depends on cwd.
func (c *Config) Normalize() error {
	vaultPath, err := filepath.Abs(c.Vault.Path)
	if err != nil {
		return fmt.Errorf("vault.path: %w", err)
	}
	c.Vault.Path = vaultPath

	dataDir, err := filepath.Abs(c.Server.DataDir)
	if err != nil {
		return fmt.Errorf("server.data_dir: %w", err)
	}
	c.Server.DataDir = dataDir
	return nil
}

func applyEnv(cfg *Config, sources map[string]string) error {
	overrides := []struct {
		env string
		key string
		set func(string) error
	}{
		{"LEWNOTES_LISTEN", "server.listen", func(v string) error { cfg.Server.Listen = v; return nil }},
		{"LEWNOTES_DATA_DIR", "server.data_dir", func(v string) error { cfg.Server.DataDir = v; return nil }},
		{"LEWNOTES_LOG_LEVEL", "server.log_level", func(v string) error { cfg.Server.LogLevel = v; return nil }},
		{"LEWNOTES_PUBLIC_URL", "server.public_url", func(v string) error { cfg.Server.PublicURL = v; return nil }},
		{"LEWNOTES_TRUST_PROXY", "server.trust_proxy", func(v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return err
			}
			cfg.Server.TrustProxy = b
			return nil
		}},
		{"LEWNOTES_VAULT", "vault.path", func(v string) error { cfg.Vault.Path = v; return nil }},
		{"LEWNOTES_COOKIE_SECURE", "auth.cookie_secure", func(v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return err
			}
			cfg.Auth.CookieSecure = b
			return nil
		}},
		{"LEWNOTES_SESSION_TTL", "auth.session_ttl", func(v string) error {
			d, err := time.ParseDuration(v)
			if err != nil {
				return err
			}
			cfg.Auth.SessionTTL = Duration(d)
			return nil
		}},
		{"LEWNOTES_TOKEN_LAST_USED_INTERVAL", "auth.token_last_used_interval", func(v string) error {
			d, err := time.ParseDuration(v)
			if err != nil {
				return err
			}
			cfg.Auth.TokenLastUsedInterval = Duration(d)
			return nil
		}},
	}

	for _, o := range overrides {
		val, ok := os.LookupEnv(o.env)
		if !ok || val == "" {
			continue
		}
		if err := o.set(val); err != nil {
			return fmt.Errorf("%s (from env %s): %w", o.key, o.env, err)
		}
		sources[o.key] = "env " + o.env
	}
	return nil
}

func (c *Config) validate(sources map[string]string) error {
	var errs []error

	add := func(key, format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		if src := sourceOf(key, sources); src != "" {
			msg += " (from " + src + ")"
		}
		errs = append(errs, fmt.Errorf("%s: %s", key, msg))
	}

	if strings.TrimSpace(c.Server.Listen) == "" {
		add("server.listen", "must not be empty")
	}
	if strings.TrimSpace(c.Server.DataDir) == "" {
		add("server.data_dir", "must not be empty")
	}
	switch c.Server.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		add("server.log_level", "must be one of debug, info, warn, error")
	}
	if strings.TrimSpace(c.Vault.Path) == "" {
		add("vault.path", "must not be empty")
	}
	for _, pattern := range c.Vault.Ignore {
		if !doublestar.ValidatePattern(pattern) {
			add("vault.ignore", "invalid glob pattern %q", pattern)
			break
		}
	}
	if c.Search.SnippetLength < 0 {
		add("search.snippet_length", "must be >= 0")
	}
	if c.Attachments.MaxSizeMB <= 0 {
		add("attachments.max_size_mb", "must be > 0")
	}
	switch c.Attachments.NameStrategy {
	case "original", "hashed":
	default:
		add("attachments.name_strategy", "must be original or hashed")
	}
	if c.Auth.SessionTTL.Std() <= 0 {
		add("auth.session_ttl", "must be > 0")
	}
	if c.Auth.TokenLastUsedInterval.Std() <= 0 {
		add("auth.token_last_used_interval", "must be > 0")
	}

	return errors.Join(errs...)
}

// sourceOf reports where a key's value came from, for validation messages.
func sourceOf(key string, sources map[string]string) string {
	if src, ok := sources[key]; ok {
		return src
	}
	if file, ok := sources["file"]; ok {
		return "file " + file
	}
	return ""
}
