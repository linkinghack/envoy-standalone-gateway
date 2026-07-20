package conf

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"github.com/linkinghack/envoy-standalone-gateway/internal/compile"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver"
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
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
