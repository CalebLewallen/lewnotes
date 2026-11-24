package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

var (
	// ErrUnauthenticated means no valid session or token was presented.
	ErrUnauthenticated = errors.New("authentication required")
	// ErrInvalidCredentials is deliberately uniform for login and password checks.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrNotSetup means login was attempted before first-run setup.
	ErrNotSetup = errors.New("setup required")
	// ErrInvalidTokenName means a device token name failed validation.
	ErrInvalidTokenName = errors.New("invalid token name")
)

const (
	// SessionCookieName holds the opaque session id.
	SessionCookieName = "lewnotes_session"
	// CSRFCookieName is readable by the SPA and echoed in CSRFHeader.
	CSRFCookieName = "lewnotes_csrf"
	// CSRFHeader is required on cookie-authenticated mutations.
	CSRFHeader = "X-Lewnotes-CSRF"

	sessionIDBytes  = 32
	csrfTokenBytes  = 32
	sessionTouchAge = 5 * time.Minute
)

// RequestMeta is request context worth storing for the device list.
type RequestMeta struct {
	UserAgent string
	IP        string
}

// ServiceOptions configures credential handling.
type ServiceOptions struct {
	Store                 *Store
	SessionTTL            time.Duration
	TokenLastUsedInterval time.Duration
	Argon2                Argon2Params
	Now                   func() time.Time
	Rand                  io.Reader
	CookieSecure          bool
}

// Service issues and verifies credentials.
type Service struct {
	store         *Store
	sessionTTL    time.Duration
	tokenLastUsed time.Duration
	argon         Argon2Params
	now           func() time.Time
	rand          io.Reader
	cookieSecure  bool

	dummyOnce sync.Once
	dummyHash string
	dummyErr  error
}

// NewService builds a credential service.
func NewService(opts ServiceOptions) (*Service, error) {
	if opts.Store == nil {
		return nil, errors.New("auth: store is required")
	}
	if opts.SessionTTL <= 0 {
		opts.SessionTTL = 720 * time.Hour
	}
	if opts.TokenLastUsedInterval <= 0 {
		opts.TokenLastUsedInterval = time.Hour
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.Rand == nil {
		opts.Rand = rand.Reader
	}
	if opts.Argon2 == (Argon2Params{}) {
		opts.Argon2 = DefaultArgon2Params
	}
	return &Service{
		store:         opts.Store,
		sessionTTL:    opts.SessionTTL,
		tokenLastUsed: opts.TokenLastUsedInterval,
		argon:         opts.Argon2,
		now:           opts.Now,
		rand:          opts.Rand,
		cookieSecure:  opts.CookieSecure,
	}, nil
}

// Session is a freshly issued browser session. Token is the cookie value.
type Session struct {
	Token     string
	CSRF      string
	ExpiresAt time.Time
}

// Principal is the authenticated identity attached to a request.
type Principal struct {
	UserID    string
	Username  string
	Kind      string // "session" or "token"
	CSRF      string
	TokenID   string
	TokenName string
	Scope     string
}

type principalKey struct{}

// WithPrincipal stores the authenticated principal on a context.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the authenticated principal, if any.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// SetupRequired reports whether the instance still needs its owner account.
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	hasUser, err := s.store.HasUser(ctx)
	if err != nil {
		return false, err
	}
	return !hasUser, nil
}

// InstanceName returns the configured instance name, or "" before setup.
func (s *Service) InstanceName(ctx context.Context) (string, error) {
	user, err := s.store.User(ctx)
	if errors.Is(err, ErrNoUser) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return user.InstanceName, nil
}

// Setup creates the owner account and signs it in. It fails if a user exists.
func (s *Service) Setup(ctx context.Context, password, instanceName string, meta RequestMeta) (Session, User, error) {
	if err := ValidatePassword(password); err != nil {
		return Session{}, User{}, err
	}
	instanceName = strings.TrimSpace(instanceName)
	if instanceName == "" {
		instanceName = "lewnotes"
	}
	if utf8.RuneCountInString(instanceName) > 64 {
		return Session{}, User{}, fmt.Errorf("instance name must be at most 64 characters")
	}

	hash, err := HashPassword(password, s.argon)
	if err != nil {
		return Session{}, User{}, fmt.Errorf("hash password: %w", err)
	}

	user := User{
		ID:           ulid.Make().String(),
		Username:     "owner",
		PasswordHash: hash,
		InstanceName: instanceName,
		CreatedAt:    s.now(),
	}
	user, err = s.store.CreateFirstUser(ctx, user)
	if err != nil {
		return Session{}, User{}, err
	}

	session, err := s.newSession(ctx, user.ID, meta)
	if err != nil {
		return Session{}, User{}, err
	}
	return session, user, nil
}

// Login verifies the owner password and issues a session.
func (s *Service) Login(ctx context.Context, password string, meta RequestMeta) (Session, User, error) {
	user, err := s.store.User(ctx)
	if errors.Is(err, ErrNoUser) {
		return Session{}, User{}, ErrNotSetup
	}
	if err != nil {
		return Session{}, User{}, err
	}

	ok, err := VerifyPassword(user.PasswordHash, password)
	if err != nil {
		return Session{}, User{}, fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return Session{}, User{}, ErrInvalidCredentials
	}

	session, err := s.newSession(ctx, user.ID, meta)
	if err != nil {
		return Session{}, User{}, err
	}
	return session, user, nil
}

// Authenticate resolves a session cookie or bearer token to a principal.
func (s *Service) Authenticate(ctx context.Context, cookieValue, bearer string) (Principal, error) {
	if bearer != "" {
		return s.authenticateToken(ctx, bearer)
	}
	if cookieValue != "" {
		return s.authenticateSession(ctx, cookieValue)
	}
	return Principal{}, ErrUnauthenticated
}

func (s *Service) authenticateSession(ctx context.Context, cookieValue string) (Principal, error) {
	stored, err := s.store.Session(ctx, hashSessionID(cookieValue))
	if errors.Is(err, ErrSessionNotFound) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}

	now := s.now()
	if !now.Before(stored.ExpiresAt) {
		// Expired sessions are rejected and cleaned up best-effort.
		_ = s.store.DeleteSession(ctx, stored.IDHash)
		return Principal{}, ErrUnauthenticated
	}

	if now.Sub(stored.LastSeenAt) >= sessionTouchAge {
		expires := now.Add(s.sessionTTL)
		if err := s.store.TouchSession(ctx, stored.IDHash, now, expires); err != nil {
			return Principal{}, err
		}
	}

	return Principal{
		UserID:   stored.UserID,
		Username: stored.Username,
		Kind:     "session",
		CSRF:     stored.CSRFToken,
	}, nil
}

func (s *Service) authenticateToken(ctx context.Context, bearer string) (Principal, error) {
	parsed, err := ParseToken(bearer)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}

	token, err := s.store.TokenByID(ctx, parsed.ID)
	if errors.Is(err, ErrTokenNotFound) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}
	if !VerifySecret(token.TokenHash, parsed.Secret) {
		return Principal{}, ErrUnauthenticated
	}

	now := s.now()
	if token.LastUsedAt == nil || now.Sub(*token.LastUsedAt) >= s.tokenLastUsed {
		if err := s.store.TouchToken(ctx, token.ID, now); err != nil {
			return Principal{}, err
		}
	}

	return Principal{
		UserID:    token.UserID,
		Username:  token.Username,
		Kind:      "token",
		TokenID:   token.ID,
		TokenName: token.Name,
		Scope:     token.Scope,
	}, nil
}

// Logout revokes the session behind cookieValue. Unknown sessions are ignored.
func (s *Service) Logout(ctx context.Context, cookieValue string) error {
	if cookieValue == "" {
		return nil
	}
	if err := s.store.DeleteSession(ctx, hashSessionID(cookieValue)); err != nil {
		return err
	}
	return nil
}

// ChangePassword rotates the owner password and revokes every session. If
// revokeTokens is set, device tokens are revoked too. The caller gets a fresh
// session so it stays signed in.
func (s *Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string, revokeTokens bool, meta RequestMeta) (Session, error) {
	user, err := s.store.UserByID(ctx, userID)
	if err != nil {
		return Session{}, err
	}

	ok, err := VerifyPassword(user.PasswordHash, currentPassword)
	if err != nil {
		return Session{}, fmt.Errorf("verify current password: %w", err)
	}
	if !ok {
		return Session{}, ErrInvalidCredentials
	}
	if err := ValidatePassword(newPassword); err != nil {
		return Session{}, err
	}

	hash, err := HashPassword(newPassword, s.argon)
	if err != nil {
		return Session{}, fmt.Errorf("hash new password: %w", err)
	}
	if err := s.store.SetPasswordHash(ctx, userID, hash); err != nil {
		return Session{}, err
	}
	if err := s.store.DeleteSessionsForUser(ctx, userID); err != nil {
		return Session{}, err
	}
	if revokeTokens {
		if err := s.store.RevokeAllTokens(ctx, userID); err != nil {
			return Session{}, err
		}
	}

	return s.newSession(ctx, userID, meta)
}

// IssuedToken is a newly created device token. Raw is shown exactly once.
type IssuedToken struct {
	Raw    string
	Record Token
}

// CreateToken issues a device token whose embedded URL is serverURL.
func (s *Service) CreateToken(ctx context.Context, userID, name, serverURL, userAgent string) (IssuedToken, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return IssuedToken{}, fmt.Errorf("%w: name must be 1-100 characters", ErrInvalidTokenName)
	}

	raw, parsed, err := NewToken(serverURL)
	if err != nil {
		return IssuedToken{}, err
	}
	record := Token{
		ID:        parsed.ID,
		UserID:    userID,
		Name:      name,
		TokenHash: HashSecret(parsed.Secret),
		Prefix:    DisplayPrefix(raw),
		Scope:     "full",
		UserAgent: userAgent,
	}
	if err := s.store.CreateToken(ctx, record); err != nil {
		return IssuedToken{}, err
	}
	record.CreatedAt = s.now()
	return IssuedToken{Raw: raw, Record: record}, nil
}

// ListTokens returns active device tokens for the owner.
func (s *Service) ListTokens(ctx context.Context, userID string) ([]Token, error) {
	return s.store.Tokens(ctx, userID)
}

// RevokeToken revokes one device token.
func (s *Service) RevokeToken(ctx context.Context, userID, tokenID string) error {
	return s.store.RevokeToken(ctx, userID, tokenID)
}

// SessionFromRequest returns the raw session cookie value, or "".
func (s *Service) SessionFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// SetSessionCookies writes the session and CSRF cookies.
func (s *Service) SetSessionCookies(w http.ResponseWriter, session Session) {
	http.SetCookie(w, s.SessionCookie(session))
	http.SetCookie(w, s.CSRFCookie(session.CSRF))
}

// SessionCookie is the HttpOnly session cookie.
func (s *Service) SessionCookie(session Session) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    session.Token,
		Path:     "/",
		MaxAge:   int(s.sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

// CSRFCookie is the readable double-submit cookie.
func (s *Service) CSRFCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(s.sessionTTL.Seconds()),
		HttpOnly: false,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

// ClearCookies expires both cookies.
func (s *Service) ClearCookies(w http.ResponseWriter) {
	for _, name := range []string{SessionCookieName, CSRFCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: name == SessionCookieName,
			Secure:   s.cookieSecure,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

func (s *Service) newSession(ctx context.Context, userID string, meta RequestMeta) (Session, error) {
	idBytes := make([]byte, sessionIDBytes)
	if _, err := io.ReadFull(s.rand, idBytes); err != nil {
		return Session{}, fmt.Errorf("generate session id: %w", err)
	}
	csrfBytes := make([]byte, csrfTokenBytes)
	if _, err := io.ReadFull(s.rand, csrfBytes); err != nil {
		return Session{}, fmt.Errorf("generate csrf token: %w", err)
	}

	id := base64.RawURLEncoding.EncodeToString(idBytes)
	csrf := base64.RawURLEncoding.EncodeToString(csrfBytes)
	now := s.now()
	expires := now.Add(s.sessionTTL)

	if err := s.store.CreateSession(ctx, StoredSession{
		IDHash:     hashSessionID(id),
		UserID:     userID,
		CSRFToken:  csrf,
		LastSeenAt: now,
		ExpiresAt:  expires,
		UserAgent:  meta.UserAgent,
		IP:         meta.IP,
	}); err != nil {
		return Session{}, err
	}
	return Session{Token: id, CSRF: csrf, ExpiresAt: expires}, nil
}

func hashSessionID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
