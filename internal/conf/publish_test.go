package conf

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"github.com/linkinghack/envoy-standalone-gateway/internal/compile"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver"
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/state"
	"github.com/linkinghack/envoy-standalone-gateway/internal/store"
)

type fakeDeliver struct {
	err   error
	apply int
}

func (f *fakeDeliver) Apply(context.Context, *ir.IR) error { f.apply++; return f.err }
func (f *fakeDeliver) UpdateEndpoints(context.Context, map[string]*endpointv3.ClusterLoadAssignment) error {
	return deliver.ErrEndpointsUnsupported
}
func (f *fakeDeliver) Status() deliver.Status { return deliver.Status{} }
func (f *fakeDeliver) Events() (<-chan deliver.Event, func()) {
	ch := make(chan deliver.Event)
	return ch, func() { close(ch) }
}

func TestPublisherPublishAndFailure(t *testing.T) {
	for _, wantErr := range []error{nil, errors.New("downstream unavailable")} {
		t.Run(func() string {
			if wantErr != nil {
				return "failure"
			}
			return "success"
		}(), func(t *testing.T) {
			data := t.TempDir()
			if err := writeMinimalDraft(data); err != nil {
				t.Fatal(err)
			}
			st, err := store.Open(filepath.Join(data, "esgw.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			fd := &fakeDeliver{err: wantErr}
			res, err := (&Publisher{DataDir: data, Store: st, Deliver: fd, Mode: compile.ModeXDS}).Publish(context.Background(), "test", "initial")
			if (err != nil) != (wantErr != nil) {
				t.Fatalf("Publish err=%v, wantErr=%v", err, wantErr)
			}
			if fd.apply != 1 || res.Seq != 1 {
				t.Fatalf("result=%+v apply=%d", res, fd.apply)
			}
			v, err := st.GetVersion(context.Background(), 1)
			if err != nil {
				t.Fatal(err)
			}
			wantState := "effective"
			if wantErr != nil {
				wantState = "failed"
			}
			if v.State != wantState {
				t.Fatalf("state=%s want %s", v.State, wantState)
			}
		})
	}
}

func TestPublisherPublishWithBaseAndConfirm(t *testing.T) {
	data := t.TempDir()
	if err := writeMinimalDraft(data); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(data, "esgw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	hash, err := DraftHash(data)
	if err != nil {
		t.Fatal(err)
	}
	pub := &Publisher{DataDir: data, Store: st, Deliver: &fakeDeliver{}, Mode: compile.ModeXDS}
	res, err := pub.PublishWithBase(context.Background(), "test", "confirm me", hash)
	if err != nil {
		t.Fatal(err)
	}
	if res.State != "confirming" || res.RunID == 0 {
		t.Fatalf("result=%+v", res)
	}
	if err := pub.Confirm(context.Background(), res.RunID, res.IRVersion); err != nil {
		t.Fatal(err)
	}
	run, err := st.GetPublishRun(context.Background(), res.RunID)
	if err != nil || run.State != "EFFECTIVE" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
}

func TestPublisherAutoConfirmsFromStateEvent(t *testing.T) {
	data := t.TempDir()
	if err := writeMinimalDraft(data); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(data, "esgw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	responses := map[string][]byte{
		"/server_info":             []byte(`{"version":"1.30","state":"LIVE"}`),
		"/config_dump?include_eds": []byte(`{"configs":[{"version_info":"old"}]}`),
		"/clusters?format=json":    []byte(`{"cluster_statuses":[]}`),
		"/certs":                   []byte(`{"certificates":[]}`),
	}
	admin := &fakeStateAdmin{responses: responses}
	stateService := state.New("node-1", admin)
	pub := &Publisher{DataDir: data, Store: st, Deliver: &fakeDeliver{}, Mode: compile.ModeXDS}
	stop := pub.AttachState(stateService)
	defer stop()
	res, err := pub.PublishWithBase(context.Background(), "test", "auto-confirm", "")
	if err != nil {
		t.Fatal(err)
	}
	admin.set("/config_dump?include_eds", []byte(`{"configs":[{"version_info":"`+res.IRVersion+`"}]}`))
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		run, getErr := st.GetPublishRun(context.Background(), res.RunID)
		if getErr == nil && run.State == "EFFECTIVE" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	run, _ := st.GetPublishRun(context.Background(), res.RunID)
	t.Fatalf("run did not auto-confirm: %+v", run)
}

type fakeStateAdmin struct {
	mu        sync.RWMutex
	responses map[string][]byte
}

func (f *fakeStateAdmin) Get(_ context.Context, path string) ([]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.responses[path], nil
}

func (f *fakeStateAdmin) Prometheus(_ context.Context, w io.Writer) error {
	_, err := io.WriteString(w, "")
	return err
}

func (f *fakeStateAdmin) set(path string, body []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[path] = body
}

func TestPublisherBaseHashConflict(t *testing.T) {
	data := t.TempDir()
	if err := writeMinimalDraft(data); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(data, "esgw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	pub := &Publisher{DataDir: data, Store: st, Deliver: &fakeDeliver{}, Mode: compile.ModeXDS}
	_, err = pub.PublishWithBase(context.Background(), "test", "conflict", "stale")
	if !errors.Is(err, ErrDraftChanged) {
		t.Fatalf("err=%v, want ErrDraftChanged", err)
	}
}

func writeMinimalDraft(data string) error {
	if err := os.MkdirAll(filepath.Join(data, "config.d"), 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(data, "config.d", "listener.yaml"), []byte(`apiVersion: esgw/v1alpha1
kind: Listener
metadata:
  name: web
spec:
  address: 127.0.0.1:8080
  port: 8080
  protocol: HTTP
`), 0o600)
}
