package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/state"
)

func TestStateStatsSystemAndPrometheusHTTP(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	reader := &fakeStateReader{current: &state.DataPlaneState{
		NodeID: "esgw", Ready: true, CollectedAt: now, Envoy: state.ServerInfo{Version: "1.39.0", State: "LIVE"},
		Listeners: []state.ListenerState{{Name: "web"}},
		Clusters:  []state.ClusterState{{Name: "backend", Endpoints: []state.EndpointState{{Address: "127.0.0.1", Port: 9000, Health: "HEALTHY"}}}},
		Routes:    []state.RouteState{{Name: "main"}}, Certs: []state.CertState{{Name: "api"}},
	}, series: []state.Series{{
		Key: state.SeriesKey{Dim: "cluster", Name: "backend", Metric: "requests"}, Interval: time.Minute,
		Values: []int64{4, 5, 2, 3}, Head: 2, Filled: 4,
	}}}
	stateAPI := &StateAPI{
		State: reader, Mode: "xds", Topology: "sidecar",
		Prometheus: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("envoy_requests_total 2\n")) }),
	}
	server := newTestServer(t, nil, nil, stateAPI.Handlers())
	cookie := bootstrapCookie(t, server)

	response := apiRequest(t, server, http.MethodGet, "/api/v1/status/summary", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"endpoints":1`) || !strings.Contains(response.Body.String(), `"ready":true`) {
		t.Fatalf("summary = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodGet, "/api/v1/status/clusters/backend/endpoints", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"HEALTHY"`) {
		t.Fatalf("endpoints = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodGet, "/api/v1/status/clusters/missing/endpoints", nil, cookie)
	assertAPIError(t, response, http.StatusNotFound, "NOT_FOUND")
	response = apiRequest(t, server, http.MethodGet, "/api/v1/stats/series?dimension=cluster&window=5m", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"values":[2,3,4,5]`) {
		t.Fatalf("series = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodGet, "/api/v1/stats/series?window=1d", nil, cookie)
	assertAPIError(t, response, http.StatusBadRequest, "INVALID_ARGUMENT")
	response = apiRequest(t, server, http.MethodGet, "/api/v1/system/info", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"version":"1.39.0"`) || !strings.Contains(response.Body.String(), `"mode":"xds"`) {
		t.Fatalf("system = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodGet, "/api/v1/envoy/stats/prometheus", nil, cookie)
	if response.Code != http.StatusOK || response.Body.String() != "envoy_requests_total 2\n" {
		t.Fatalf("prometheus = %d %q", response.Code, response.Body.String())
	}
}

func TestPrometheusFailureIsRedacted(t *testing.T) {
	t.Parallel()
	stateAPI := &StateAPI{
		State: &fakeStateReader{current: &state.DataPlaneState{}},
		Prometheus: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "secret admin address", http.StatusBadGateway)
		}),
	}
	server := newTestServer(t, nil, nil, stateAPI.Handlers())
	cookie := bootstrapCookie(t, server)
	response := apiRequest(t, server, http.MethodGet, "/api/v1/envoy/stats/prometheus", nil, cookie)
	assertAPIError(t, response, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE")
	if strings.Contains(response.Body.String(), "secret") {
		t.Fatal("upstream detail leaked")
	}
}

type fakeStateReader struct {
	current *state.DataPlaneState
	series  []state.Series
	err     error
}

func (f *fakeStateReader) CurrentState(context.Context, bool) (*state.DataPlaneState, error) {
	return f.current, f.err
}

func (f *fakeStateReader) Series(_ context.Context, query state.SeriesQuery) ([]state.Series, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []state.Series
	for _, item := range f.series {
		if query.Dim != "" && item.Key.Dim != query.Dim || query.Name != "" && item.Key.Name != query.Name || query.Metric != "" && item.Key.Metric != query.Metric {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}
