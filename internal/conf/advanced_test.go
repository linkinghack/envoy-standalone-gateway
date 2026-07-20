package conf

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

func TestDiffSnapshotsAndRollbackSource(t *testing.T) {
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(data, "config.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(data, "config.d", "a.yaml")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(data, 1, SnapshotMeta{Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(data, 2, SnapshotMeta{Seq: 2}); err != nil {
		t.Fatal(err)
	}
	diff, err := DiffSnapshots(data, 1, 2)
	if err != nil || len(diff.Files) != 1 || diff.Files[0].Status != "changed" {
		t.Fatalf("diff=%+v err=%v", diff, err)
	}
	if !strings.Contains(diff.Files[0].Patch, "- one") || !strings.Contains(diff.Files[0].Patch, "+ two") {
		t.Fatalf("patch=%q", diff.Files[0].Patch)
	}
	if err := RollbackSource(data, 1, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "one\n" {
		t.Fatalf("rollback content=%q err=%v", got, err)
	}
}

func TestDraftWatcher(t *testing.T) {
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(data, "config.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "config.d", "a.yaml"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes, errs := (DraftWatcher{DataDir: data, Debounce: 10 * time.Millisecond}).Watch(ctx)
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(data, "config.d", "a.yaml"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case hash := <-changes:
		if hash == "" {
			t.Fatal("empty changed hash")
		}
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("watcher did not report change")
	}
}

func TestParseNativeStrict(t *testing.T) {
	ir, err := ParseNative([]byte(`static_resources:
  clusters:
  - name: foo
    type: STATIC
    load_assignment:
      cluster_name: foo
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: 127.0.0.1
                port_value: 8080
`))
	if err != nil {
		t.Fatal(err)
	}
	if ir.Version == "" || ir.Clusters["foo"] == nil || ir.Endpoints["foo"] == nil {
		t.Fatalf("native IR=%+v", ir)
	}
	if _, err := ParseNative([]byte("static_resources: {}\nunknown_field: true\n")); err == nil {
		t.Fatal("expected strict unknown-field error")
	}
}

func TestRollbackSourceRequiresForceForExistingDraft(t *testing.T) {
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(data, "config.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(data, "config.d", "a.yaml")
	if err := os.WriteFile(path, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(data, 1, SnapshotMeta{Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RollbackSource(data, 1, false); err == nil {
		t.Fatal("expected force guard")
	}
	if err := RollbackSource(data, 1, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "one\n" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestDraftDocumentCRUD(t *testing.T) {
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(data, "config.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(data, "config.d", "objects.yaml")
	body := "apiVersion: esgw/v1alpha1\nkind: Listener\nmetadata:\n  name: one\nspec:\n  address: 127.0.0.1:8080\n  port: 8080\n  protocol: HTTP\n---\napiVersion: esgw/v1alpha1\nkind: Listener\nmetadata:\n  name: two\nspec:\n  address: 127.0.0.1:8081\n  port: 8081\n  protocol: HTTP\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	origin := protocol.Origin{File: path, DocIndex: 1}
	replacement := []byte("apiVersion: esgw/v1alpha1\nkind: Listener\nmetadata:\n  name: three\nspec:\n  address: 127.0.0.1:8082\n  port: 8082\n  protocol: HTTP\n")
	if err := WriteDraftDocument(data, origin, replacement); err != nil {
		t.Fatal(err)
	}
	draft, errs, err := LoadDraft(data)
	if err != nil || len(errs) != 0 || len(draft.Config.Listeners) != 2 || draft.Config.Listeners[1].Metadata.Name != "three" {
		t.Fatalf("draft=%+v errs=%v err=%v", draft, errs, err)
	}
	if err := DeleteDraftDocument(data, protocol.Origin{File: path, DocIndex: 0}); err != nil {
		t.Fatal(err)
	}
	draft, errs, err = LoadDraft(data)
	if err != nil || len(errs) != 0 || len(draft.Config.Listeners) != 1 || draft.Config.Listeners[0].Metadata.Name != "three" {
		t.Fatalf("after delete draft=%+v errs=%v err=%v", draft, errs, err)
	}
}
