// Package clilogin owns CLI login requests: the short-lived rows a CLI creates
// before a human approves it in the browser.
package clilogin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/caleblewallen/lewnotes/server/internal/auth"
	"github.com/caleblewallen/lewnotes/server/internal/handoff"
	"github.com/oklog/ulid/v2"
)

// Request statuses. Only `pending` can be approved or denied, and only
// `approved` can be consumed.
const (
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusDenied    = "denied"
	StatusConsumed  = "consumed"
	StatusExpired   = "expired"
	StatusCancelled = "cancelled"
)

const (
	// DefaultTTL is how long a login request stays approvable.
	DefaultTTL = 10 * time.Minute

	userCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	userCodeLength   = 8
)

var (
	ErrNotFound     = errors.New("login request not found")
	ErrExpired      = errors.New("login request expired")
	ErrNotPending   = errors.New("login request is not pending")
	ErrConsumed     = errors.New("login request already consumed")
	ErrInvalidCode  = errors.New("invalid login code")
	ErrInvalidState = errors.New("invalid handoff state")
)

// Request is one CLI login attempt.
type Request struct {
	ID           string
	PublicKey    []byte
	StateHash    string
	DeviceName   string
	Client       string
	RedirectURI  string
	ServerOrigin string
	UserCode     string
	Status       string
	UserID       string
	TokenID      string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// CreateParams is the untrusted input from POST /api/v1/cli/login.
type CreateParams struct {
	PublicKey    []byte
	State        string
	DeviceName   string
	Client       string
	RedirectURI  string
	ServerOrigin string
}

// Options configures the service.
type Options struct {
	DB   *sql.DB
	Auth *auth.Service
	Now  func() time.Time
	Rand io.Reader
	TTL  time.Duration
}

// Service creates, approves, and consumes login requests.
type Service struct {
	db   *sql.DB
	auth *auth.Service
	now  func() time.Time
	rand io.Reader
	ttl  time.Duration
}

// NewService builds a login-request service.
func NewService(opts Options) (*Service, error) {
	if opts.DB == nil {
		return nil, errors.New("clilogin: database is required")
	}
	if opts.Auth == nil {
		return nil, errors.New("clilogin: auth service is required")
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.Rand == nil {
		opts.Rand = rand.Reader
	}
	if opts.TTL <= 0 {
		opts.TTL = DefaultTTL
	}
	return &Service{db: opts.DB, auth: opts.Auth, now: opts.Now, rand: opts.Rand, ttl: opts.TTL}, nil
}

// Create inserts a pending request and returns it with a display user code.
func (s *Service) Create(ctx context.Context, p CreateParams) (Request, error) {
	code, err := s.uniqueUserCode(ctx)
	if err != nil {
		return Request{}, err
	}
	now := s.now()
	req := Request{
		ID:           ulid.Make().String(),
		PublicKey:    p.PublicKey,
		StateHash:    HashState(p.State),
		DeviceName:   p.DeviceName,
		Client:       p.Client,
		RedirectURI:  p.RedirectURI,
		ServerOrigin: p.ServerOrigin,
		UserCode:     code,
		Status:       StatusPending,
		CreatedAt:    now,
		ExpiresAt:    now.Add(s.ttl),
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO cli_login_requests
			(id, public_key, state_hash, device_name, client, redirect_uri, server_origin,
			 user_code, status, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.ID, req.PublicKey, req.StateHash, req.DeviceName, req.Client, req.RedirectURI,
		req.ServerOrigin, req.UserCode, req.Status,
		req.CreatedAt.Unix(), req.ExpiresAt.Unix()); err != nil {
		return Request{}, fmt.Errorf("create login request: %w", err)
	}
	return req, nil
}

// Get loads one request by id.
func (s *Service) Get(ctx context.Context, id string) (Request, error) {
	var (
		req       Request
		created   int64
		expires   int64
		userID    sql.NullString
		tokenID   sql.NullString
		redirect  string
		publicKey []byte
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, public_key, state_hash, device_name, client, COALESCE(redirect_uri, ''),
		       server_origin, user_code, status, COALESCE(user_id, ''), COALESCE(token_id, ''),
		       created_at, expires_at
		FROM cli_login_requests WHERE id = ?`, id).
		Scan(&req.ID, &publicKey, &req.StateHash, &req.DeviceName, &req.Client, &redirect,
			&req.ServerOrigin, &req.UserCode, &req.Status, &userID, &tokenID, &created, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("read login request: %w", err)
	}
	req.PublicKey = publicKey
	req.RedirectURI = redirect
	req.UserID = userID.String
	req.TokenID = tokenID.String
	req.CreatedAt = time.Unix(created, 0).UTC()
	req.ExpiresAt = time.Unix(expires, 0).UTC()
	if req.Status == StatusPending && !s.now().Before(req.ExpiresAt) {
		_ = s.expire(ctx, req.ID)
		req.Status = StatusExpired
	}
	return req, nil
}

// VerifyState compares the presented state with the stored hash.
func (s *Service) VerifyState(req Request, state string) bool {
	if state == "" {
		return false
	}
	return subtleCompare(HashState(state), req.StateHash)
}

// FindByUserCode resolves a pending request from the short code shown by a
// headless CLI. Dashes, spaces, and common confusables are normalized.
func (s *Service) FindByUserCode(ctx context.Context, code string) (Request, error) {
	normalized := NormalizeUserCode(code)
	if len(normalized) != userCodeLength {
		return Request{}, ErrInvalidCode
	}
	var id string
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM cli_login_requests
		WHERE user_code = ? AND status = ? AND expires_at > ?`,
		normalized, StatusPending, s.now().Unix()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, ErrInvalidCode
	}
	if err != nil {
		return Request{}, fmt.Errorf("resolve login code: %w", err)
	}
	return s.Get(ctx, id)
}

// Approve mints a device token, encrypts the handoff to the CLI key, and moves
// the request to approved. The plaintext token exists only in the envelope.
func (s *Service) Approve(ctx context.Context, id, userID, username, deviceName string) (Request, error) {
	req, err := s.Get(ctx, id)
	if err != nil {
		return Request{}, err
	}
	if req.Status == StatusExpired {
		return Request{}, ErrExpired
	}
	if req.Status != StatusPending {
		return Request{}, ErrNotPending
	}
	if deviceName == "" {
		deviceName = req.DeviceName
	}

	issued, err := s.auth.CreateToken(ctx, userID, deviceName, req.ServerOrigin, req.Client)
	if err != nil {
		return Request{}, fmt.Errorf("mint login token: %w", err)
	}
	blob, err := handoff.Encrypt(req.PublicKey, req.ID, req.ServerOrigin, handoff.Payload{
		Version:      1,
		Server:       req.ServerOrigin,
		Token:        issued.Raw,
		TokenID:      issued.Record.ID,
		DeviceName:   deviceName,
		Username:     username,
		InstanceName: "",
		IssuedAt:     s.now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return Request{}, err
	}

	now := s.now()
	result, err := s.db.ExecContext(ctx, `
		UPDATE cli_login_requests
		SET status = ?, user_id = ?, token_id = ?, handoff = ?, device_name = ?, approved_at = ?
		WHERE id = ? AND status = ?`,
		StatusApproved, userID, issued.Record.ID, blob, deviceName, now.Unix(), id, StatusPending)
	if err != nil {
		return Request{}, fmt.Errorf("approve login request: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Request{}, fmt.Errorf("approve login rows: %w", err)
	}
	if rows == 0 {
		return Request{}, ErrNotPending
	}
	return s.Get(ctx, id)
}

// Deny marks a pending request denied.
func (s *Service) Deny(ctx context.Context, id, userID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE cli_login_requests SET status = ?, user_id = ?
		WHERE id = ? AND status = ?`, StatusDenied, userID, id, StatusPending)
	if err != nil {
		return fmt.Errorf("deny login request: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("deny login rows: %w", err)
	}
	if rows == 0 {
		return ErrNotPending
	}
	return nil
}

// Cancel marks a pending request cancelled (best-effort on Ctrl-C).
func (s *Service) Cancel(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE cli_login_requests SET status = ?
		WHERE id = ? AND status = ?`, StatusCancelled, id, StatusPending)
	if err != nil {
		return fmt.Errorf("cancel login request: %w", err)
	}
	return nil
}

// TakeHandoff consumes an approved request and returns its envelope exactly
// once. Later calls return ErrConsumed.
func (s *Service) TakeHandoff(ctx context.Context, id, state string) (string, error) {
	req, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if !s.VerifyState(req, state) {
		return "", ErrInvalidState
	}
	switch req.Status {
	case StatusPending:
		return "", ErrNotPending
	case StatusDenied, StatusCancelled:
		return "", ErrNotPending
	case StatusExpired:
		return "", ErrExpired
	case StatusConsumed:
		return "", ErrConsumed
	case StatusApproved:
	default:
		return "", ErrNotFound
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin consume: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var blob string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(handoff, '') FROM cli_login_requests
		WHERE id = ? AND status = ?`, id, StatusApproved).Scan(&blob); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrConsumed
		}
		return "", fmt.Errorf("read handoff: %w", err)
	}
	if blob == "" {
		return "", ErrConsumed
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE cli_login_requests
		SET status = ?, handoff = NULL, consumed_at = ?
		WHERE id = ? AND status = ?`, StatusConsumed, s.now().Unix(), id, StatusApproved); err != nil {
		return "", fmt.Errorf("consume handoff: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit consume: %w", err)
	}
	return blob, nil
}

// Sweep deletes requests older than maxAge. It runs at startup and hourly.
func (s *Service) Sweep(ctx context.Context, maxAge time.Duration) error {
	cutoff := s.now().Add(-maxAge).Unix()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM cli_login_requests WHERE created_at < ?`, cutoff); err != nil {
		return fmt.Errorf("sweep login requests: %w", err)
	}
	return nil
}

func (s *Service) expire(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE cli_login_requests SET status = ?
		WHERE id = ? AND status = ? AND expires_at <= ?`,
		StatusExpired, id, StatusPending, s.now().Unix())
	return err
}

func (s *Service) uniqueUserCode(ctx context.Context) (string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		code, err := s.newUserCode()
		if err != nil {
			return "", err
		}
		var exists bool
		if err := s.db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM cli_login_requests WHERE user_code = ? AND status = ?)`,
			code, StatusPending).Scan(&exists); err != nil {
			return "", fmt.Errorf("check login code: %w", err)
		}
		if !exists {
			return code, nil
		}
	}
	return "", errors.New("could not allocate a unique login code")
}

func (s *Service) newUserCode() (string, error) {
	buf := make([]byte, userCodeLength)
	if _, err := io.ReadFull(s.rand, buf); err != nil {
		return "", fmt.Errorf("generate login code: %w", err)
	}
	out := make([]byte, userCodeLength)
	for i, b := range buf {
		out[i] = userCodeAlphabet[int(b)%len(userCodeAlphabet)]
	}
	return string(out), nil
}

// DisplayUserCode formats a stored code as XXXX-XXXX.
func DisplayUserCode(code string) string {
	if len(code) != userCodeLength {
		return code
	}
	return code[:4] + "-" + code[4:]
}

// NormalizeUserCode uppercases a typed code, strips separators, and folds the
// confusable characters Crockford base32 leaves out.
func NormalizeUserCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(code)) {
		switch r {
		case '-', ' ', '\t':
			continue
		case 'I', 'L':
			r = '1'
		case 'O':
			r = '0'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// HashState returns the hex sha256 stored for a CLI state nonce.
func HashState(state string) string {
	sum := sha256.Sum256([]byte(state))
	return hex.EncodeToString(sum[:])
}

// NewStateToken returns a base64url nonce suitable for the CLI state header.
func NewStateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func subtleCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
