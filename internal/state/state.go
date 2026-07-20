package state

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ObjectRef identifies a protocol object owner.
type ObjectRef struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// ServerInfo is the normalized Envoy server_info response.
type ServerInfo struct {
	Version         string        `json:"version"`
	State           string        `json:"state"`
	Uptime          time.Duration `json:"uptime"`
	HotRestartEpoch int           `json:"hot_restart_epoch"`
}

// VersionStatus reports expected and observed data-plane versions.
type VersionStatus struct {
	Expected string            `json:"expected"`
	Observed string            `json:"observed"`
	Status   string            `json:"status"` // IDLE | CONFIRMED | TIMEOUT
	At       time.Time         `json:"at"`
	Elapsed  time.Duration     `json:"elapsed"`
	Details  []ResourceVersion `json:"details,omitempty"`
}

// ResourceVersion is one observed xDS version_info entry.
type ResourceVersion struct {
	Type    string `json:"type"`
	Version string `json:"version"`
}

// DataPlaneState is the current normalized state snapshot.
type DataPlaneState struct {
	NodeID        string          `json:"node_id"`
	Envoy         ServerInfo      `json:"envoy"`
	CollectedAt   time.Time       `json:"collected_at"`
	Stale         bool            `json:"stale"`
	LastSuccessAt time.Time       `json:"last_success_at"`
	Version       VersionStatus   `json:"version"`
	Listeners     []ListenerState `json:"listeners,omitempty"`
	Clusters      []ClusterState  `json:"clusters,omitempty"`
}

// ListenerState is a normalized listener resource status.
type ListenerState struct {
	Name    string     `json:"name"`
	Address string     `json:"address,omitempty"`
	Owner   *ObjectRef `json:"owner,omitempty"`
}

// ClusterState is a normalized cluster and endpoint status.
type ClusterState struct {
	Name      string          `json:"name"`
	Owner     *ObjectRef      `json:"owner,omitempty"`
	Endpoints []EndpointState `json:"endpoints,omitempty"`
}

// EndpointState is a normalized cluster endpoint health record.
type EndpointState struct {
	Address   string   `json:"address"`
	Port      uint32   `json:"port"`
	Health    string   `json:"health"`
	Weight    uint32   `json:"weight,omitempty"`
	FailFlags []string `json:"fail_flags,omitempty"`
}

// VersionConfirmEvent is emitted when an expected version confirms or times out.
type VersionConfirmEvent struct {
	Expected  string
	Observed  string
	Status    string
	Resources []ResourceVersion
	Elapsed   time.Duration
	At        time.Time
}

// SeriesQuery selects metric series. Empty fields match all series.
type SeriesQuery struct {
	Dim    string
	Name   string
	Metric string
}

// Service implements M-STATE read-only collection and confirmation.
type Service struct {
	NodeID string
	Client AdminClient

	mu            sync.Mutex
	current       DataPlaneState
	expected      string
	expectAt      time.Time
	timeout       time.Duration
	confirmCh     map[int]chan<- VersionConfirmEvent
	nextSub       int
	confirmCancel context.CancelFunc
	series        *SeriesStore
}

// New constructs an M-STATE service.
func New(nodeID string, client AdminClient) *Service {
	return &Service{NodeID: nodeID, Client: client, timeout: 30 * time.Second, confirmCh: map[int]chan<- VersionConfirmEvent{}, series: NewSeriesStore(2160, 5000, 10*time.Second)}
}

// CurrentState returns the cached state, optionally refreshing from Envoy.
func (s *Service) CurrentState(ctx context.Context, refresh bool) (*DataPlaneState, error) {
	if refresh {
		if err := s.refresh(ctx); err != nil {
			s.mu.Lock()
			state := s.current
			state.Stale = true
			s.current = state
			s.mu.Unlock()
			return &state, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.current
	return &state, nil
}

// VersionStatus returns the current version confirmation state.
func (s *Service) VersionStatus(context.Context) VersionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current.Version
}

// Series returns the current bounded metric samples.
func (s *Service) Series(_ context.Context, q SeriesQuery) ([]Series, error) {
	if s.series == nil {
		return nil, nil
	}
	all := s.series.Snapshot()
	out := make([]Series, 0, len(all))
	for _, item := range all {
		if q.Dim != "" && q.Dim != item.Key.Dim {
			continue
		}
		if q.Name != "" && q.Name != item.Key.Name {
			continue
		}
		if q.Metric != "" && q.Metric != item.Key.Metric {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

// PrometheusProxy returns an HTTP handler for Envoy's raw Prometheus metrics.
func (s *Service) PrometheusProxy() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.Client.Prometheus(r.Context(), w); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
	})
}

// ExpectVersion registers a version to confirm through config_dump polling.
func (s *Service) ExpectVersion(version string, timeout time.Duration) {
	s.mu.Lock()
	if s.confirmCancel != nil {
		s.confirmCancel()
	}
	s.expected = version
	s.expectAt = time.Now()
	if timeout > 0 {
		s.timeout = timeout
	}
	s.current.Version = VersionStatus{Expected: version, Status: "IDLE"}
	ctx, cancel := context.WithCancel(context.Background())
	s.confirmCancel = cancel
	s.mu.Unlock()
	go s.confirmLoop(ctx)
}

// SubscribeConfirm subscribes to confirmation and timeout events.
func (s *Service) SubscribeConfirm(ch chan<- VersionConfirmEvent) func() {
	s.mu.Lock()
	id := s.nextSub
	s.nextSub++
	s.confirmCh[id] = ch
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.confirmCh, id)
			s.mu.Unlock()
		})
	}
}

func (s *Service) refresh(ctx context.Context) error {
	serverBody, err := s.Client.Get(ctx, "/server_info")
	if err != nil {
		return err
	}
	var server struct {
		Version string `json:"version"`
		State   string `json:"state"`
		Uptime  string `json:"uptime_current_epoch"`
		Epoch   int    `json:"hot_restart_epoch"`
	}
	if err := json.Unmarshal(serverBody, &server); err != nil {
		return err
	}
	dumpBody, err := s.Client.Get(ctx, "/config_dump?include_eds")
	if err != nil {
		return err
	}
	observed, resources := configDumpVersions(dumpBody)
	clusterBody, clusterErr := s.Client.Get(ctx, "/clusters?format=json")
	now := time.Now()
	s.mu.Lock()
	s.current.NodeID = s.NodeID
	s.current.Envoy = ServerInfo{Version: server.Version, State: server.State, Uptime: parseUptime(server.Uptime), HotRestartEpoch: server.Epoch}
	s.current.CollectedAt = now
	s.current.LastSuccessAt = now
	s.current.Stale = false
	if s.expected != "" {
		status := "IDLE"
		if observed == s.expected {
			status = "CONFIRMED"
		} else if !s.expectAt.IsZero() && now.Sub(s.expectAt) >= s.timeout {
			status = "TIMEOUT"
		}
		s.current.Version = VersionStatus{Expected: s.expected, Observed: observed, Status: status, At: now, Elapsed: now.Sub(s.expectAt), Details: resources}
		if status == "CONFIRMED" || status == "TIMEOUT" {
			ev := VersionConfirmEvent{Expected: s.expected, Observed: observed, Status: status, Resources: resources, Elapsed: now.Sub(s.expectAt), At: now}
			for _, ch := range s.confirmCh {
				select {
				case ch <- ev:
				default:
				}
			}
			s.expected = ""
		}
	}
	s.current.Listeners = parseListeners(dumpBody)
	if clusterErr == nil {
		s.current.Clusters = parseClusters(clusterBody)
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) confirmLoop(ctx context.Context) {
	delay := 500 * time.Millisecond
	for {
		if _, err := s.CurrentState(ctx, true); err == nil {
			s.mu.Lock()
			done := s.current.Version.Status == "CONFIRMED" || s.current.Version.Status == "TIMEOUT"
			s.mu.Unlock()
			if done {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if delay < 5*time.Second {
			delay *= 2
		}
	}
}

func configDumpVersions(body []byte) (string, []ResourceVersion) {
	var dump map[string]any
	if json.Unmarshal(body, &dump) != nil {
		return "", nil
	}
	var resources []ResourceVersion
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, value := range x {
				if k == "version_info" {
					if version, ok := value.(string); ok {
						resources = append(resources, ResourceVersion{Type: "", Version: version})
					}
				}
				walk(value)
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		}
	}
	walk(dump)
	versions := map[string]bool{}
	for _, item := range resources {
		if item.Version != "" {
			versions[item.Version] = true
		}
	}
	if len(versions) == 1 {
		for version := range versions {
			return version, resources
		}
	}
	values := func() []string {
		out := make([]string, 0, len(versions))
		for version := range versions {
			out = append(out, version)
		}
		sort.Strings(out)
		return out
	}()
	return strings.Join(values, ","), resources
}

func parseListeners(body []byte) []ListenerState {
	var dump map[string]any
	if json.Unmarshal(body, &dump) != nil {
		return nil
	}
	var out []ListenerState
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if name, ok := x["name"].(string); ok && strings.HasPrefix(name, "lis/") {
				item := ListenerState{Name: name, Owner: resolveOwner(name)}
				if address, ok := x["address"].(map[string]any); ok {
					item.Address = addressString(address)
				}
				out = append(out, item)
			}
			for _, value := range x {
				walk(value)
			}
		case []any:
			for _, item := range x {
				walk(item)
			}
		}
	}
	walk(dump)
	return out
}

func parseClusters(body []byte) []ClusterState {
	var doc struct {
		Clusters []struct {
			Name  string `json:"name"`
			Hosts []struct {
				Address struct {
					Socket struct {
						Address   string `json:"address"`
						PortValue uint32 `json:"port_value"`
					} `json:"socket_address"`
				} `json:"address"`
				Health                  string `json:"health_status"`
				Weight                  uint32 `json:"weight"`
				FailedActiveHealthCheck bool   `json:"failed_active_health_check"`
				FailedOutlierCheck      bool   `json:"failed_outlier_check"`
				FailedActiveHcTimeout   bool   `json:"failed_active_hc_timeout"`
			} `json:"host_statuses"`
		} `json:"cluster_statuses"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return nil
	}
	out := make([]ClusterState, 0, len(doc.Clusters))
	for _, c := range doc.Clusters {
		cluster := ClusterState{Name: c.Name, Owner: resolveOwner(c.Name)}
		for _, host := range c.Hosts {
			health := strings.ToUpper(host.Health)
			if health == "" {
				health = "UNKNOWN"
			} else if health == "HEALTHY" || health == "UNHEALTHY" || health == "DRAINING" || health == "TIMEOUT" {
				// already normalized
			} else if strings.Contains(health, "HEALTHY") && !strings.Contains(health, "UNHEALTHY") {
				health = "HEALTHY"
			} else {
				health = "UNHEALTHY"
			}
			flags := make([]string, 0, 3)
			if host.FailedActiveHealthCheck {
				flags = append(flags, "failed_active_health_check")
			}
			if host.FailedOutlierCheck {
				flags = append(flags, "failed_outlier_check")
			}
			if host.FailedActiveHcTimeout {
				flags = append(flags, "failed_active_hc_timeout")
			}
			switch strings.ToLower(host.Health) {
			case "failed_active_health_check":
				flags = appendUnique(flags, "failed_active_health_check")
			case "failed_outlier_check":
				flags = appendUnique(flags, "failed_outlier_check")
			case "failed_active_hc_timeout":
				flags = appendUnique(flags, "failed_active_hc_timeout")
			}
			cluster.Endpoints = append(cluster.Endpoints, EndpointState{
				Address:   host.Address.Socket.Address,
				Port:      host.Address.Socket.PortValue,
				Health:    health,
				Weight:    host.Weight,
				FailFlags: flags,
			})
		}
		out = append(out, cluster)
	}
	return out
}

func appendUnique(values []string, value string) []string {
	for _, item := range values {
		if item == value {
			return values
		}
	}
	return append(values, value)
}

func parseUptime(value string) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := time.ParseDuration(value + "s"); err == nil {
		return seconds
	}
	return 0
}

func resolveOwner(name string) *ObjectRef {
	switch {
	case strings.HasPrefix(name, "lis/"):
		return &ObjectRef{Kind: "Listener", Name: strings.TrimPrefix(name, "lis/")}
	case strings.HasPrefix(name, "rc/"):
		return &ObjectRef{Kind: "Listener", Name: strings.TrimPrefix(name, "rc/")}
	case strings.HasPrefix(name, "us/"):
		return &ObjectRef{Kind: "Upstream", Name: strings.TrimPrefix(name, "us/")}
	case strings.HasPrefix(name, "crt/"):
		parts := strings.Split(name, "/")
		if len(parts) > 1 {
			return &ObjectRef{Kind: "Listener", Name: parts[1]}
		}
	}
	return nil
}

func addressString(value map[string]any) string {
	if socket, ok := value["socket_address"].(map[string]any); ok {
		address, _ := socket["address"].(string)
		port, _ := socket["port_value"].(float64)
		if address != "" && port != 0 {
			return address + ":" + fmt.Sprintf("%.0f", port)
		}
	}
	return ""
}
