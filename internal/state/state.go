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
	Ready         bool            `json:"ready"`
	ReadyStatus   string          `json:"ready_status,omitempty"`
	Version       VersionStatus   `json:"version"`
	Listeners     []ListenerState `json:"listeners,omitempty"`
	Clusters      []ClusterState  `json:"clusters,omitempty"`
	Certs         []CertState     `json:"certs,omitempty"`
	Routes        []RouteState    `json:"routes,omitempty"`
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

// CertState is a normalized certificate status record.
type CertState struct {
	Name     string     `json:"name"`
	Owner    *ObjectRef `json:"owner,omitempty"`
	Subject  string     `json:"subject,omitempty"`
	SANs     []string   `json:"sans,omitempty"`
	NotAfter time.Time  `json:"not_after,omitempty"`
	DaysLeft int        `json:"days_left,omitempty"`
	Serial   string     `json:"serial,omitempty"`
}

// RouteState is a normalized route configuration with virtual hosts.
type RouteState struct {
	Name         string       `json:"name"`
	Owner        *ObjectRef   `json:"owner,omitempty"`
	VirtualHosts []VHostState `json:"virtual_hosts,omitempty"`
}

// VHostState is a normalized route virtual host.
type VHostState struct {
	Name    string     `json:"name"`
	Domains []string   `json:"domains,omitempty"`
	Owner   *ObjectRef `json:"owner,omitempty"`
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
	collectMu     sync.Mutex
	adminMu       sync.Mutex
	requestMu     sync.Mutex
	inflight      map[string]*adminFlight
	backoffMu     sync.Mutex
	backoff       map[string]backoffState
}

type adminFlight struct {
	done chan struct{}
	body []byte
	err  error
}

type backoffState struct {
	failures int
	next     time.Time
}

func (s *Service) get(ctx context.Context, path string) ([]byte, error) {
	s.requestMu.Lock()
	if flight := s.inflight[path]; flight != nil {
		s.requestMu.Unlock()
		select {
		case <-flight.done:
			return flight.body, flight.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	flight := &adminFlight{done: make(chan struct{})}
	s.inflight[path] = flight
	s.requestMu.Unlock()

	s.adminMu.Lock()
	body, err := s.Client.Get(ctx, path)
	s.adminMu.Unlock()

	s.requestMu.Lock()
	flight.body, flight.err = body, err
	delete(s.inflight, path)
	close(flight.done)
	s.requestMu.Unlock()
	return body, err
}

// PollConfig controls the periodic state collector.
type PollConfig struct {
	ReadyInterval    time.Duration
	StatsInterval    time.Duration
	ClustersInterval time.Duration
	ConfigInterval   time.Duration
	CertsInterval    time.Duration
}

func (c PollConfig) withDefaults() PollConfig {
	if c.ReadyInterval <= 0 {
		c.ReadyInterval = 10 * time.Second
	}
	if c.StatsInterval <= 0 {
		c.StatsInterval = 10 * time.Second
	}
	if c.ClustersInterval <= 0 {
		c.ClustersInterval = 15 * time.Second
	}
	if c.ConfigInterval <= 0 {
		c.ConfigInterval = 60 * time.Second
	}
	if c.CertsInterval <= 0 {
		c.CertsInterval = 5 * time.Minute
	}
	return c
}

// New constructs an M-STATE service.
func New(nodeID string, client AdminClient) *Service {
	return &Service{
		NodeID: nodeID, Client: client, timeout: 30 * time.Second,
		confirmCh: map[int]chan<- VersionConfirmEvent{},
		series:    NewSeriesStore(2160, 5000, 10*time.Second),
		inflight:  map[string]*adminFlight{},
		backoff:   map[string]backoffState{},
	}
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

// ObserveProcess is the narrow M-PROC probe. M-STATE remains the sole Envoy
// admin consumer while exposing only LIVE and hot restart epoch.
func (s *Service) ObserveProcess(ctx context.Context) (bool, int, error) {
	s.collectReady(ctx)
	body, err := s.get(ctx, "/server_info")
	if err != nil {
		return false, 0, err
	}
	var server struct {
		Epoch int `json:"hot_restart_epoch"`
	}
	if err := json.Unmarshal(body, &server); err != nil {
		return false, 0, err
	}
	s.mu.Lock()
	ready := s.current.Ready
	s.mu.Unlock()
	return ready, server.Epoch, nil
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

// Start starts serialized periodic collection. The returned function stops it.
func (s *Service) Start(ctx context.Context, config PollConfig) func() {
	config = config.withDefaults()
	runCtx, cancel := context.WithCancel(ctx)
	go s.pollLoop(runCtx, config)
	return cancel
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
	s.collectMu.Lock()
	defer s.collectMu.Unlock()
	return s.refreshLocked(ctx, true)
}

func (s *Service) refreshLocked(ctx context.Context, includeClusters bool) error {
	serverBody, err := s.get(ctx, "/server_info")
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
	dumpBody, err := s.get(ctx, "/config_dump?include_eds")
	if err != nil {
		return err
	}
	observed, resources := configDumpVersions(dumpBody)
	var clusterBody []byte
	var clusterErr error
	if includeClusters {
		clusterBody, clusterErr = s.get(ctx, "/clusters?format=json")
	}
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
	s.current.Routes = parseRoutes(dumpBody)
	if clusterErr == nil {
		s.current.Clusters = parseClusters(clusterBody)
	}
	if certsBody, certErr := s.get(ctx, "/certs"); certErr == nil {
		s.current.Certs = parseCerts(certsBody)
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) pollLoop(ctx context.Context, config PollConfig) {
	readyInterval := time.Second
	ready := time.NewTicker(readyInterval)
	stats := time.NewTicker(config.StatsInterval)
	clusters := time.NewTicker(config.ClustersInterval)
	dump := time.NewTicker(config.ConfigInterval)
	certs := time.NewTicker(config.CertsInterval)
	defer ready.Stop()
	defer stats.Stop()
	defer clusters.Stop()
	defer dump.Stop()
	defer certs.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ready.C:
			s.collectReady(ctx)
			s.mu.Lock()
			isReady := s.current.Ready
			s.mu.Unlock()
			if isReady && readyInterval != config.ReadyInterval {
				ready.Stop()
				readyInterval = config.ReadyInterval
				ready = time.NewTicker(readyInterval)
			}
		case <-stats.C:
			s.collectStats(ctx)
		case <-clusters.C:
			s.collectClusters(ctx)
		case <-dump.C:
			_, _ = s.CurrentState(ctx, true)
		case <-certs.C:
			s.collectCerts(ctx)
		}
	}
}

func (s *Service) collectReady(ctx context.Context) {
	s.collectMu.Lock()
	defer s.collectMu.Unlock()
	const path = "/ready"
	if !s.backoffAllowed(path) {
		return
	}
	body, err := s.get(ctx, path)
	s.recordBackoff(path, err)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.current.Ready = false
		s.current.ReadyStatus = "UNAVAILABLE"
		return
	}
	status := strings.TrimSpace(string(body))
	s.current.ReadyStatus = status
	s.current.Ready = strings.EqualFold(status, "LIVE") || strings.EqualFold(status, "OK")
}

func (s *Service) collectStats(ctx context.Context) {
	s.collectMu.Lock()
	defer s.collectMu.Unlock()
	const path = "/stats?format=json"
	if !s.backoffAllowed(path) {
		return
	}
	body, err := s.get(ctx, path)
	s.recordBackoff(path, err)
	if err != nil {
		return
	}
	now := time.Now()
	for _, sample := range parseStats(body) {
		_ = s.series.Add(sample.Key, sample.Value, now)
	}
}

func (s *Service) collectClusters(ctx context.Context) {
	s.collectMu.Lock()
	defer s.collectMu.Unlock()
	const path = "/clusters?format=json"
	if !s.backoffAllowed(path) {
		return
	}
	body, err := s.get(ctx, path)
	s.recordBackoff(path, err)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.current.Clusters = parseClusters(body)
	s.mu.Unlock()
}

func (s *Service) collectCerts(ctx context.Context) {
	s.collectMu.Lock()
	defer s.collectMu.Unlock()
	const path = "/certs"
	if !s.backoffAllowed(path) {
		return
	}
	body, err := s.get(ctx, path)
	s.recordBackoff(path, err)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.current.Certs = parseCerts(body)
	s.mu.Unlock()
}

func (s *Service) backoffAllowed(path string) bool {
	s.backoffMu.Lock()
	defer s.backoffMu.Unlock()
	state := s.backoff[path]
	return state.next.IsZero() || !time.Now().Before(state.next)
}

func (s *Service) recordBackoff(path string, err error) {
	s.backoffMu.Lock()
	defer s.backoffMu.Unlock()
	if err == nil {
		delete(s.backoff, path)
		return
	}
	state := s.backoff[path]
	state.failures++
	delay := time.Second
	for i := 1; i < state.failures && delay < time.Minute; i++ {
		delay *= 2
	}
	if delay > time.Minute {
		delay = time.Minute
	}
	state.next = time.Now().Add(delay)
	s.backoff[path] = state
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
	var walk func(any, string)
	walk = func(v any, contextType string) {
		switch x := v.(type) {
		case map[string]any:
			typeName := contextType
			if raw, ok := x["@type"].(string); ok {
				typeName = raw
			}
			for k, value := range x {
				if k == "version_info" {
					if version, ok := value.(string); ok {
						resources = append(resources, ResourceVersion{Type: typeName, Version: version})
					}
				}
				walk(value, typeName+"/"+k)
			}
		case []any:
			for _, item := range x {
				walk(item, contextType)
			}
		}
	}
	walk(dump, "")
	filtered := resources[:0]
	for _, item := range resources {
		lower := strings.ToLower(item.Type)
		if strings.Contains(lower, "endpoint") || strings.Contains(lower, "eds") {
			continue
		}
		filtered = append(filtered, item)
	}
	resources = filtered
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

func parseRoutes(body []byte) []RouteState {
	var dump map[string]any
	if json.Unmarshal(body, &dump) != nil {
		return nil
	}
	var out []RouteState
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			name, _ := x["name"].(string)
			if strings.HasPrefix(name, "rc/") {
				route := RouteState{Name: name, Owner: resolveOwner(name)}
				if virtualHosts, ok := x["virtual_hosts"].([]any); ok {
					for _, raw := range virtualHosts {
						host, ok := raw.(map[string]any)
						if !ok {
							continue
						}
						vhostName, _ := host["name"].(string)
						vhost := VHostState{Name: vhostName, Owner: resolveOwner(vhostName)}
						if domains, ok := host["domains"].([]any); ok {
							for _, domain := range domains {
								if value, ok := domain.(string); ok {
									vhost.Domains = append(vhost.Domains, value)
								}
							}
						}
						route.VirtualHosts = append(route.VirtualHosts, vhost)
					}
				}
				out = append(out, route)
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

type statSample struct {
	Key   SeriesKey
	Value int64
}

func parseStats(body []byte) []statSample {
	var doc struct {
		Stats []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		} `json:"stats"`
		Histograms []struct {
			Name               string `json:"name"`
			SupportedQuantiles []struct {
				Quantile float64 `json:"quantile"`
				Value    int64   `json:"value"`
			} `json:"supported_quantiles"`
		} `json:"histograms"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return nil
	}
	out := make([]statSample, 0, len(doc.Stats))
	for _, item := range doc.Stats {
		var value int64
		if json.Unmarshal(item.Value, &value) != nil {
			var text string
			if json.Unmarshal(item.Value, &text) != nil {
				continue
			}
			if _, err := fmt.Sscan(text, &value); err != nil {
				continue
			}
		}
		dim, name, metric := statIdentity(item.Name)
		out = append(out, statSample{Key: SeriesKey{Dim: dim, Name: name, Metric: metric}, Value: value})
	}
	for _, histogram := range doc.Histograms {
		dim, name, metric := statIdentity(histogram.Name)
		for _, quantile := range histogram.SupportedQuantiles {
			suffix := fmt.Sprintf("p%d", int(quantile.Quantile*100))
			out = append(out, statSample{
				Key:   SeriesKey{Dim: dim, Name: name, Metric: metric + "." + suffix},
				Value: quantile.Value,
			})
		}
	}
	return out
}

func statIdentity(name string) (string, string, string) {
	parts := strings.Split(name, ".")
	if len(parts) >= 3 && parts[0] == "cluster" {
		return "upstream", parts[1], strings.Join(parts[2:], ".")
	}
	if len(parts) >= 3 && parts[0] == "http" {
		return "listener", parts[1], strings.Join(parts[2:], ".")
	}
	if len(parts) >= 3 && parts[0] == "listener" {
		return "listener", parts[1], strings.Join(parts[2:], ".")
	}
	return "global", "", name
}

func parseCerts(body []byte) []CertState {
	var doc struct {
		Certificates []struct {
			Name          string   `json:"name"`
			CertChainFile string   `json:"cert_chain_file"`
			Subject       string   `json:"subject"`
			Serial        string   `json:"serial_number"`
			SANs          []string `json:"subject_alt_names"`
			NotAfter      string   `json:"expiration_time"`
			DaysLeft      float64  `json:"days_until_expiration"`
		} `json:"certificates"`
	}
	if json.Unmarshal(body, &doc) != nil {
		return nil
	}
	out := make([]CertState, 0, len(doc.Certificates))
	for _, item := range doc.Certificates {
		name := item.Name
		if name == "" {
			name = item.CertChainFile
		}
		var notAfter time.Time
		if item.NotAfter != "" {
			notAfter, _ = time.Parse(time.RFC3339, item.NotAfter)
		}
		days := int(item.DaysLeft)
		if days == 0 && !notAfter.IsZero() {
			days = int(time.Until(notAfter).Hours() / 24)
		}
		out = append(out, CertState{Name: name, Owner: resolveOwner(name), Subject: item.Subject, SANs: item.SANs, NotAfter: notAfter, DaysLeft: days, Serial: item.Serial})
	}
	return out
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
