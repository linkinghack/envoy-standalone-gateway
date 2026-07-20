package state

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

type fakeAdmin struct {
	responses map[string][]byte
}

func (f fakeAdmin) Get(_ context.Context, path string) ([]byte, error) {
	return f.responses[path], nil
}

func (f fakeAdmin) Prometheus(_ context.Context, w io.Writer) error {
	_, err := io.WriteString(w, "envoy_requests_total 1\n")
	return err
}

func TestHTTPClientRejectsWritePaths(t *testing.T) {
	client := &HTTPClient{Address: "127.0.0.1:9901"}
	if _, err := client.Get(context.Background(), "/quitquitquit"); err == nil {
		t.Fatal("expected write path rejection")
	}
}

func TestServiceConfirmsConfigDumpVersion(t *testing.T) {
	service := New("node-1", fakeAdmin{responses: map[string][]byte{
		"/server_info":             []byte(`{"version":"1.30","state":"LIVE","hot_restart_epoch":2}`),
		"/config_dump?include_eds": []byte(`{"configs":[{"version_info":"abc123"},{"version_info":"abc123"}]}`),
	}})
	events := make(chan VersionConfirmEvent, 1)
	cancel := service.SubscribeConfirm(events)
	defer cancel()
	service.ExpectVersion("abc123", time.Second)
	state, err := service.CurrentState(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version.Status != "CONFIRMED" || state.Version.Observed != "abc123" {
		t.Fatalf("state=%+v", state.Version)
	}
	select {
	case event := <-events:
		if event.Status != "CONFIRMED" || event.Observed != "abc123" {
			t.Fatalf("event=%+v", event)
		}
	default:
		t.Fatal("missing confirmation event")
	}
}

func TestConfigDumpVersionMismatch(t *testing.T) {
	got, resources := configDumpVersions([]byte(`{"configs":[{"version_info":"a"},{"version_info":"b"}]}`))
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") || len(resources) != 2 {
		t.Fatalf("got=%q resources=%+v", got, resources)
	}
}

func TestServiceRetainsStaleSnapshot(t *testing.T) {
	service := New("node-1", failingAdmin{})
	service.mu.Lock()
	service.current = DataPlaneState{NodeID: "node-1", LastSuccessAt: time.Now()}
	service.mu.Unlock()
	state, err := service.CurrentState(context.Background(), true)
	if err == nil || !state.Stale || state.LastSuccessAt.IsZero() {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

type failingAdmin struct{}

func (failingAdmin) Get(context.Context, string) ([]byte, error) { return nil, io.ErrUnexpectedEOF }
func (failingAdmin) Prometheus(context.Context, io.Writer) error { return io.ErrUnexpectedEOF }
