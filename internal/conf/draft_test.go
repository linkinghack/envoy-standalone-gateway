package conf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const listenerYAML = `apiVersion: esgw/v1alpha1
kind: Listener
metadata:
  name: web
spec:
  address: 127.0.0.1:8080
  port: 8080
  protocol: HTTP
`

func TestLoadDraftHashAndMutualExclusion(t *testing.T) {
	data := t.TempDir()
	d, errs, err := LoadDraft(data)
	if err != nil || len(errs) != 0 || d.Mode != ModeAbstract {
		t.Fatalf("empty draft = %+v, errs=%v, err=%v", d, errs, err)
	}
	if err := os.WriteFile(filepath.Join(data, "config.d", "10-listener.yaml"), []byte(listenerYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	d, errs, err = LoadDraft(data)
	if err != nil || len(errs) != 0 || len(d.Config.Listeners) != 1 {
		t.Fatalf("loaded draft = %+v, errs=%v, err=%v", d, errs, err)
	}
	h1, err := DraftHash(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "config.d", "10-listener.yaml"), []byte(listenerYAML+"# changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h2, err := DraftHash(data)
	if err != nil || h1 == h2 {
		t.Fatalf("hashes = %q, %q, err=%v", h1, h2, err)
	}
	if err := os.WriteFile(filepath.Join(data, "native.yaml"), []byte("static_resources: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadDraft(data); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("mutual exclusion error = %v", err)
	}
}

func TestSnapshot(t *testing.T) {
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(data, "config.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(data, "config.d", "a.yaml")
	if err := os.WriteFile(src, []byte(listenerYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	dir, err := Snapshot(data, 7, SnapshotMeta{Seq: 7, Mode: ModeAbstract, State: "failed"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "000007" {
		t.Fatalf("snapshot dir = %s", dir)
	}
	got, err := os.ReadFile(filepath.Join(dir, "config", "config.d", "a.yaml"))
	if err != nil || string(got) != listenerYAML {
		t.Fatalf("snapshot source = %q, err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(data, 7, SnapshotMeta{Seq: 7}); err == nil {
		t.Fatal("expected duplicate snapshot error")
	}
}
