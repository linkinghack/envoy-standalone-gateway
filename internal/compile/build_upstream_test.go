package compile

import (
	"testing"
	"time"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// buildCluster 构建单个 Upstream 的 Cluster（经 F2 填默认值）。
func buildCluster(t *testing.T, u *protocol.Upstream) *clusterv3.Cluster {
	t.Helper()
	cs := &protocol.ConfigSet{Upstreams: []*protocol.Upstream{u}}
	res, errs := buildCS(t, cs)
	assertNoErrs(t, errs)
	return findCluster(t, res, "us/"+u.Metadata.Name)
}

// TestUpstreamEndpoints 覆盖 endpoints → STATIC + 内联 CLA（weight 透传）。
func TestUpstreamEndpoints(t *testing.T) {
	u := newUpstream("app")
	u.Spec.Endpoints = []protocol.Endpoint{
		{Address: "10.0.0.1", Port: 8080, Weight: ptr(int32(3))},
		{Address: "10.0.0.2", Port: 8081},
	}
	c := buildCluster(t, u)
	if c.GetType() != clusterv3.Cluster_STATIC {
		t.Fatalf("type = %v, want STATIC", c.GetType())
	}
	if c.GetConnectTimeout().GetSeconds() != 5 {
		t.Fatalf("connectTimeout = %v, want 5s（F2 默认值）", c.GetConnectTimeout())
	}
	eps := c.GetLoadAssignment().GetEndpoints()
	if c.GetLoadAssignment().GetClusterName() != "us/app" || len(eps) != 1 || len(eps[0].GetLbEndpoints()) != 2 {
		t.Fatalf("load assignment = %v", c.GetLoadAssignment())
	}
	ep0 := eps[0].GetLbEndpoints()[0]
	if ep0.GetEndpoint().GetAddress().GetSocketAddress().GetAddress() != "10.0.0.1" ||
		ep0.GetEndpoint().GetAddress().GetSocketAddress().GetPortValue() != 8080 ||
		ep0.GetLoadBalancingWeight().GetValue() != 3 {
		t.Fatalf("endpoint[0] = %v", ep0)
	}
	if eps[0].GetLbEndpoints()[1].GetLoadBalancingWeight() != nil {
		t.Fatalf("endpoint[1] weight = %v, want nil（Envoy 默认 1）", eps[0].GetLbEndpoints()[1].GetLoadBalancingWeight())
	}
}

// TestUpstreamDNS 覆盖 dns → LOGICAL_DNS（默认）/ STRICT_DNS + 单端点 CLA。
func TestUpstreamDNS(t *testing.T) {
	t.Run("logical default", func(t *testing.T) {
		u := newUpstream("app")
		u.Spec.Endpoints = nil
		u.Spec.DNS = &protocol.DNSEndpointSource{Hostname: "user.internal", Port: 8080}
		c := buildCluster(t, u)
		if c.GetType() != clusterv3.Cluster_LOGICAL_DNS {
			t.Fatalf("type = %v, want LOGICAL_DNS", c.GetType())
		}
		eps := c.GetLoadAssignment().GetEndpoints()
		if len(eps) != 1 || len(eps[0].GetLbEndpoints()) != 1 {
			t.Fatalf("load assignment = %v", c.GetLoadAssignment())
		}
		addr := eps[0].GetLbEndpoints()[0].GetEndpoint().GetAddress().GetSocketAddress()
		if addr.GetAddress() != "user.internal" || addr.GetPortValue() != 8080 {
			t.Fatalf("dns endpoint = %v", addr)
		}
	})

	t.Run("strict", func(t *testing.T) {
		u := newUpstream("app")
		u.Spec.Endpoints = nil
		u.Spec.DNS = &protocol.DNSEndpointSource{Hostname: "user.internal", Port: 8080, Resolution: protocol.DNSResolutionStrict}
		c := buildCluster(t, u)
		if c.GetType() != clusterv3.Cluster_STRICT_DNS {
			t.Fatalf("type = %v, want STRICT_DNS", c.GetType())
		}
	})
}

// TestUpstreamLBPolicy 覆盖 loadBalancer 策略映射（含 ringHash/maglev LbConfig）。
func TestUpstreamLBPolicy(t *testing.T) {
	cases := []struct {
		policy protocol.LBPolicy
		want   clusterv3.Cluster_LbPolicy
	}{
		{protocol.LBPolicyRoundRobin, clusterv3.Cluster_ROUND_ROBIN},
		{protocol.LBPolicyLeastRequest, clusterv3.Cluster_LEAST_REQUEST},
		{protocol.LBPolicyRandom, clusterv3.Cluster_RANDOM},
		{protocol.LBPolicyRingHash, clusterv3.Cluster_RING_HASH},
		{protocol.LBPolicyMaglev, clusterv3.Cluster_MAGLEV},
	}
	for _, tc := range cases {
		t.Run(string(tc.policy), func(t *testing.T) {
			u := newUpstream("app")
			u.Spec.LoadBalancer = &protocol.LoadBalancer{Policy: tc.policy}
			c := buildCluster(t, u)
			if c.GetLbPolicy() != tc.want {
				t.Fatalf("lb policy = %v, want %v", c.GetLbPolicy(), tc.want)
			}
			switch tc.policy {
			case protocol.LBPolicyRingHash:
				if c.GetRingHashLbConfig() == nil {
					t.Fatal("ringHash must set ring_hash_lb_config")
				}
			case protocol.LBPolicyMaglev:
				if c.GetMaglevLbConfig() == nil {
					t.Fatal("maglev must set maglev_lb_config")
				}
			}
		})
	}
}

// TestUpstreamHealthCheck 覆盖 http/tcp 主动健康检查。
func TestUpstreamHealthCheck(t *testing.T) {
	t.Run("http", func(t *testing.T) {
		u := newUpstream("app")
		u.Spec.HealthCheck = &protocol.HealthCheck{
			HTTP:               &protocol.HTTPHealthCheck{Path: "/healthz"},
			Interval:           &protocol.Duration{Duration: 10 * time.Second},
			Timeout:            &protocol.Duration{Duration: 2 * time.Second},
			HealthyThreshold:   ptr(int32(2)),
			UnhealthyThreshold: ptr(int32(3)),
		}
		c := buildCluster(t, u)
		if len(c.GetHealthChecks()) != 1 {
			t.Fatalf("health checks = %d, want 1", len(c.GetHealthChecks()))
		}
		hc := c.GetHealthChecks()[0]
		if hc.GetInterval().GetSeconds() != 10 || hc.GetTimeout().GetSeconds() != 2 ||
			hc.GetHealthyThreshold().GetValue() != 2 || hc.GetUnhealthyThreshold().GetValue() != 3 {
			t.Fatalf("health check = %v", hc)
		}
		hhc := hc.GetHttpHealthCheck()
		if hhc.GetPath() != "/healthz" {
			t.Fatalf("http path = %q", hhc.GetPath())
		}
		// expectedStatuses 默认 [200]（F2 填充）→ 右开区间 [200,201)。
		if len(hhc.GetExpectedStatuses()) != 1 ||
			hhc.GetExpectedStatuses()[0].GetStart() != 200 || hhc.GetExpectedStatuses()[0].GetEnd() != 201 {
			t.Fatalf("expected statuses = %v", hhc.GetExpectedStatuses())
		}
	})

	t.Run("tcp", func(t *testing.T) {
		u := newUpstream("app")
		u.Spec.HealthCheck = &protocol.HealthCheck{
			TCP:      &protocol.TCPHealthCheck{},
			Interval: &protocol.Duration{Duration: 10 * time.Second},
			Timeout:  &protocol.Duration{Duration: 2 * time.Second},
		}
		c := buildCluster(t, u)
		if c.GetHealthChecks()[0].GetTcpHealthCheck() == nil {
			t.Fatalf("health checker = %v, want tcp", c.GetHealthChecks()[0].GetHealthChecker())
		}
	})
}

// TestUpstreamTLS 覆盖上游 TLS transport socket：sni 默认/caFile/insecureSkipVerify。
func TestUpstreamTLS(t *testing.T) {
	t.Run("sni default from dns hostname", func(t *testing.T) {
		u := newUpstream("app")
		u.Spec.Endpoints = nil
		u.Spec.DNS = &protocol.DNSEndpointSource{Hostname: "user.internal", Port: 443}
		u.Spec.TLS = &protocol.UpstreamTLS{Enabled: true}
		c := buildCluster(t, u)
		ts := c.GetTransportSocket()
		if ts.GetName() != tlsTransportSocketName {
			t.Fatalf("transport socket = %q", ts.GetName())
		}
		utc := &tlsv3.UpstreamTlsContext{}
		mustUnmarshal(t, ts.GetTypedConfig(), utc)
		if utc.GetSni() != "user.internal" {
			t.Fatalf("sni = %q, want dns.hostname 默认", utc.GetSni())
		}
		if utc.GetCommonTlsContext().GetValidationContext() != nil {
			t.Fatal("no caFile/insecureSkipVerify → no validation context（系统 CA）")
		}
	})

	t.Run("explicit sni caFile insecureSkipVerify", func(t *testing.T) {
		u := newUpstream("app")
		u.Spec.TLS = &protocol.UpstreamTLS{
			Enabled: true, SNI: "svc.example.com", CAFile: "/etc/ssl/ca.crt", InsecureSkipVerify: true,
		}
		c := buildCluster(t, u)
		utc := &tlsv3.UpstreamTlsContext{}
		mustUnmarshal(t, c.GetTransportSocket().GetTypedConfig(), utc)
		if utc.GetSni() != "svc.example.com" {
			t.Fatalf("sni = %q", utc.GetSni())
		}
		vc := utc.GetCommonTlsContext().GetValidationContext()
		if vc.GetTrustedCa().GetFilename() != "/etc/ssl/ca.crt" {
			t.Fatalf("trusted ca = %v", vc.GetTrustedCa())
		}
		if vc.GetTrustChainVerification() != tlsv3.CertificateValidationContext_ACCEPT_UNTRUSTED {
			t.Fatalf("trust chain verification = %v, want ACCEPT_UNTRUSTED", vc.GetTrustChainVerification())
		}
	})

	t.Run("disabled emits no socket", func(t *testing.T) {
		u := newUpstream("app")
		u.Spec.TLS = &protocol.UpstreamTLS{Enabled: false, SNI: "ignored"}
		c := buildCluster(t, u)
		if c.GetTransportSocket() != nil {
			t.Fatal("tls.enabled=false must not emit transport socket")
		}
	})
}

// TestUpstreamConnection 覆盖 connection 参数：connectTimeout/http2/熔断透传。
func TestUpstreamConnection(t *testing.T) {
	t.Run("http2", func(t *testing.T) {
		u := newUpstream("app")
		u.Spec.Connection = &protocol.UpstreamConnection{HTTP2: true}
		c := buildCluster(t, u)
		opts := c.GetTypedExtensionProtocolOptions()
		if len(opts) != 1 || opts["envoy.extensions.upstreams.http.v3.HttpProtocolOptions"] == nil {
			t.Fatalf("typed extension protocol options = %v", opts)
		}
	})

	t.Run("explicit connectTimeout", func(t *testing.T) {
		u := newUpstream("app")
		u.Spec.Connection = &protocol.UpstreamConnection{
			ConnectTimeout: &protocol.Duration{Duration: 3 * time.Second},
		}
		c := buildCluster(t, u)
		if c.GetConnectTimeout().GetSeconds() != 3 {
			t.Fatalf("connectTimeout = %v", c.GetConnectTimeout())
		}
	})

	t.Run("circuit breakers pass-through", func(t *testing.T) {
		u := newUpstream("app")
		u.Spec.Connection = &protocol.UpstreamConnection{
			MaxConnections:     ptr(int32(1024)),
			MaxPendingRequests: ptr(int32(512)),
		}
		c := buildCluster(t, u)
		th := c.GetCircuitBreakers().GetThresholds()
		if len(th) != 1 || th[0].GetMaxConnections().GetValue() != 1024 ||
			th[0].GetMaxPendingRequests().GetValue() != 512 ||
			th[0].GetPriority() != corev3.RoutingPriority_DEFAULT {
			t.Fatalf("circuit breakers = %v", c.GetCircuitBreakers())
		}
	})
}
