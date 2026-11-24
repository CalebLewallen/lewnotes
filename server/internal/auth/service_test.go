package auth

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caleblewallen/lewnotes/server/internal/index"
)

type serviceClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *serviceClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *serviceClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestService(t *testing.T) (*Service, *serviceClock) {
	t.Helper()

	ix, err := index.Open(filepath.Join(t.TempDir(), "lewnotes.db"))
	if err != nil {
		t.Fatalf("index.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = ix.Close() })

	clock := &serviceClock{now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	service, err := NewService(ServiceOptions{
		Store:      NewStore(ix.DB()),
		SessionTTL: 30 * 24 * time.Hour,
		Argon2:     testParams,
		Now:        clock.Now,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service, clock
}

func mustSetup(t *testing.T, service *Service) (Session, User) {
	t.Helper()
	session, user, err := service.Setup(context.Background(), "correct horse battery staple", "home", RequestMeta{UserAgent: "test", IP: "127.0.0.1"})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	return session, user
}

func TestSetupAndLogin(t *testing.T) {
	ctx := context.Background()
	service, clock := newTestService(t)

	required, err := service.SetupRequired(ctx)
	if err != nil || !required {
		t.Fatalf("SetupRequired() = %v, %v; want true, nil", required, err)
	}

	session, user := mustSetup(t, service)
	if user.Username != "owner" || user.InstanceName != "home" {
		t.Errorf("user = %+v", user)
	}
	if required, _ := service.SetupRequired(ctx); required {
		t.Error("SetupRequired() = true after setup")
	}
	if name, _ := service.InstanceName(ctx); name != "home" {
		t.Errorf("InstanceName() = %q, want home", name)
	}

	if _, _, err := service.Setup(ctx, "another password 1", "x", RequestMeta{}); !errors.Is(err, ErrAlreadySetup) {
		t.Errorf("second Setup() error = %v, want ErrAlreadySetup", err)
	}
	if _, _, err := service.Setup(ctx, "short", "x", RequestMeta{}); err == nil {
		t.Error("Setup(short password) = nil, want policy error")
	}

	if _, _, err := service.Login(ctx, "wrong password", RequestMeta{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login(wrong) error = %v, want ErrInvalidCredentials", err)
	}

	principal, err := service.Authenticate(ctx, session.Token, "")
	if err != nil {
		t.Fatalf("Authenticate(session) error = %v", err)
	}
	if principal.Kind != "session" || principal.CSRF != session.CSRF || principal.UserID != user.ID {
		t.Errorf("principal = %+v", principal)
	}

	clock.Advance(31 * 24 * time.Hour)
	if _, err := service.Authenticate(ctx, session.Token, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("Authenticate(expired) error = %v, want ErrUnauthenticated", err)
	}
}

func TestConcurrentSetupCreatesOneUser(t *testing.T) {
	ctx := context.Background()
	service, _ := newTestService(t)

	const attempts = 8
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		successes int
	)
	errs := make(chan error, attempts)

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := service.Setup(ctx, "correct horse battery staple", "home", RequestMeta{})
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
				return
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	if successes != 1 {
		t.Fatalf("successful setups = %d, want 1", successes)
	}
	for err := range errs {
		if !errors.Is(err, ErrAlreadySetup) {
			t.Errorf("loser error = %v, want ErrAlreadySetup", err)
		}
	}
}

func TestDeviceTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	service, clock := newTestService(t)
	_, user := mustSetup(t, service)

	issued, err := service.CreateToken(ctx, user.ID, "caleb-laptop", "https://notes.example.com", "go-test")
	if err != nil {
		t.Fatalf("CreateToken() error = %v", err)
	}
	if issued.Record.TokenHash == "" || issued.Record.Prefix == "" {
		t.Errorf("record = %+v, want hash and prefix", issued.Record)
	}
	if _, err := ParseToken(issued.Raw); err != nil {
		t.Fatalf("issued token does not parse: %v", err)
	}

	principal, err := service.Authenticate(ctx, "", issued.Raw)
	if err != nil {
		t.Fatalf("Authenticate(token) error = %v", err)
	}
	if principal.Kind != "token" || principal.TokenID != issued.Record.ID || principal.Username != "owner" {
		t.Errorf("principal = %+v", principal)
	}

	clock.Advance(2 * time.Hour)
	if _, err := service.Authenticate(ctx, "", issued.Raw); err != nil {
		t.Errorf("Authenticate token after last-used interval error = %v", err)
	}
	tokens, err := service.ListTokens(ctx, user.ID)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("ListTokens() = %d, %v; want 1", len(tokens), err)
	}
	if tokens[0].LastUsedAt == nil {
		t.Error("last_used_at was not recorded")
	}

	tampered := issued.Raw[:len(issued.Raw)-1] + "A"
	if issued.Raw[len(issued.Raw)-1] == 'A' {
		tampered = issued.Raw[:len(issued.Raw)-1] + "B"
	}
	if _, err := service.Authenticate(ctx, "", tampered); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("Authenticate(tampered) error = %v, want ErrUnauthenticated", err)
	}

	if err := service.RevokeToken(ctx, user.ID, issued.Record.ID); err != nil {
		t.Fatalf("RevokeToken() error = %v", err)
	}
	if _, err := service.Authenticate(ctx, "", issued.Raw); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("Authenticate(revoked) error = %v, want ErrUnauthenticated", err)
	}
	if tokens, _ := service.ListTokens(ctx, user.ID); len(tokens) != 0 {
		t.Errorf("ListTokens() after revoke = %d, want 0", len(tokens))
	}
	if err := service.RevokeToken(ctx, user.ID, issued.Record.ID); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("double revoke error = %v, want ErrTokenNotFound", err)
	}
}

func TestLogoutAndPasswordChangeRevokeSessions(t *testing.T) {
	ctx := context.Background()
	service, _ := newTestService(t)
	session, user := mustSetup(t, service)

	second, _, err := service.Login(ctx, "correct horse battery staple", RequestMeta{})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	if err := service.Logout(ctx, session.Token); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, err := service.Authenticate(ctx, session.Token, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("Authenticate(logged out) error = %v, want ErrUnauthenticated", err)
	}

	rotated, err := service.ChangePassword(ctx, user.ID, "correct horse battery staple", "another good password", false, RequestMeta{})
	if err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	if _, err := service.Authenticate(ctx, second.Token, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("Authenticate(other session) error = %v, want ErrUnauthenticated", err)
	}
	if _, err := service.Authenticate(ctx, rotated.Token, ""); err != nil {
		t.Errorf("Authenticate(new session) error = %v", err)
	}
	if _, _, err := service.Login(ctx, "correct horse battery staple", RequestMeta{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("Login(old password) error = %v, want ErrInvalidCredentials", err)
	}
	if _, _, err := service.Login(ctx, "another good password", RequestMeta{}); err != nil {
		t.Errorf("Login(new password) error = %v", err)
	}
	if _, err := service.ChangePassword(ctx, user.ID, "wrong current", "another good password", false, RequestMeta{}); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("ChangePassword(wrong current) error = %v, want ErrInvalidCredentials", err)
	}
}
