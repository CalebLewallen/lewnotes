// Package settings stores runtime instance settings that the owner can change
// from the web app. Values here are fallbacks: an equivalent key in config.toml
// or the environment always wins.
package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PublicURLKey is the effective public origin used in tokens and login links.
const PublicURLKey = "public_url"

// Store owns the SQL for the settings table.
type Store struct {
	db *sql.DB
}

// NewStore shares the index's single database pool.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Get returns a setting and whether it exists.
func (s *Store) Get(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read setting %s: %w", key, err)
	}
	return value, true, nil
}

// Set upserts a setting.
func (s *Store) Set(ctx context.Context, key, value string, at time.Time) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, at.UTC().Unix()); err != nil {
		return fmt.Errorf("write setting %s: %w", key, err)
	}
	return nil
}
