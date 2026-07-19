package protocol

import (
	"testing"
	"time"
)

func TestApplyDefaults(t *testing.T) {
	cs, errs := LoadDir("testdata/s1")
	if len(errs) != 0 {
		t.Fatalf("load S1: %v", errs)
	}
	ApplyDefaults(cs)

	// Gateway 省略时创建默认实例。
	if cs.Gateway == nil || cs.Gateway.Metadata.Name != DefaultGatewayName {
		t.Fatalf("gateway: %+v", cs.Gateway)
	}
	if cs.Gateway.Spec.HTTP.IdleTimeout.Duration != 60*time.Second ||
		*cs.Gateway.Spec.HTTP.MaxRequestHeadersKB != 60 ||
		*cs.Gateway.Spec.HTTP.ServerHeader != "esgw" {
		t.Fatalf("gateway http defaults: %+v", cs.Gateway.Spec.HTTP)
	}

	for _, l := range cs.Listeners {
		if l.Spec.Address != "0.0.0.0" {
			t.Fatalf("listener %s address: %q", l.Metadata.Name, l.Spec.Address)
		}
		wantHTTP2 := l.Spec.Protocol == ProtocolHTTPS
		if *l.Spec.HTTP.HTTP2 != wantHTTP2 {
			t.Fatalf("listener %s http2: want %v", l.Metadata.Name, wantHTTP2)
		}
	}
	https := cs.Listeners[0]
	if https.Spec.TLS.MinVersion != TLSVersion12 ||
		len(https.Spec.TLS.ALPN) != 2 || https.Spec.TLS.ALPN[0] != "h2" {
		t.Fatalf("tls defaults: %+v", https.Spec.TLS)
	}

	for _, r := range cs.Routes {
		for i, rule := range r.Spec.Rules {
			if rule.Timeout == nil || rule.Timeout.Duration != 15*time.Second {
				t.Fatalf("route %s rule[%d] timeout: %+v", r.Metadata.Name, i, rule.Timeout)
			}
		}
	}

	for _, u := range cs.Upstreams {
		if u.Spec.LoadBalancer.Policy != LBPolicyRoundRobin {
			t.Fatalf("upstream %s lb policy: %q", u.Metadata.Name, u.Spec.LoadBalancer.Policy)
		}
		if u.Spec.Connection.ConnectTimeout.Duration != 5*time.Second {
			t.Fatalf("upstream %s connectTimeout: %v", u.Metadata.Name, u.Spec.Connection.ConnectTimeout)
		}
	}
}

func TestApplyDefaultsS2(t *testing.T) {
	cs, errs := LoadDir("testdata/s2")
	if len(errs) != 0 {
		t.Fatalf("load S2: %v", errs)
	}
	ApplyDefaults(cs)

	// 内联 rateLimit：burst 默认 = requests，key 默认 clientIP（S2 显式给了 key）。
	login := cs.Routes[0].Spec.Rules[0]
	rl := login.Policies[1].Inline.RateLimit
	if rl.Burst == nil || *rl.Burst != 10 {
		t.Fatalf("inline rateLimit burst: %+v", rl)
	}

	// 显式 timeout 不被覆盖。
	if cs.Routes[0].Spec.Rules[1].Timeout.Duration != 10*time.Second {
		t.Fatalf("users timeout: %v", cs.Routes[0].Spec.Rules[1].Timeout)
	}
	if cs.Routes[0].Spec.Rules[0].Timeout.Duration != 15*time.Second {
		t.Fatalf("login timeout default: %v", cs.Routes[0].Spec.Rules[0].Timeout)
	}

	order := cs.Upstreams[0]
	if got := order.Spec.HealthCheck.HTTP.ExpectedStatuses; len(got) != 1 || got[0] != 200 {
		t.Fatalf("expectedStatuses: %v", got)
	}
	if order.Spec.DNS.Resolution != DNSResolutionLogical {
		t.Fatalf("dns resolution: %q", order.Spec.DNS.Resolution)
	}
	// 显式 connectTimeout 缺省 → 5s；显式 http2 保留。
	if order.Spec.Connection.ConnectTimeout.Duration != 5*time.Second || !order.Spec.Connection.HTTP2 {
		t.Fatalf("connection: %+v", order.Spec.Connection)
	}
}
