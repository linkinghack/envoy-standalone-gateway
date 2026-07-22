package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestAuthStoreLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "esgw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	count, err := s.UserCount(ctx)
	if err != nil || count != 0 {
		t.Fatalf("initial user count = %d, %v", count, err)
	}
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	user, err := s.CreateUser(ctx, User{Username: "admin", PasswordHash: "phc", CreatedAt: now})
	if err != nil || user.ID == 0 {
		t.Fatalf("create user = %+v, %v", user, err)
	}
	if _, err := s.CreateUser(ctx, User{Username: "admin", PasswordHash: "other"}); err == nil {
		t.Fatal("duplicate username succeeded")
	}
	loaded, err := s.UserByUsername(ctx, "admin")
	if err != nil || loaded.PasswordHash != "phc" || !loaded.PasswordUpdatedAt.Equal(now) {
		t.Fatalf("loaded user = %+v, %v", loaded, err)
	}

	abs := now.Add(7 * 24 * time.Hour)
	first := Session{TokenHash: "hash-1", UserID: user.ID, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour), AbsoluteExpiresAt: abs, IP: "127.0.0.1", UserAgent: "test"}
	if err := s.CreateSession(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, Session{TokenHash: "hash-2", UserID: user.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: abs}); err != nil {
		t.Fatal(err)
	}
	got, err := s.SessionByTokenHash(ctx, "hash-1")
	if err != nil || got.Username != "admin" || !got.LastActiveAt.Equal(now) || got.TokenHash != "hash-1" {
		t.Fatalf("session = %+v, %v", got, err)
	}
	rotatedAt := now.Add(13 * time.Hour)
	if err := s.RotateSession(ctx, "hash-1", "hash-3", rotatedAt, now.Add(37*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionByTokenHash(ctx, "hash-1"); err != sql.ErrNoRows {
		t.Fatalf("old token error = %v, want sql.ErrNoRows", err)
	}
	if err := s.UpdatePasswordAndRevokeSessions(ctx, user.ID, "new-phc", "hash-3", rotatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionByTokenHash(ctx, "hash-2"); err != sql.ErrNoRows {
		t.Fatalf("other session error = %v, want sql.ErrNoRows", err)
	}
	got, err = s.SessionByTokenHash(ctx, "hash-3")
	if err != nil {
		t.Fatalf("kept session: %v", err)
	}
	loaded, err = s.UserByUsername(ctx, "admin")
	if err != nil || loaded.PasswordHash != "new-phc" {
		t.Fatalf("updated user = %+v, %v", loaded, err)
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "esgw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	user, err := s.CreateUser(ctx, User{Username: "admin", PasswordHash: "phc"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sessions := []Session{
		{TokenHash: "idle-expired", UserID: user.ID, CreatedAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-time.Second), AbsoluteExpiresAt: now.Add(5 * 24 * time.Hour)},
		{TokenHash: "absolute-expired", UserID: user.ID, CreatedAt: now.Add(-8 * 24 * time.Hour), ExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(-time.Second)},
		{TokenHash: "active", UserID: user.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(7 * 24 * time.Hour)},
	}
	for _, session := range sessions {
		if err := s.CreateSession(ctx, session); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := s.DeleteExpiredSessions(ctx, now)
	if err != nil || deleted != 2 {
		t.Fatalf("deleted = %d, %v", deleted, err)
	}
	if _, err := s.SessionByTokenHash(ctx, "active"); err != nil {
		t.Fatalf("active session removed: %v", err)
	}
}
