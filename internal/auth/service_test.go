package auth

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/store"
)

func TestBootstrapLoginRotationAndPasswordChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	durable, err := store.Open(filepath.Join(t.TempDir(), "esgw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = durable.Close() })
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	service, err := New(durable, Config{
		StartedAt: now, Now: func() time.Time { return now }, PasswordParams: testPasswordParams,
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := service.BootstrapState(ctx)
	if err != nil || !state.Required {
		t.Fatalf("bootstrap state = %+v, %v", state, err)
	}
	first, err := service.Bootstrap(ctx, "admin", "long-enough-password", "127.0.0.1", "test agent")
	if err != nil || first.Token == "" || first.Username != "admin" {
		t.Fatalf("bootstrap session = %+v, %v", first, err)
	}
	if _, err := durable.SessionByTokenHash(ctx, first.Token); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("raw bearer token was stored: %v", err)
	}
	if _, err := service.Bootstrap(ctx, "other", "long-enough-password", "127.0.0.1", ""); !errors.Is(err, ErrBootstrapComplete) {
		t.Fatalf("second bootstrap = %v", err)
	}
	state, err = service.BootstrapState(ctx)
	if err != nil || state.Required {
		t.Fatalf("completed bootstrap state = %+v, %v", state, err)
	}

	if _, err := service.Login(ctx, "missing", "long-enough-password", "10.0.0.2", ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("missing login = %v", err)
	}
	second, err := service.Login(ctx, "admin", "long-enough-password", "10.0.0.2", "other")
	if err != nil || second.Token == "" {
		t.Fatalf("login = %+v, %v", second, err)
	}

	now = now.Add(13 * time.Hour)
	rotated, err := service.Authenticate(ctx, first.Token)
	if err != nil || rotated.Token == "" || rotated.Token == first.Token {
		t.Fatalf("rotated session = %+v, %v", rotated, err)
	}
	if _, err := service.Authenticate(ctx, first.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("old token auth = %v", err)
	}
	if err := service.ChangePassword(ctx, rotated.Token, "long-enough-password", "replacement-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, second.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("other session survived password change: %v", err)
	}
	if _, err := service.Login(ctx, "admin", "long-enough-password", "10.0.0.3", ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("old password login = %v", err)
	}
	if _, err := service.Login(ctx, "admin", "replacement-password", "10.0.0.3", ""); err != nil {
		t.Fatalf("new password login = %v", err)
	}
}

func TestBootstrapWindowAndLoginRateLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	durable, err := store.Open(filepath.Join(t.TempDir(), "esgw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = durable.Close() })
	now := time.Date(2026, 7, 22, 10, 0, 0, 0, time.UTC)
	service, err := New(durable, Config{StartedAt: now, Now: func() time.Time { return now }, PasswordParams: testPasswordParams})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Bootstrap(ctx, "bad name", "long-enough-password", "", ""); !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("invalid username = %v", err)
	}
	if _, err := service.Bootstrap(ctx, "admin", "short", "", ""); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("weak password = %v", err)
	}
	now = now.Add(31 * time.Minute)
	if _, err := service.Bootstrap(ctx, "admin", "long-enough-password", "", ""); !errors.Is(err, ErrBootstrapClosed) {
		t.Fatalf("expired bootstrap = %v", err)
	}

	// A fresh service reopens the setup window as documented after restart.
	service, err = New(durable, Config{StartedAt: now, Now: func() time.Time { return now }, PasswordParams: testPasswordParams})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Bootstrap(ctx, "admin", "long-enough-password", "", ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := service.Login(ctx, "admin", "wrong-password-long", "192.0.2.1", ""); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("failure %d = %v", i, err)
		}
	}
	if _, err := service.Login(ctx, "admin", "long-enough-password", "192.0.2.1", ""); err == nil {
		t.Fatal("account lock did not rate limit")
	} else {
		var limited RateLimitError
		if !errors.As(err, &limited) || limited.RetryAfter <= 0 {
			t.Fatalf("lock error = %T %v", err, err)
		}
	}
	now = now.Add(5*time.Minute + time.Second)
	if _, err := service.Login(ctx, "admin", "long-enough-password", "192.0.2.1", ""); err != nil {
		t.Fatalf("login after lock = %v", err)
	}
}

func TestExpiredSessionIsRevoked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	durable, err := store.Open(filepath.Join(t.TempDir(), "esgw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = durable.Close() })
	now := time.Now().UTC()
	service, err := New(durable, Config{
		StartedAt: now, Now: func() time.Time { return now }, IdleTTL: time.Hour,
		AbsoluteTTL: 2 * time.Hour, PasswordParams: testPasswordParams,
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := service.Bootstrap(ctx, "admin", "long-enough-password", "", "")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour + time.Second)
	if _, err := service.Authenticate(ctx, session.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired auth = %v", err)
	}
	if _, err := durable.SessionByTokenHash(ctx, tokenHash(session.Token)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired session retained: %v", err)
	}
}
