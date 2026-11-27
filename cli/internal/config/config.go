// Package config stores the CLI's server and device-token settings.
//
// The file is TOML at ~/.config/lewnotes/config.toml (0600). Environment
// variables are applied by the command layer, not here, so the file stays the
// single on-disk representation.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// File is the on-disk CLI configuration.
type File struct {
	Server     string `toml:"server,omitempty"`
	Token      string `toml:"token,omitempty"`
	DeviceName string `toml:"device_name,omitempty"`
}

// Path resolves the config file path. An explicit --config wins, then
// LEWNOTES_CONFIG, then the platform user-config directory.
func Path(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if fromEnv := os.Getenv("LEWNOTES_CONFIG"); fromEnv != "" {
		return fromEnv, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user config directory: %w", err)
	}
	return filepath.Join(dir, "lewnotes", "config.toml"), nil
}

// Load reads the config file. A missing file is not an error: it returns an
// empty config so first-run login can proceed.
func Load(path string) (File, string, error) {
	resolved, err := Path(path)
	if err != nil {
		return File{}, "", err
	}
	data, err := os.ReadFile(resolved)
	if errors.Is(err, os.ErrNotExist) {
		return File{}, resolved, nil
	}
	if err != nil {
		return File{}, resolved, fmt.Errorf("read config: %w", err)
	}
	var file File
	if err := toml.Unmarshal(data, &file); err != nil {
		return File{}, resolved, fmt.Errorf("parse %s: %w", resolved, err)
	}
	return file, resolved, nil
}

// Save writes the config atomically with owner-only permissions.
func Save(path string, file File) error {
	encoded, err := toml.Marshal(file)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	temp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("secure config file: %w", err)
	}
	if _, err := temp.Write(encoded); err != nil {
		temp.Close()
		return fmt.Errorf("write config: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
