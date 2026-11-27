package clilogin

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/caleblewallen/lewnotes/server/internal/auth"
	"github.com/caleblewallen/lewnotes/server/internal/handoff"
	"github.com/caleblewallen/lewnotes/server/internal/index"
)

func testService(t *testing.T) (*Service, *auth.Service, *auth.User, func(time.Duration)) {
	t.Helper()
	ix, err := index.Open(filepath.Join(t.TempDir(), "lewnotes.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ix.Close() })

	authService, err := auth.NewService(auth.ServiceOptions{Store: auth.NewStore(ix.DB())})
	if err != nil {
		t.Fatal(err)
	}
	session, user, err := authService.Setup(context.Background(), "correct horse battery", "home", auth.RequestMeta{})
	if err != nil {
		t.Fatal(err)
	}
	_ = session

	now := time.Unix(1_800_000_000, 0).UTC()
	service, err := NewService(Options{
		DB:   ix.DB(),
		Auth: authService,
		Now:  func() time.Time { return now },
		TTL:  time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, authService, &user, func(d time.Duration) { now = now.Add(d) }
}

func TestLoginRequestLifecycle(t *testing.T) {
	ctx := context.Background()
	service, _, user, advance := testService(t)

	cliKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	req, err := service.Create(ctx, CreateParams{
		PublicKey:    cliKey.PublicKey().Bytes(),
		State:        "state-token-abcdefghijklmnopqrstuvwxyz",
		DeviceName:   "laptop",
		Client:       "lewnotes/test",
		RedirectURI:  "http://127.0.0.1:4321/cb",
		ServerOrigin: "https://notes.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Status != StatusPending || len(req.UserCode) != 8 {
		t.Fatalf("created = %+v", req)
	}

	if !service.VerifyState(req, "state-token-abcdefghijklmnopqrstuvwxyz") {
		t.Fatal("expected the state to verify")
	}
	if service.VerifyState(req, "wrong") {
		t.Fatal("expected a wrong state to fail")
	}

	if _, err := service.TakeHandoff(ctx, req.ID, "state-token-abcdefghijklmnopqrstuvwxyz"); !errors.Is(err, ErrNotPending) {
		t.Fatalf("take before approve: %v, want ErrNotPending", err)
	}

	approved, err := service.Approve(ctx, req.ID, user.ID, user.Username, "laptop")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != StatusApproved || approved.DeviceName != "laptop" {
		t.Fatalf("approved = %+v", approved)
	}

	blob, err := service.TakeHandoff(ctx, req.ID, "state-token-abcdefghijklmnopqrstuvwxyz")
	if err != nil {
		t.Fatalf("take handoff: %v", err)
	}
	payload, err := handoff.Decrypt(blob, cliKey, req.ID, "https://notes.example.com")
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if payload.Server != "https://notes.example.com" || payload.Username != "owner" {
		t.Fatalf("payload = %+v", payload)
	}
	if _, err := auth.ParseToken(payload.Token); err != nil {
		t.Fatalf("issued token is invalid: %v", err)
	}

	if _, err := service.TakeHandoff(ctx, req.ID, "state-token-abcdefghijklmnopqrstuvwxyz"); !errors.Is(err, ErrConsumed) {
		t.Fatalf("second take: %v, want ErrConsumed", err)
	}

	advance(48 * time.Hour)
	if err := service.Sweep(ctx, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, req.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after sweep: %v, want ErrNotFound", err)
	}
}

func TestLoginRequestDenyExpiryAndCodes(t *testing.T) {
	ctx := context.Background()
	service, _, user, advance := testService(t)

	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	makeRequest := func() Request {
		t.Helper()
		req, err := service.Create(ctx, CreateParams{
			PublicKey:    key.PublicKey().Bytes(),
			State:        "another-state-token-abcdefghijklmnop",
			DeviceName:   "desktop",
			Client:       "lewnotes/test",
			ServerOrigin: "https://notes.example.com",
		})
		if err != nil {
			t.Fatal(err)
		}
		return req
	}

	denied := makeRequest()
	if err := service.Deny(ctx, denied.ID, user.ID); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if err := service.Deny(ctx, denied.ID, user.ID); !errors.Is(err, ErrNotPending) {
		t.Fatalf("second deny: %v, want ErrNotPending", err)
	}

	coded := makeRequest()
	found, err := service.FindByUserCode(ctx, DisplayUserCode(coded.UserCode))
	if err != nil || found.ID != coded.ID {
		t.Fatalf("resolve code = %+v, %v", found, err)
	}
	lowered := "  " + DisplayUserCode(coded.UserCode)[:4] + "-" + DisplayUserCode(coded.UserCode)[5:]
	if found, err := service.FindByUserCode(ctx, lowered); err != nil || found.ID != coded.ID {
		t.Fatalf("resolve lowercase code = %+v, %v", found, err)
	}
	if _, err := service.FindByUserCode(ctx, "ZZZZ-ZZZZ"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("bad code: %v, want ErrInvalidCode", err)
	}

	expiring := makeRequest()
	advance(2 * time.Minute)
	stale, err := service.Get(ctx, expiring.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Status != StatusExpired {
		t.Fatalf("status = %q, want expired", stale.Status)
	}
	if _, err := service.Approve(ctx, expiring.ID, user.ID, user.Username, "desktop"); !errors.Is(err, ErrExpired) {
		t.Fatalf("approve expired: %v, want ErrExpired", err)
	}
	if _, err := service.FindByUserCode(ctx, DisplayUserCode(expiring.UserCode)); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("resolve expired code: %v, want ErrInvalidCode", err)
	}
}

func TestNormalizeUserCode(t *testing.T) {
	cases := map[string]string{
		"7k4m-9qrt": "7K4M9QRT",
		"7K4M 9QRT": "7K4M9QRT",
		"OIL1":      "0111",
	}
	for input, want := range cases {
		if got := NormalizeUserCode(input); got != want {
			t.Errorf("NormalizeUserCode(%q) = %q, want %q", input, got, want)
		}
	}
}
