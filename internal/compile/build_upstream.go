package compile

import (
	"fmt"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	upstreamhttpv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// buildUpstream 把一个 Upstream 翻译为 Cluster（含内联 ClusterLoadAssignment，
// 编译层 §3 Builder 表：endpoints→STATIC、dns→LOGICAL/STRICT_DNS）。
// k8s 来源（P2）在 F2 已拦截；本函数防御性报错。
func buildUpstream(u *protocol.Upstream) (*clusterv3.Cluster, []CompileError) {
	s := &u.Spec
	c := &clusterv3.Cluster{
		Name:           clusterResourceName(u.Metadata.Name),
		ConnectTimeout: durationpb.New(s.Connection.ConnectTimeout.Duration), // F2 已填默认值
	}
	switch {
	case len(s.Endpoints) > 0:
		c.ClusterDiscoveryType = &clusterv3.Cluster_Type{Type: clusterv3.Cluster_STATIC}
		c.LoadAssignment = buildCLA(u)
	case s.DNS != nil:
		typ := clusterv3.Cluster_LOGICAL_DNS
		if s.DNS.Resolution == protocol.DNSResolutionStrict {
			typ = clusterv3.Cluster_STRICT_DNS
		}
		c.ClusterDiscoveryType = &clusterv3.Cluster_Type{Type: typ}
		c.LoadAssignment = buildCLA(u)
	default:
		return nil, []CompileError{buildError(u.Origin, protocol.KindUpstream, u.Metadata.Name,
			"spec.kubernetesService", "kubernetesService endpoint source is not implemented in M0 (P2)")}
	}

	// loadBalancer 策略映射（F2 已填默认值 roundRobin）。
	switch s.LoadBalancer.Policy {
	case protocol.LBPolicyLeastRequest:
		c.LbPolicy = clusterv3.Cluster_LEAST_REQUEST
	case protocol.LBPolicyRandom:
		c.LbPolicy = clusterv3.Cluster_RANDOM
	case protocol.LBPolicyRingHash:
		c.LbPolicy = clusterv3.Cluster_RING_HASH
		c.LbConfig = &clusterv3.Cluster_RingHashLbConfig_{RingHashLbConfig: &clusterv3.Cluster_RingHashLbConfig{}}
	case protocol.LBPolicyMaglev:
		c.LbPolicy = clusterv3.Cluster_MAGLEV
		c.LbConfig = &clusterv3.Cluster_MaglevLbConfig_{MaglevLbConfig: &clusterv3.Cluster_MaglevLbConfig{}}
	default: // roundRobin
		c.LbPolicy = clusterv3.Cluster_ROUND_ROBIN
	}

	if hc := buildHealthCheck(s.HealthCheck); hc != nil {
		c.HealthChecks = []*corev3.HealthCheck{hc}
	}
	if s.TLS != nil && s.TLS.Enabled {
		ts, err := upstreamTLSSocket(s.TLS, s.DNS)
		if err != nil {
			return nil, []CompileError{buildError(u.Origin, protocol.KindUpstream, u.Metadata.Name,
				"spec.tls", "%v", err)}
		}
		c.TransportSocket = ts
	}
	if s.Connection.HTTP2 {
		cfg, err := marshalAny(&upstreamhttpv3.HttpProtocolOptions{
			UpstreamProtocolOptions: &upstreamhttpv3.HttpProtocolOptions_ExplicitHttpConfig_{
				ExplicitHttpConfig: &upstreamhttpv3.HttpProtocolOptions_ExplicitHttpConfig{
					ProtocolConfig: &upstreamhttpv3.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
						Http2ProtocolOptions: &corev3.Http2ProtocolOptions{},
					},
				},
			},
		})
		if err != nil {
			return nil, []CompileError{buildError(u.Origin, protocol.KindUpstream, u.Metadata.Name,
				"spec.connection.http2", "marshal HttpProtocolOptions: %v", err)}
		}
		c.TypedExtensionProtocolOptions = map[string]*anypb.Any{
			"envoy.extensions.upstreams.http.v3.HttpProtocolOptions": cfg,
		}
	}
	// 熔断字段（P1）：结构预留、值透传，不设计专项测试（T4 任务书）。
	if s.Connection.MaxConnections != nil || s.Connection.MaxPendingRequests != nil {
		th := &clusterv3.CircuitBreakers_Thresholds{Priority: corev3.RoutingPriority_DEFAULT}
		if s.Connection.MaxConnections != nil {
			th.MaxConnections = wrapperspb.UInt32(uint32(*s.Connection.MaxConnections))
		}
		if s.Connection.MaxPendingRequests != nil {
			th.MaxPendingRequests = wrapperspb.UInt32(uint32(*s.Connection.MaxPendingRequests))
		}
		c.CircuitBreakers = &clusterv3.CircuitBreakers{Thresholds: []*clusterv3.CircuitBreakers_Thresholds{th}}
	}
	return c, nil
}

// buildCLA 生成内联 ClusterLoadAssignment：endpoints 全量端点（weight 透传）；
// dns 单端点（hostname:port）。逻辑资源统一内联，EDS 拆分属 F5 形态化。
func buildCLA(u *protocol.Upstream) *endpointv3.ClusterLoadAssignment {
	cla := &endpointv3.ClusterLoadAssignment{ClusterName: clusterResourceName(u.Metadata.Name)}
	locality := &endpointv3.LocalityLbEndpoints{}
	if dns := u.Spec.DNS; dns != nil {
		locality.LbEndpoints = append(locality.LbEndpoints, lbEndpoint(dns.Hostname, dns.Port, nil))
	} else {
		for _, e := range u.Spec.Endpoints {
			locality.LbEndpoints = append(locality.LbEndpoints, lbEndpoint(e.Address, e.Port, e.Weight))
		}
	}
	cla.Endpoints = []*endpointv3.LocalityLbEndpoints{locality}
	return cla
}

// lbEndpoint 生成一个 LbEndpoint（weight 为 nil 时不设置，取 Envoy 默认 1）。
func lbEndpoint(address string, port int32, weight *int32) *endpointv3.LbEndpoint {
	ep := &endpointv3.LbEndpoint{
		HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
			Endpoint: &endpointv3.Endpoint{Address: socketAddress(address, port)},
		},
	}
	if weight != nil {
		ep.LoadBalancingWeight = wrapperspb.UInt32(uint32(*weight))
	}
	return ep
}

// buildHealthCheck 翻译主动健康检查（http/tcp 二选一，F1 已校验）。
// interval/timeout/threshold 协议未声明默认值：缺省不设置，由 F6 PGV 把关（记录于 T4 进展）。
func buildHealthCheck(h *protocol.HealthCheck) *corev3.HealthCheck {
	if h == nil {
		return nil
	}
	hc := &corev3.HealthCheck{}
	if h.Timeout != nil {
		hc.Timeout = durationpb.New(h.Timeout.Duration)
	}
	if h.Interval != nil {
		hc.Interval = durationpb.New(h.Interval.Duration)
	}
	if h.HealthyThreshold != nil {
		hc.HealthyThreshold = wrapperspb.UInt32(uint32(*h.HealthyThreshold))
	}
	if h.UnhealthyThreshold != nil {
		hc.UnhealthyThreshold = wrapperspb.UInt32(uint32(*h.UnhealthyThreshold))
	}
	switch {
	case h.HTTP != nil:
		hhc := &corev3.HealthCheck_HttpHealthCheck{Path: h.HTTP.Path}
		for _, s := range h.HTTP.ExpectedStatuses { // F2 已填默认值 [200]
			hhc.ExpectedStatuses = append(hhc.ExpectedStatuses, &typev3.Int64Range{
				Start: int64(s), End: int64(s) + 1, // Int64Range 右开区间
			})
		}
		hc.HealthChecker = &corev3.HealthCheck_HttpHealthCheck_{HttpHealthCheck: hhc}
	case h.TCP != nil:
		hc.HealthChecker = &corev3.HealthCheck_TcpHealthCheck_{TcpHealthCheck: &corev3.HealthCheck_TcpHealthCheck{}}
	}
	return hc
}

// upstreamTLSSocket 生成对上游的 TLS transport socket：
// sni 缺省取 dns.hostname（协议 §3.4）；caFile 省略 = 系统 CA（不设 TrustedCa）；
// insecureSkipVerify → trust_chain_verification = ACCEPT_UNTRUSTED（此时必须给出
// ValidationContext 承载该标志，信任链校验被显式放宽）。
func upstreamTLSSocket(t *protocol.UpstreamTLS, dns *protocol.DNSEndpointSource) (*corev3.TransportSocket, error) {
	sni := t.SNI
	if sni == "" && dns != nil {
		sni = dns.Hostname
	}
	up := &tlsv3.UpstreamTlsContext{Sni: sni}
	if t.CAFile != "" || t.InsecureSkipVerify {
		vc := &tlsv3.CertificateValidationContext{}
		if t.CAFile != "" {
			vc.TrustedCa = &corev3.DataSource{
				Specifier: &corev3.DataSource_Filename{Filename: t.CAFile},
			}
		}
		if t.InsecureSkipVerify {
			vc.TrustChainVerification = tlsv3.CertificateValidationContext_ACCEPT_UNTRUSTED
		}
		up.CommonTlsContext = &tlsv3.CommonTlsContext{
			ValidationContextType: &tlsv3.CommonTlsContext_ValidationContext{ValidationContext: vc},
		}
	}
	cfg, err := marshalAny(up)
	if err != nil {
		return nil, fmt.Errorf("marshal UpstreamTlsContext: %w", err)
	}
	return &corev3.TransportSocket{
		Name:       tlsTransportSocketName,
		ConfigType: &corev3.TransportSocket_TypedConfig{TypedConfig: cfg},
	}, nil
}
