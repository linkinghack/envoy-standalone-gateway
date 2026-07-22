package static_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linkinghack/envoy-standalone-gateway/internal/compile"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver/static"
)

type fakeRestarter struct {
	calls int
	err   error
}

func (r *fakeRestarter) HotRestart(context.Context) error { r.calls++; return r.err }

func TestStaticServerFileOnlyAlwaysRepairsOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "envoy.yaml")
	server := static.NewServer(static.Writer{OutputPath: path}, static.RenderOptions{AdminSocketPath: "/run/esgw/admin.sock"}, nil, nil)
	input := compileMinimal(t, compile.ModeStatic)
	if err := server.Apply(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("externally damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := server.Apply(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	payload, _ := os.ReadFile(path)
	if !strings.Contains(string(payload), input.Version) || !strings.Contains(string(payload), "/run/esgw/admin.sock") {
		t.Fatalf("repaired payload:\n%s", payload)
	}
	if status := server.Status(); status.Phase != deliver.PhaseAwaitingConfirm || status.Version != input.Version {
		t.Fatalf("status = %+v", status)
	}
}

func TestStaticServerRestartFailureRestoresLastGood(t *testing.T) {
	path := filepath.Join(t.TempDir(), "envoy.yaml")
	if err := os.WriteFile(path, []byte("last-good\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	restarter := &fakeRestarter{err: errors.New("epoch did not become LIVE")}
	server := static.NewServer(static.Writer{OutputPath: path}, static.RenderOptions{}, restarter, nil)
	input := compileMinimal(t, compile.ModeStatic)
	events, cancel := server.Events()
	defer cancel()
	if err := server.Apply(context.Background(), input); err == nil || !strings.Contains(err.Error(), "stage=hot_restart") {
		t.Fatalf("Apply() error = %v", err)
	}
	payload, _ := os.ReadFile(path)
	if string(payload) != "last-good\n" {
		t.Fatalf("restored payload = %q", payload)
	}
	if event := <-events; event.Kind != deliver.EventHotRestartFailed {
		t.Fatalf("event = %+v", event)
	}
}
