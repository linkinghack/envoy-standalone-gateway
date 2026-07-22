package proc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordStoreRoundTrip(t *testing.T) {
	store := RecordStore{Path: filepath.Join(t.TempDir(), "run", "proc.json")}
	if _, err := store.Load(); !errors.Is(err, ErrNoRecord) {
		t.Fatalf("empty Load() = %v", err)
	}
	want := Record{PID: 123, BaseID: 4, Epoch: 2, ConfigPath: "/tmp/envoy.yaml", BinaryPath: "/bin/envoy", StartedAt: time.Unix(42, 0).UTC(), EnvoyVersion: "1.39.0", State: "running"}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil || got != want {
		t.Fatalf("Load() = %+v, %v; want %+v", got, err, want)
	}
	info, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("record mode = %o", info.Mode().Perm())
	}
}

func TestRecordStoreRejectsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proc.json")
	if err := os.WriteFile(path, []byte(`{"pid":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (RecordStore{Path: path}).Load(); err == nil {
		t.Fatal("want invalid record error")
	}
}
