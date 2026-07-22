package protocol

import "fmt"

// Upstream 是后端服务定义（协议 §3.4）。端点来源三选一：endpoints / dns / kubernetesService。
type Upstream struct {
	APIVersion string       `json:"apiVersion"`
	Kind       Kind         `json:"kind"`
	Metadata   ObjectMeta   `json:"metadata"`
	Spec       UpstreamSpec `json:"spec"`
	Origin     Origin       `json:"-"`
}

// UpstreamSpec 是 Upstream 的 spec。
type UpstreamSpec struct {
	Endpoints         []Endpoint               `json:"endpoints,omitempty"`         // ① 静态地址列表（FR-6.4，P0）
	DNS               *DNSEndpointSource       `json:"dns,omitempty"`               // ② 域名解析（FR-6.4，P0）
	KubernetesService *KubernetesServiceSource `json:"kubernetesService,omitempty"` // ③ k8s Service 引用（FR-6.3，P2；仅 k8s 环境可用）
	LoadBalancer      *LoadBalancer            `json:"loadBalancer,omitempty"`
	HealthCheck       *HealthCheck             `json:"healthCheck,omitempty"` // 可选；主动健康检查（FR-1.2）
	TLS               *UpstreamTLS             `json:"tls,omitempty"`         // 可选；对上游启用 TLS
	Connection        *UpstreamConnection      `json:"connection,omitempty"`
	EnvoyPatch        []EnvoyPatch             `json:"envoyPatch,omitempty"` // escape hatch（协议 §7.1）
}

// Endpoint 是一个静态后端地址。
type Endpoint struct {
	Address string `json:"address"`
	Port    int32  `json:"port" jsonschema:"minimum=1,maximum=65535"`
	Weight  *int32 `json:"weight,omitempty"` // 可选
}

// DNSEndpointSource 是域名解析端点来源。
type DNSEndpointSource struct {
	Hostname   string        `json:"hostname"`
	Port       int32         `json:"port" jsonschema:"minimum=1,maximum=65535"`
	Resolution DNSResolution `json:"resolution,omitempty"` // logical(默认,LOGICAL_DNS) | strict(STRICT_DNS)
}

// KubernetesServiceSource 是 k8s Service 端点来源（M-DISCO 提供候选与 EDS 动态端点）。
type KubernetesServiceSource struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Port      int32  `json:"port" jsonschema:"minimum=1,maximum=65535"`
}

// LoadBalancer 是负载均衡配置。
type LoadBalancer struct {
	Policy LBPolicy `json:"policy,omitempty"` // roundRobin(默认) | leastRequest | random | ringHash | maglev
	HashOn []HashOn `json:"hashOn,omitempty"` // 仅 ringHash/maglev 需要
}

// HashOn 是一致性哈希键来源。
type HashOn struct {
	Header string `json:"header"`
}

// HealthCheck 是主动健康检查；http 与 tcp 二选一。
type HealthCheck struct {
	HTTP               *HTTPHealthCheck `json:"http,omitempty"`
	TCP                *TCPHealthCheck  `json:"tcp,omitempty"`
	Interval           *Duration        `json:"interval,omitempty"`
	Timeout            *Duration        `json:"timeout,omitempty"`
	HealthyThreshold   *int32           `json:"healthyThreshold,omitempty"`
	UnhealthyThreshold *int32           `json:"unhealthyThreshold,omitempty"`
}

// HTTPHealthCheck 是 HTTP 探活。
type HTTPHealthCheck struct {
	Path             string  `json:"path,omitempty"`
	ExpectedStatuses []int32 `json:"expectedStatuses,omitempty"` // 默认 [200]
}

// TCPHealthCheck 是 TCP 探活。
type TCPHealthCheck struct{}

// UpstreamTLS 是对上游启用 TLS 的配置。
type UpstreamTLS struct {
	Enabled            bool   `json:"enabled,omitempty"`
	SNI                string `json:"sni,omitempty"`    // 默认取 dns.hostname / rewrite.host
	CAFile             string `json:"caFile,omitempty"` // 省略 = 系统 CA
	InsecureSkipVerify bool   `json:"insecureSkipVerify,omitempty"`
}

// UpstreamConnection 是上游连接配置。
type UpstreamConnection struct {
	ConnectTimeout     *Duration `json:"connectTimeout,omitempty"`                                           // 默认 5s
	HTTP2              bool      `json:"http2,omitempty"`                                                    // 对上游使用 h2（gRPC 后端置 true）
	MaxConnections     *int32    `json:"maxConnections,omitempty" jsonschema:"minimum=1,maximum=2147483647"` // 熔断参数（P1，映射 circuit_breakers）
	MaxPendingRequests *int32    `json:"maxPendingRequests,omitempty" jsonschema:"minimum=1,maximum=2147483647"`
}

const upstreamCircuitBreakerMax int32 = 2_147_483_647

func (s *UpstreamSpec) validate() error {
	sources := 0
	if len(s.Endpoints) > 0 {
		sources++
		for i, e := range s.Endpoints {
			if e.Address == "" {
				return fmt.Errorf("spec.endpoints[%d].address: is required", i)
			}
			if e.Port < 1 || e.Port > 65535 {
				return fmt.Errorf("spec.endpoints[%d].port: invalid port %d (want 1-65535)", i, e.Port)
			}
		}
	}
	if s.DNS != nil {
		sources++
		if s.DNS.Hostname == "" {
			return fmt.Errorf("spec.dns.hostname: is required")
		}
		if s.DNS.Port < 1 || s.DNS.Port > 65535 {
			return fmt.Errorf("spec.dns.port: invalid port %d (want 1-65535)", s.DNS.Port)
		}
		if s.DNS.Resolution != "" && !s.DNS.Resolution.Valid() {
			return fmt.Errorf("spec.dns.resolution: invalid value %q (want logical | strict)", s.DNS.Resolution)
		}
	}
	if s.KubernetesService != nil {
		sources++
		ks := s.KubernetesService
		if ks.Namespace == "" || ks.Name == "" {
			return fmt.Errorf("spec.kubernetesService: namespace and name are required")
		}
		if ks.Port < 1 || ks.Port > 65535 {
			return fmt.Errorf("spec.kubernetesService.port: invalid port %d (want 1-65535)", ks.Port)
		}
	}
	if sources != 1 {
		return fmt.Errorf("spec: exactly one endpoint source of endpoints | dns | kubernetesService is required, got %d", sources)
	}
	if s.LoadBalancer != nil {
		if s.LoadBalancer.Policy != "" && !s.LoadBalancer.Policy.Valid() {
			return fmt.Errorf("spec.loadBalancer.policy: invalid value %q (want roundRobin | leastRequest | random | ringHash | maglev)", s.LoadBalancer.Policy)
		}
		for i, h := range s.LoadBalancer.HashOn {
			if h.Header == "" {
				return fmt.Errorf("spec.loadBalancer.hashOn[%d].header: is required", i)
			}
		}
	}
	if s.HealthCheck != nil {
		probes := 0
		if s.HealthCheck.HTTP != nil {
			probes++
		}
		if s.HealthCheck.TCP != nil {
			probes++
		}
		if probes != 1 {
			return fmt.Errorf("spec.healthCheck: exactly one of http | tcp is required, got %d", probes)
		}
	}
	if c := s.Connection; c != nil {
		for _, item := range []struct {
			name  string
			value *int32
		}{
			{name: "maxConnections", value: c.MaxConnections},
			{name: "maxPendingRequests", value: c.MaxPendingRequests},
		} {
			if item.value != nil && (*item.value < 1 || *item.value > upstreamCircuitBreakerMax) {
				return fmt.Errorf("spec.connection.%s: must be between 1 and %d", item.name, upstreamCircuitBreakerMax)
			}
		}
	}
	return validateEnvoyPatches("spec.envoyPatch", s.EnvoyPatch)
}
