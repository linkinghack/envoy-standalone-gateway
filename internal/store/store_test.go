package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenMigrationSettingsAndVersions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "esgw.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.SetSetting(ctx, "draftHash", "abc"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetSetting(ctx, "draftHash")
	if err != nil || !ok || got != "abc" {
		t.Fatalf("setting = %q, %v, %v", got, ok, err)
	}
	seq, err := s.NextVersionSeq(ctx)
	if err != nil || seq != 1 {
		t.Fatalf("first seq = %d, %v", seq, err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	want := Version{Seq: seq, CreatedAt: now, Author: "test", Mode: "xds", IRVersion: "ir1", State: "effective"}
	if err := s.InsertVersion(ctx, want); err != nil {
		t.Fatal(err)
	}
	gotVersion, err := s.GetVersion(ctx, seq)
	if err != nil {
		t.Fatal(err)
	}
	if gotVersion.Author != want.Author || gotVersion.IRVersion != want.IRVersion || gotVersion.State != want.State {
		t.Fatalf("version = %+v, want %+v", gotVersion, want)
	}
	seq2, err := s.NextVersionSeq(ctx)
	if err != nil || seq2 != 2 {
		t.Fatalf("second seq = %d, %v", seq2, err)
	}
	// Re-opening applies migrations idempotently and preserves state.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	got, ok, err = s.GetSetting(ctx, "draftHash")
	if err != nil || !ok || got != "abc" {
		t.Fatalf("reopen setting = %q, %v, %v", got, ok, err)
	}
}

func TestActivePublishUniqueIndex(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "esgw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Exec(`INSERT INTO publish_runs(state,created_at,updated_at) VALUES('VALIDATING',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO publish_runs(state,created_at,updated_at) VALUES('CONFIRMING',?,?)`, now, now); err == nil {
		t.Fatal("expected active publish uniqueness error")
	}
	if _, err := s.db.Exec(`INSERT INTO publish_runs(state,created_at,updated_at) VALUES('EFFECTIVE',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM publish_runs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("publish rows = %d", n)
	}
}

func TestPublishRunLifecycle(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "esgw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	id, err := s.CreatePublishRun(ctx, PublishRun{TriggerBy: "alice", BaseHash: "h1", State: "VALIDATING"})
	if err != nil || id == 0 {
		t.Fatalf("create run id=%d err=%v", id, err)
	}
	got, err := s.GetPublishRun(ctx, id)
	if err != nil || got.TriggerBy != "alice" || got.State != "VALIDATING" {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	got.State = "EFFECTIVE"
	got.VersionSeq = 3
	if err := s.UpdatePublishRun(ctx, got); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetPublishRun(ctx, id)
	if err != nil || got.State != "EFFECTIVE" || got.VersionSeq != 3 {
		t.Fatalf("updated run=%+v err=%v", got, err)
	}
}
