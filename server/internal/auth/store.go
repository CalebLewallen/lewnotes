package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrNoUser means the instance has not been set up yet.
	ErrNoUser = errors.New("no user")
	// ErrAlreadySetup means a user already exists.
	ErrAlreadySetup = errors.New("instance is already set up")
	// ErrSessionNotFound means the session id is unknown (or expired rows were pruned).
	ErrSessionNotFound = errors.New("session not found")
	// ErrTokenNotFound means the token id is unknown or revoked.
	ErrTokenNotFound = errors.New("token not found")
)

// Store owns the SQL for server-only state: users, sessions, and device tokens.
// It shares the index's single database connection pool.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// User is the single owner account.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	InstanceName string
	CreatedAt    time.Time
}

// StoredSession is one session row.
type StoredSession struct {
	IDHash     string
	UserID     string
	Username   string
	CSRFToken  string
	LastSeenAt time.Time
	ExpiresAt  time.Time
	UserAgent  string
	IP         string
}

// Token is one device token row. The plaintext secret is never stored.
type Token struct {
	ID         string
	UserID     string
	Username   string
	Name       string
	TokenHash  string
	Prefix     string
	Scope      string
	UserAgent  string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// HasUser reports whether setup has happened.
func (s *Store) HasUser(ctx context.Context) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("check for user: %w", err)
	}
	return exists, nil
}

// User returns the owner account.
func (s *Store) User(ctx context.Context) (User, error) {
	var (
		user    User
		created int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, instance_name, created_at
		FROM users ORDER BY created_at LIMIT 1`).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.InstanceName, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNoUser
	}
	if err != nil {
		return User{}, fmt.Errorf("read user: %w", err)
	}
	user.CreatedAt = time.Unix(created, 0).UTC()
	return user, nil
}

// UserByID returns one owner account by id.
func (s *Store) UserByID(ctx context.Context, id string) (User, error) {
	var (
		user    User
		created int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, instance_name, created_at
		FROM users WHERE id = ?`, id).
		Scan(&user.ID, &user.Username, &user.PasswordHash, &user.InstanceName, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNoUser
	}
	if err != nil {
		return User{}, fmt.Errorf("read user %s: %w", id, err)
	}
	user.CreatedAt = time.Unix(created, 0).UTC()
	return user, nil
}

// CreateFirstUser inserts the owner only when no user exists. The check and the
// insert share a transaction, so concurrent setup attempts cannot both succeed.
func (s *Store) CreateFirstUser(ctx context.Context, user User) (User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, fmt.Errorf("begin setup: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return User{}, fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return User{}, ErrAlreadySetup
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, instance_name)
		VALUES (?, ?, ?, ?)`,
		user.ID, user.Username, user.PasswordHash, user.InstanceName); err != nil {
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return User{}, fmt.Errorf("commit setup: %w", err)
	}
	return user, nil
}

// SetPasswordHash replaces the owner's password hash.
func (s *Store) SetPasswordHash(ctx context.Context, userID, hash string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE users SET password_hash = ?, updated_at = unixepoch() WHERE id = ?`, hash, userID); err != nil {
		return fmt.Errorf("update password hash: %w", err)
	}
	return nil
}

// CreateSession inserts a session row.
func (s *Store) CreateSession(ctx context.Context, sess StoredSession) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id_hash, user_id, csrf_token, last_seen_at, expires_at, user_agent, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sess.IDHash, sess.UserID, sess.CSRFToken,
		sess.LastSeenAt.UTC().Unix(), sess.ExpiresAt.UTC().Unix(),
		sess.UserAgent, sess.IP); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// Session loads one session joined with its username.
func (s *Store) Session(ctx context.Context, idHash string) (StoredSession, error) {
	var (
		sess      StoredSession
		lastSeen  int64
		expiresAt int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT s.id_hash, s.user_id, u.username, s.csrf_token, s.last_seen_at, s.expires_at, s.user_agent, s.ip
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.id_hash = ?`, idHash).
		Scan(&sess.IDHash, &sess.UserID, &sess.Username, &sess.CSRFToken,
			&lastSeen, &expiresAt, &sess.UserAgent, &sess.IP)
	if errors.Is(err, sql.ErrNoRows) {
		return StoredSession{}, ErrSessionNotFound
	}
	if err != nil {
		return StoredSession{}, fmt.Errorf("read session: %w", err)
	}
	sess.LastSeenAt = time.Unix(lastSeen, 0).UTC()
	sess.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	return sess, nil
}

// TouchSession slides a session's expiry forward.
func (s *Store) TouchSession(ctx context.Context, idHash string, seen, expires time.Time) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id_hash = ?`,
		seen.UTC().Unix(), expires.UTC().Unix(), idHash); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

// DeleteSession revokes one session.
func (s *Store) DeleteSession(ctx context.Context, idHash string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, idHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteSessionsForUser revokes every session for a user.
func (s *Store) DeleteSessionsForUser(ctx context.Context, userID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	return nil
}

// CreateToken inserts a device token row.
func (s *Store) CreateToken(ctx context.Context, token Token) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO api_tokens (id, user_id, name, token_hash, prefix, scope, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		token.ID, token.UserID, token.Name, token.TokenHash, token.Prefix, token.Scope, token.UserAgent); err != nil {
		return fmt.Errorf("create token: %w", err)
	}
	return nil
}

// Tokens lists a user's active device tokens, newest first.
func (s *Store) Tokens(ctx context.Context, userID string) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, name, token_hash, prefix, scope, created_at, last_used_at, user_agent
		FROM api_tokens
		WHERE user_id = ? AND revoked_at IS NULL
		ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()

	tokens := []Token{}
	for rows.Next() {
		token, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

// TokenByID loads an active token and its owner's username.
func (s *Store) TokenByID(ctx context.Context, id string) (Token, error) {
	var (
		token    Token
		created  int64
		lastUsed sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT t.id, t.user_id, u.username, t.name, t.token_hash, t.prefix, t.scope,
		       t.created_at, t.last_used_at, t.user_agent
		FROM api_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.id = ? AND t.revoked_at IS NULL`, id).
		Scan(&token.ID, &token.UserID, &token.Username, &token.Name, &token.TokenHash,
			&token.Prefix, &token.Scope, &created, &lastUsed, &token.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return Token{}, ErrTokenNotFound
	}
	if err != nil {
		return Token{}, fmt.Errorf("read token: %w", err)
	}
	token.CreatedAt = time.Unix(created, 0).UTC()
	if lastUsed.Valid {
		t := time.Unix(lastUsed.Int64, 0).UTC()
		token.LastUsedAt = &t
	}
	return token, nil
}

// TouchToken records a token's last use.
func (s *Store) TouchToken(ctx context.Context, id string, at time.Time) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`,
		at.UTC().Unix(), id); err != nil {
		return fmt.Errorf("touch token: %w", err)
	}
	return nil
}

// RevokeToken revokes one token belonging to userID.
func (s *Store) RevokeToken(ctx context.Context, userID, id string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE api_tokens SET revoked_at = unixepoch()
		WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, id, userID)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke token rows: %w", err)
	}
	if n == 0 {
		return ErrTokenNotFound
	}
	return nil
}

// RevokeAllTokens revokes every active token for a user.
func (s *Store) RevokeAllTokens(ctx context.Context, userID string) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE api_tokens SET revoked_at = unixepoch()
		WHERE user_id = ? AND revoked_at IS NULL`, userID); err != nil {
		return fmt.Errorf("revoke all tokens: %w", err)
	}
	return nil
}

type tokenScanner interface {
	Scan(dest ...any) error
}

func scanToken(row tokenScanner) (Token, error) {
	var (
		token    Token
		created  int64
		lastUsed sql.NullInt64
	)
	if err := row.Scan(&token.ID, &token.UserID, &token.Name, &token.TokenHash, &token.Prefix,
		&token.Scope, &created, &lastUsed, &token.UserAgent); err != nil {
		return Token{}, fmt.Errorf("scan token row: %w", err)
	}
	token.CreatedAt = time.Unix(created, 0).UTC()
	if lastUsed.Valid {
		t := time.Unix(lastUsed.Int64, 0).UTC()
		token.LastUsedAt = &t
	}
	return token, nil
}
