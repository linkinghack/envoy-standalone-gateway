package protocol

import "time"

// 协议声明的全部默认值集中定义于此文件（编译层 F2 消费）。
// 本包只定义常量与 ApplyDefaults；调用时机归编译层（T3）。

const (
	// DefaultGatewayName 是 v0 唯一隐式 Gateway 实例的名字（协议 §2.2）。
	DefaultGatewayName = "default"

	// DefaultIdleTimeout 是 Gateway.spec.http.idleTimeout 的默认值（协议 §3.1）。
	DefaultIdleTimeout = 60 * time.Second
	// DefaultMaxRequestHeadersKB 是 Gateway.spec.http.maxRequestHeadersKb 的默认值（协议 §3.1）。
	DefaultMaxRequestHeadersKB = 60
	// DefaultServerHeader 是 Gateway.spec.http.serverHeader 的默认值（协议 §3.1）；"" 表示透传上游。
	DefaultServerHeader = "esgw"

	// DefaultListenerAddress 是 Listener.spec.address 的默认值（协议 §3.2）。
	DefaultListenerAddress = "0.0.0.0"
	// DefaultTLSMinVersion 是 Listener.spec.tls.minVersion 的默认值（协议 §3.2）。
	DefaultTLSMinVersion = TLSVersion12

	// DefaultRuleTimeout 是 rule.timeout 的默认值（协议 §3.3）；0s = 不限。
	DefaultRuleTimeout = 15 * time.Second

	// DefaultDNSResolution 是 Upstream.spec.dns.resolution 的默认值（协议 §3.4）。
	DefaultDNSResolution = DNSResolutionLogical
	// DefaultLBPolicy 是 Upstream.spec.loadBalancer.policy 的默认值（协议 §3.4）。
	DefaultLBPolicy = LBPolicyRoundRobin
	// DefaultConnectTimeout 是 Upstream.spec.connection.connectTimeout 的默认值（协议 §3.4）。
	DefaultConnectTimeout = 5 * time.Second
)

// DefaultALPN 是 Listener.spec.tls.alpn 的默认值（协议 §3.2）。
var DefaultALPN = []string{"h2", "http/1.1"}

// DefaultExpectedStatuses 是 healthCheck.http.expectedStatuses 的默认值（协议 §3.4）。
var DefaultExpectedStatuses = []int32{200}

// ApplyDefaults 把协议声明的默认值填入 ConfigSet（原地修改）。
// 加载器不调用它；由编译层在语义校验前调用（T3）。
func ApplyDefaults(cs *ConfigSet) {
	if cs.Gateway == nil {
		cs.Gateway = &Gateway{
			APIVersion: APIVersionV1Alpha1,
			Kind:       KindGateway,
			Metadata:   ObjectMeta{Name: DefaultGatewayName},
		}
	}
	g := &cs.Gateway.Spec
	if g.HTTP == nil {
		g.HTTP = &HTTPDefaults{}
	}
	if g.HTTP.IdleTimeout == nil {
		g.HTTP.IdleTimeout = &Duration{DefaultIdleTimeout}
	}
	if g.HTTP.MaxRequestHeadersKB == nil {
		v := int32(DefaultMaxRequestHeadersKB)
		g.HTTP.MaxRequestHeadersKB = &v
	}
	if g.HTTP.ServerHeader == nil {
		s := DefaultServerHeader
		g.HTTP.ServerHeader = &s
	}

	for _, l := range cs.Listeners {
		s := &l.Spec
		if s.Address == "" {
			s.Address = DefaultListenerAddress
		}
		if s.TLS != nil {
			if s.TLS.MinVersion == "" {
				s.TLS.MinVersion = DefaultTLSMinVersion
			}
			if len(s.TLS.ALPN) == 0 {
				s.TLS.ALPN = append([]string(nil), DefaultALPN...)
			}
		}
		if s.Protocol == ProtocolHTTP || s.Protocol == ProtocolHTTPS {
			if s.HTTP == nil {
				s.HTTP = &ListenerHTTP{}
			}
			if s.HTTP.HTTP2 == nil {
				// 默认 true（HTTPS）；HTTP 明文默认 false（协议 §3.2）。
				v := s.Protocol == ProtocolHTTPS
				s.HTTP.HTTP2 = &v
			}
		}
	}

	for _, r := range cs.Routes {
		for i := range r.Spec.Rules {
			if r.Spec.Rules[i].Timeout == nil {
				r.Spec.Rules[i].Timeout = &Duration{DefaultRuleTimeout}
			}
		}
	}

	for _, u := range cs.Upstreams {
		s := &u.Spec
		if s.DNS != nil && s.DNS.Resolution == "" {
			s.DNS.Resolution = DefaultDNSResolution
		}
		if s.LoadBalancer == nil {
			s.LoadBalancer = &LoadBalancer{}
		}
		if s.LoadBalancer.Policy == "" {
			s.LoadBalancer.Policy = DefaultLBPolicy
		}
		if s.HealthCheck != nil && s.HealthCheck.HTTP != nil && len(s.HealthCheck.HTTP.ExpectedStatuses) == 0 {
			s.HealthCheck.HTTP.ExpectedStatuses = append([]int32(nil), DefaultExpectedStatuses...)
		}
		if s.Connection == nil {
			s.Connection = &UpstreamConnection{}
		}
		if s.Connection.ConnectTimeout == nil {
			s.Connection.ConnectTimeout = &Duration{DefaultConnectTimeout}
		}
	}

	for _, p := range cs.Policies {
		applyRateLimitDefaults(p.Spec.RateLimit)
	}
	for _, l := range cs.Listeners {
		for i := range l.Spec.Policies {
			if l.Spec.Policies[i].Inline != nil {
				applyRateLimitDefaults(l.Spec.Policies[i].Inline.RateLimit)
			}
		}
	}
	for _, r := range cs.Routes {
		for i := range r.Spec.Policies {
			if r.Spec.Policies[i].Inline != nil {
				applyRateLimitDefaults(r.Spec.Policies[i].Inline.RateLimit)
			}
		}
		for j := range r.Spec.Rules {
			for i := range r.Spec.Rules[j].Policies {
				if r.Spec.Rules[j].Policies[i].Inline != nil {
					applyRateLimitDefaults(r.Spec.Rules[j].Policies[i].Inline.RateLimit)
				}
			}
		}
	}
	if cs.Gateway != nil {
		for i := range cs.Gateway.Spec.Policies {
			if cs.Gateway.Spec.Policies[i].Inline != nil {
				applyRateLimitDefaults(cs.Gateway.Spec.Policies[i].Inline.RateLimit)
			}
		}
	}
}

// applyRateLimitDefaults 填充 rateLimit 的默认值：burst 默认 = requests，key 默认 clientIP（协议 §3.5）。
func applyRateLimitDefaults(r *RateLimitPolicy) {
	if r == nil {
		return
	}
	if r.Burst == nil {
		v := r.Requests
		r.Burst = &v
	}
	if r.Key == "" {
		r.Key = RateLimitKeyClientIP
	}
}
