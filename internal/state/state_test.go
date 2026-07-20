package state

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestServiceTimesOutVersionConfirmation(t *testing.T) {
	service := New("node-1", fakeAdmin{responses: map[string][]byte{
		"/server_info":             []byte(`{"version":"1.30","state":"LIVE"}`),
		"/config_dump?include_eds": []byte(`{"configs":[{"version_info":"old"}]}`),
	}})
	events := make(chan VersionConfirmEvent, 1)
	cancel := service.SubscribeConfirm(events)
	defer cancel()
	service.ExpectVersion("new", time.Millisecond)
	time.Sleep(2 * time.Millisecond)
	state, err := service.CurrentState(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version.Status != "TIMEOUT" {
		t.Fatalf("state=%+v", state.Version)
	}
	select {
	case event := <-events:
		if event.Status != "TIMEOUT" || event.Expected != "new" {
			t.Fatalf("event=%+v", event)
		}
	default:
		t.Fatal("missing timeout event")
	}
}

func TestConfigDumpVersionMismatch(t *testing.T) {
	got, resources := configDumpVersions([]byte(`{"configs":[{"version_info":"a"},{"version_info":"b"}]}`))
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") || len(resources) != 2 {
		t.Fatalf("got=%q resources=%+v", got, resources)
	}
}

func TestServiceParsesListenersAndClusters(t *testing.T) {
	service := New("node-1", fakeAdmin{responses: map[string][]byte{
		"/server_info":             []byte(`{"version":"1.30","state":"LIVE","uptime_current_epoch":"12","hot_restart_epoch":2}`),
		"/config_dump?include_eds": []byte(`{"configs":[{"name":"lis/web","address":{"socket_address":{"address":"0.0.0.0","port_value":443}}}]}`),
		"/clusters?format=json":    []byte(`{"cluster_statuses":[{"name":"us/api","host_statuses":[{"address":{"socket_address":{"address":"10.0.0.7","port_value":8080}},"health_status":"healthy","weight":3},{"address":{"socket_address":{"address":"10.0.0.8","port_value":8080}},"health_status":"failed_active_health_check"}]}]}`),
	}})
	state, err := service.CurrentState(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Listeners) != 1 || state.Listeners[0].Address != "0.0.0.0:443" {
		t.Fatalf("listeners=%+v", state.Listeners)
	}
	if got := state.Listeners[0].Owner; got == nil || got.Kind != "Listener" || got.Name != "web" {
		t.Fatalf("listener owner=%+v", got)
	}
	if len(state.Clusters) != 1 || len(state.Clusters[0].Endpoints) != 2 {
		t.Fatalf("clusters=%+v", state.Clusters)
	}
	if got := state.Clusters[0].Endpoints[0]; got.Address != "10.0.0.7" || got.Port != 8080 || got.Health != "HEALTHY" || got.Weight != 3 {
		t.Fatalf("endpoint=%+v", got)
	}
	if got := state.Clusters[0].Endpoints[1]; got.Health != "UNHEALTHY" {
		t.Fatalf("failed endpoint=%+v", got)
	}
}

func TestSeriesStoreRingAndCardinality(t *testing.T) {
	store := NewSeriesStore(2, 1, time.Second)
	key := SeriesKey{Dim: "listener", Name: "lis/web", Metric: "rq_total"}
	base := time.Unix(100, 0)
	for i := int64(1); i <= 3; i++ {
		if err := store.Add(key, i, base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	series := store.Snapshot()
	if len(series) != 1 || series[0].Filled != 2 || !reflect.DeepEqual(series[0].Values, []int64{3, 2}) {
		t.Fatalf("series=%+v", series)
	}
	if err := store.Add(SeriesKey{Name: "another"}, 1, base); err == nil {
		t.Fatal("expected cardinality limit error")
	}
}

func TestHTTPClientReadOnlyEndpointsAndPrometheus(t *testing.T) {
	requests := make(chan string, 2)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.RequestURI()
		if r.URL.Path == "/stats/prometheus" {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "envoy_requests_total 1\n")
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	server.Start()
	defer server.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	client := &HTTPClient{Address: server.Listener.Addr().String(), Client: &http.Client{Transport: transport}}
	body, err := client.Get(context.Background(), "/ready")
	if err != nil || string(body) != `{"ok":true}` {
		t.Fatalf("body=%s err=%v", body, err)
	}
	var out strings.Builder
	if err := client.Prometheus(context.Background(), &out); err != nil || out.String() != "envoy_requests_total 1\n" {
		t.Fatalf("prometheus=%q err=%v", out.String(), err)
	}
	close(requests)
	var got []string
	for path := range requests {
		got = append(got, path)
	}
	if !reflect.DeepEqual(got, []string{"/ready", "/stats/prometheus"}) {
		t.Fatalf("requests=%v", got)
	}
	if _, err := client.Get(context.Background(), "/quitquitquit"); err == nil {
		t.Fatal("expected write path rejection")
	}
}

func TestHTTPClientErrorsAndDecodeJSON(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	server.Listener = ln
	server.Start()
	defer server.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", ln.Addr().String())
	}}
	client := &HTTPClient{Address: ln.Addr().String(), Client: &http.Client{Transport: transport}}
	if _, err := client.Get(context.Background(), "/ready"); err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("err=%v", err)
	}
	if _, err := DecodeJSON[map[string]string]([]byte("{")); err == nil {
		t.Fatal("expected JSON decode error")
	}
	if _, err := (&HTTPClient{}).Get(context.Background(), "/ready"); err == nil {
		t.Fatal("expected empty address error")
	}
	if _, err := (&HTTPClient{Address: "bad-address"}).Get(context.Background(), "/ready"); err == nil {
		t.Fatal("expected invalid address error")
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
