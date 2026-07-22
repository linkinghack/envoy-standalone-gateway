package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/state"
	buildversion "github.com/linkinghack/envoy-standalone-gateway/internal/version"
)

// StateReader is the bounded read surface exposed by M-STATE.
type StateReader interface {
	CurrentState(context.Context, bool) (*state.DataPlaneState, error)
	Series(context.Context, state.SeriesQuery) ([]state.Series, error)
}

// StateAPI adapts normalized data-plane state and metrics to HTTP.
type StateAPI struct {
	State      StateReader
	Prometheus http.Handler
	Mode       string
	Topology   string
}

// Handlers returns all state, stats and system operation adapters.
func (a *StateAPI) Handlers() map[string]OperationHandler {
	return map[string]OperationHandler{
		"getStatusSummary":           a.summary,
		"listStatusListeners":        a.listeners,
		"listStatusClusters":         a.clusters,
		"listStatusClusterEndpoints": a.endpoints,
		"listStatusRoutes":           a.routes,
		"listStatusCerts":            a.certs,
		"getStatsOverview":           a.statsOverview,
		"listStatsSeries":            a.statsSeries,
		"getSystemInfo":              a.systemInfo,
		"getEnvoyPrometheusStats":    a.envoyPrometheus,
	}
}

func (a *StateAPI) summary(w http.ResponseWriter, r *http.Request) {
	current, ok := a.current(w, r)
	if !ok {
		return
	}
	endpoints := 0
	for _, cluster := range current.Clusters {
		endpoints += len(cluster.Endpoints)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodeId": current.NodeID, "ready": current.Ready, "readyStatus": current.ReadyStatus,
		"stale": current.Stale, "collectedAt": current.CollectedAt, "lastSuccessAt": current.LastSuccessAt,
		"envoy": current.Envoy, "version": current.Version,
		"counts": map[string]int{"listeners": len(current.Listeners), "clusters": len(current.Clusters), "endpoints": endpoints, "routes": len(current.Routes), "certs": len(current.Certs)},
	})
}

func (a *StateAPI) listeners(w http.ResponseWriter, r *http.Request) {
	current, ok := a.current(w, r)
	if ok {
		writeStateItems(w, current.Listeners, current)
	}
}

func (a *StateAPI) clusters(w http.ResponseWriter, r *http.Request) {
	current, ok := a.current(w, r)
	if ok {
		writeStateItems(w, current.Clusters, current)
	}
}

func (a *StateAPI) endpoints(w http.ResponseWriter, r *http.Request) {
	current, ok := a.current(w, r)
	if !ok {
		return
	}
	for _, cluster := range current.Clusters {
		if cluster.Name == r.PathValue("name") {
			writeStateItems(w, cluster.Endpoints, current)
			return
		}
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "cluster not found")
}

func (a *StateAPI) routes(w http.ResponseWriter, r *http.Request) {
	current, ok := a.current(w, r)
	if ok {
		writeStateItems(w, current.Routes, current)
	}
}

func (a *StateAPI) certs(w http.ResponseWriter, r *http.Request) {
	current, ok := a.current(w, r)
	if ok {
		writeStateItems(w, current.Certs, current)
	}
}

func (a *StateAPI) statsOverview(w http.ResponseWriter, r *http.Request) {
	window, ok := parseWindow(w, r)
	if !ok {
		return
	}
	series, err := a.series(r.Context(), state.SeriesQuery{}, window)
	if err != nil {
		writeStateInternalError(w, err)
		return
	}
	byDimension := map[string]int{}
	for _, item := range series {
		byDimension[item.Key.Dim]++
	}
	writeJSON(w, http.StatusOK, map[string]any{"window": window.String(), "series": len(series), "byDimension": byDimension})
}

func (a *StateAPI) statsSeries(w http.ResponseWriter, r *http.Request) {
	window, ok := parseWindow(w, r)
	if !ok {
		return
	}
	dimension := r.URL.Query().Get("dimension")
	if dimension != "" && dimension != "route" && dimension != "cluster" {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "dimension must be route or cluster")
		return
	}
	series, err := a.series(r.Context(), state.SeriesQuery{
		Dim: dimension, Name: r.URL.Query().Get("name"), Metric: r.URL.Query().Get("metric"),
	}, window)
	if err != nil {
		writeStateInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": series, "total": len(series), "window": window.String()})
}

func (a *StateAPI) systemInfo(w http.ResponseWriter, r *http.Request) {
	current, ok := a.current(w, r)
	if !ok {
		return
	}
	matrix := append([]string(nil), buildversion.EnvoyMatrixVersions...)
	writeJSON(w, http.StatusOK, map[string]any{
		"version": buildversion.Version, "goVersion": runtime.Version(), "mode": a.Mode, "topology": a.Topology,
		"envoy": map[string]any{"version": current.Envoy.Version, "supportedMinorMin": buildversion.EnvoyMinMinor, "supportedMinorMax": buildversion.EnvoyMaxMinor, "validationMatrix": matrix},
	})
}

func (a *StateAPI) envoyPrometheus(w http.ResponseWriter, r *http.Request) {
	if a.Prometheus == nil {
		writeError(w, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE", "Envoy metrics are unavailable")
		return
	}
	buffer := &bufferedResponse{header: make(http.Header), status: http.StatusOK}
	a.Prometheus.ServeHTTP(buffer, r)
	if buffer.status >= http.StatusBadRequest {
		writeError(w, http.StatusBadGateway, "UPSTREAM_UNAVAILABLE", "Envoy metrics are unavailable")
		return
	}
	for key, values := range buffer.header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buffer.body.Bytes())
}

func (a *StateAPI) current(w http.ResponseWriter, r *http.Request) (*state.DataPlaneState, bool) {
	if a.State == nil {
		writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "data-plane state is unavailable")
		return nil, false
	}
	current, err := a.State.CurrentState(r.Context(), false)
	if err != nil {
		writeStateInternalError(w, err)
		return nil, false
	}
	if current == nil {
		current = &state.DataPlaneState{Stale: true}
	}
	return current, true
}

func (a *StateAPI) series(ctx context.Context, query state.SeriesQuery, window time.Duration) ([]state.Series, error) {
	if a.State == nil {
		return nil, errors.New("data-plane state is unavailable")
	}
	series, err := a.State.Series(ctx, query)
	if err != nil {
		return nil, err
	}
	for index := range series {
		series[index] = trimSeries(series[index], window)
	}
	sort.Slice(series, func(i, j int) bool {
		left, right := series[i].Key, series[j].Key
		return fmt.Sprintf("%s\x00%s\x00%s", left.Dim, left.Name, left.Metric) < fmt.Sprintf("%s\x00%s\x00%s", right.Dim, right.Name, right.Metric)
	})
	return series, nil
}

func trimSeries(item state.Series, window time.Duration) state.Series {
	filled := min(item.Filled, len(item.Values))
	if filled == 0 {
		item.Values = nil
		return item
	}
	start := item.Head - filled
	if start < 0 {
		start += len(item.Values)
	}
	ordered := make([]int64, filled)
	for index := range filled {
		ordered[index] = item.Values[(start+index)%len(item.Values)]
	}
	if item.Interval > 0 {
		limit := int(window / item.Interval)
		if limit > 0 && len(ordered) > limit {
			ordered = ordered[len(ordered)-limit:]
		}
	}
	item.Values, item.Head, item.Filled = ordered, 0, len(ordered)
	return item
}

func parseWindow(w http.ResponseWriter, r *http.Request) (time.Duration, bool) {
	raw := r.URL.Query().Get("window")
	if raw == "" {
		raw = "15m"
	}
	allowed := map[string]time.Duration{"5m": 5 * time.Minute, "15m": 15 * time.Minute, "1h": time.Hour, "6h": 6 * time.Hour}
	window, ok := allowed[raw]
	if !ok {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "window must be 5m, 15m, 1h or 6h")
	}
	return window, ok
}

func writeStateItems(w http.ResponseWriter, items any, current *state.DataPlaneState) {
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "stale": current.Stale, "collectedAt": current.CollectedAt})
}

func writeStateInternalError(w http.ResponseWriter, err error) {
	_ = err
	writeError(w, http.StatusInternalServerError, "INTERNAL", "internal server error")
}

type bufferedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (r *bufferedResponse) Header() http.Header    { return r.header }
func (r *bufferedResponse) WriteHeader(status int) { r.status = status }
func (r *bufferedResponse) Write(content []byte) (int, error) {
	return r.body.Write(content)
}
