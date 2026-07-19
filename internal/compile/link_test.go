package compile

import (
	"testing"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// TestLinkDanglingReferences 覆盖引用解析：Route→Listener、backends/forward→Upstream、
// 四级 policies 字符串引用→Policy，悬空引用报错且 Path 精确到字段。
func TestLinkDanglingReferences(t *testing.T) {
	t.Run("route listeners dangling", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Routes:    []*protocol.Route{newHTTPRoute("r1", []string{"ghost"}, nil, "app")},
			Upstreams: []*protocol.Upstream{newUpstream("app")},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindRoute, name: "r1", path: "spec.listeners[0]", msg: `listener "ghost" not found`},
		})
	})

	t.Run("rule backend upstream dangling", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newListener("http", 80, protocol.ProtocolHTTP)},
			Routes:    []*protocol.Route{newHTTPRoute("r1", []string{"http"}, nil, "ghost")},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindRoute, name: "r1", path: "spec.rules[0].backends[0].upstream", msg: `upstream "ghost" not found`},
		})
	})

	t.Run("forward upstream dangling", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newListener("tcp", 3306, protocol.ProtocolTCP)},
			Routes:    []*protocol.Route{newForwardRoute("r1", []string{"tcp"}, "ghost")},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindRoute, name: "r1", path: "spec.forward.upstream", msg: `upstream "ghost" not found`},
		})
	})

	t.Run("policy refs dangling at four levels", func(t *testing.T) {
		gw := &protocol.Gateway{
			APIVersion: protocol.APIVersionV1Alpha1,
			Kind:       protocol.KindGateway,
			Metadata:   protocol.ObjectMeta{Name: protocol.DefaultGatewayName},
			Spec:       protocol.GatewaySpec{Policies: []protocol.PolicyAttachment{{Ref: "p-gw"}}},
			Origin:     testOrigin(),
		}
		lis := newListener("http", 80, protocol.ProtocolHTTP)
		lis.Spec.Policies = []protocol.PolicyAttachment{{Ref: "p-lis"}}
		route := newHTTPRoute("r1", []string{"http"}, nil, "app")
		route.Spec.Policies = []protocol.PolicyAttachment{{Ref: "p-route"}}
		route.Spec.Rules[0].Policies = []protocol.PolicyAttachment{{Ref: "p-rule"}}
		cs := &protocol.ConfigSet{
			Gateway:   gw,
			Listeners: []*protocol.Listener{lis},
			Routes:    []*protocol.Route{route},
			Upstreams: []*protocol.Upstream{newUpstream("app")},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindGateway, name: "default", path: "spec.policies[0]", msg: `policy "p-gw" not found`},
			{kind: protocol.KindListener, name: "http", path: "spec.policies[0]", msg: `policy "p-lis" not found`},
			{kind: protocol.KindRoute, name: "r1", path: "spec.policies[0]", msg: `policy "p-route" not found`},
			{kind: protocol.KindRoute, name: "r1", path: "spec.rules[0].policies[0]", msg: `policy "p-rule" not found`},
		})
	})

	t.Run("boundary: all references resolved", func(t *testing.T) {
		p := newPolicy("rl", rateLimitSpec())
		gw := &protocol.Gateway{
			APIVersion: protocol.APIVersionV1Alpha1,
			Kind:       protocol.KindGateway,
			Metadata:   protocol.ObjectMeta{Name: protocol.DefaultGatewayName},
			Spec:       protocol.GatewaySpec{Policies: []protocol.PolicyAttachment{{Ref: "rl"}}},
			Origin:     testOrigin(),
		}
		lis := newListener("http", 80, protocol.ProtocolHTTP)
		lis.Spec.Policies = []protocol.PolicyAttachment{{Ref: "rl"}}
		route := newHTTPRoute("r1", []string{"http"}, []string{"www.example.com"}, "app")
		route.Spec.Policies = []protocol.PolicyAttachment{{Ref: "rl"}}
		route.Spec.Rules[0].Policies = []protocol.PolicyAttachment{{Ref: "rl"}, {Inline: &protocol.PolicySpec{
			HeaderModifier: &protocol.HeaderModifierPolicy{},
		}}}
		cs := &protocol.ConfigSet{
			Gateway:   gw,
			Listeners: []*protocol.Listener{lis},
			Routes:    []*protocol.Route{route},
			Upstreams: []*protocol.Upstream{newUpstream("app")},
			Policies:  []*protocol.Policy{p},
		}
		assertNoErrs(t, linkErrs(cs))
	})
}

// TestListenerAddressUnique 覆盖 Listener address:port 唯一性（协议 §3.2）。
func TestListenerAddressUnique(t *testing.T) {
	t.Run("duplicate default address", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{
				newListener("a", 80, protocol.ProtocolHTTP),
				newListener("b", 80, protocol.ProtocolHTTP),
			},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindListener, name: "b", path: "spec.port", msg: `"0.0.0.0:80" conflicts with listener "a"`},
		})
	})

	t.Run("duplicate explicit ipv6 address", func(t *testing.T) {
		mk := func(name string) *protocol.Listener {
			l := newListener(name, 443, protocol.ProtocolTCP)
			l.Spec.Address = "::"
			return l
		}
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{mk("a"), mk("b")}}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindListener, name: "b", path: "spec.port", msg: `"[::]:443" conflicts with listener "a"`},
		})
	})

	t.Run("boundary: same port different address", func(t *testing.T) {
		a := newListener("a", 80, protocol.ProtocolHTTP)
		b := newListener("b", 80, protocol.ProtocolHTTP)
		b.Spec.Address = "127.0.0.1"
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{a, b}}
		assertNoErrs(t, linkErrs(cs))
	})

	t.Run("boundary: same address different port", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{
				newListener("a", 80, protocol.ProtocolHTTP),
				newListener("b", 8080, protocol.ProtocolHTTP),
			},
		}
		assertNoErrs(t, linkErrs(cs))
	})
}

// TestRouteForm 覆盖 Route 形态与 Listener 协议匹配（协议 §3.3.5）。
func TestRouteForm(t *testing.T) {
	t.Run("rules route on L4 listener", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newListener("tcp", 3306, protocol.ProtocolTCP)},
			Routes:    []*protocol.Route{newHTTPRoute("r1", []string{"tcp"}, nil, "app")},
			Upstreams: []*protocol.Upstream{newUpstream("app")},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindRoute, name: "r1", path: "spec.listeners[0]", msg: `listener "tcp" is TCP (L4-class) but route uses rules (HTTP form)`},
		})
	})

	t.Run("forward route on HTTP listener", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newListener("http", 80, protocol.ProtocolHTTP)},
			Routes:    []*protocol.Route{newForwardRoute("r1", []string{"http"}, "app")},
			Upstreams: []*protocol.Upstream{newUpstream("app")},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindRoute, name: "r1", path: "spec.listeners[0]", msg: `listener "http" is HTTP (HTTP-class) but route uses forward (L4 form)`},
		})
	})

	t.Run("rules and forward both set", func(t *testing.T) {
		r := newHTTPRoute("r1", []string{"tcp"}, nil, "app")
		r.Spec.Forward = &protocol.Forward{Upstream: "app"}
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newListener("tcp", 3306, protocol.ProtocolTCP)},
			Routes:    []*protocol.Route{r},
			Upstreams: []*protocol.Upstream{newUpstream("app")},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindRoute, name: "r1", path: "spec.forward", msg: "mutually exclusive"},
		})
	})

	t.Run("neither rules nor forward", func(t *testing.T) {
		r := newHTTPRoute("r1", []string{"http"}, nil, "app")
		r.Spec.Rules = nil
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newListener("http", 80, protocol.ProtocolHTTP)},
			Routes:    []*protocol.Route{r},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindRoute, name: "r1", path: "spec", msg: "either rules (HTTP form) or forward (L4 form)"},
		})
	})

	t.Run("sniHosts on non-TLS listener", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newListener("tcp", 3306, protocol.ProtocolTCP)},
			Routes:    []*protocol.Route{newForwardRoute("r1", []string{"tcp"}, "app", "db.example.com")},
			Upstreams: []*protocol.Upstream{newUpstream("app")},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindRoute, name: "r1", path: "spec.forward.sniHosts", msg: "only allowed on listeners with protocol TLS"},
		})
	})

	t.Run("boundary: forms match protocols", func(t *testing.T) {
		tlsLis := newTLSListener("tls", 8443, protocol.ProtocolTLS, "/x.crt", "/x.key")
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{
				newListener("http", 80, protocol.ProtocolHTTP),
				newTLSListener("https", 443, protocol.ProtocolHTTPS, "/x.crt", "/x.key"),
				newListener("tcp", 3306, protocol.ProtocolTCP),
				newListener("udp", 53, protocol.ProtocolUDP),
				tlsLis,
			},
			Routes: []*protocol.Route{
				newHTTPRoute("r-http", []string{"http", "https"}, nil, "app"),
				newForwardRoute("r-tcp", []string{"tcp", "udp"}, "app"),
				newForwardRoute("r-tls", []string{"tls"}, "app", "db.example.com"),
			},
			Upstreams: []*protocol.Upstream{newUpstream("app")},
		}
		assertNoErrs(t, linkErrs(cs))
	})
}

// TestHostnameConflicts 覆盖同 Listener 下 hostname 同精度冲突（协议 §3.3 要点 1）。
func TestHostnameConflicts(t *testing.T) {
	listeners := func() []*protocol.Listener {
		return []*protocol.Listener{
			newListener("http", 80, protocol.ProtocolHTTP),
			newListener("http2", 8080, protocol.ProtocolHTTP),
		}
	}
	upstreams := func() []*protocol.Upstream { return []*protocol.Upstream{newUpstream("app")} }

	t.Run("exact duplicate", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: listeners(),
			Routes: []*protocol.Route{
				newHTTPRoute("r1", []string{"http"}, []string{"www.example.com"}, "app"),
				newHTTPRoute("r2", []string{"http"}, []string{"www.example.com"}, "app"),
			},
			Upstreams: upstreams(),
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindRoute, name: "r2", path: "spec.hostnames[0]", msg: `"www.example.com" on listener "http" conflicts with route "r1"`},
		})
	})

	t.Run("fallback duplicate", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: listeners(),
			Routes: []*protocol.Route{
				newHTTPRoute("r1", []string{"http"}, nil, "app"),
				newHTTPRoute("r2", []string{"http"}, nil, "app"),
			},
			Upstreams: upstreams(),
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindRoute, name: "r2", path: "spec.hostnames", msg: `"*" on listener "http" conflicts with route "r1"`},
		})
	})

	t.Run("wildcard duplicate", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: listeners(),
			Routes: []*protocol.Route{
				newHTTPRoute("r1", []string{"http"}, []string{"*.example.com"}, "app"),
				newHTTPRoute("r2", []string{"http"}, []string{"*.example.com"}, "app"),
			},
			Upstreams: upstreams(),
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindRoute, name: "r2", path: "spec.hostnames[0]", msg: "conflicts with route \"r1\""},
		})
	})

	t.Run("case-insensitive duplicate", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: listeners(),
			Routes: []*protocol.Route{
				newHTTPRoute("r1", []string{"http"}, []string{"WWW.Example.com"}, "app"),
				newHTTPRoute("r2", []string{"http"}, []string{"www.example.com"}, "app"),
			},
			Upstreams: upstreams(),
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindRoute, name: "r2", path: "spec.hostnames[0]", msg: "conflicts with route \"r1\""},
		})
	})

	t.Run("boundary: distinct precisions coexist", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: listeners(),
			Routes: []*protocol.Route{
				newHTTPRoute("exact", []string{"http"}, []string{"api.example.com"}, "app"),
				newHTTPRoute("wild", []string{"http"}, []string{"*.example.com"}, "app"),
				newHTTPRoute("fallback", []string{"http"}, nil, "app"),
			},
			Upstreams: upstreams(),
		}
		assertNoErrs(t, linkErrs(cs))
	})

	t.Run("boundary: same hostname on different listeners", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: listeners(),
			Routes: []*protocol.Route{
				newHTTPRoute("r1", []string{"http"}, []string{"www.example.com"}, "app"),
				newHTTPRoute("r2", []string{"http2"}, []string{"www.example.com"}, "app"),
			},
			Upstreams: upstreams(),
		}
		assertNoErrs(t, linkErrs(cs))
	})
}

// TestPolicyAttachmentLevels 覆盖策略挂接层级合法性（编译层 §3 F2）：
// ipAccess 不可挂 rule 级。
func TestPolicyAttachmentLevels(t *testing.T) {
	t.Run("ipAccess ref at rule level", func(t *testing.T) {
		route := newHTTPRoute("r1", []string{"http"}, nil, "app")
		route.Spec.Rules[0].Policies = []protocol.PolicyAttachment{{Ref: "acl"}}
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newListener("http", 80, protocol.ProtocolHTTP)},
			Routes:    []*protocol.Route{route},
			Upstreams: []*protocol.Upstream{newUpstream("app")},
			Policies:  []*protocol.Policy{newPolicy("acl", ipAccessSpec())},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindRoute, name: "r1", path: "spec.rules[0].policies[0]", msg: "ipAccess policy cannot be attached at rule level"},
		})
	})

	t.Run("ipAccess inline at rule level", func(t *testing.T) {
		spec := ipAccessSpec()
		route := newHTTPRoute("r1", []string{"http"}, nil, "app")
		route.Spec.Rules[0].Policies = []protocol.PolicyAttachment{{Inline: &spec}}
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newListener("http", 80, protocol.ProtocolHTTP)},
			Routes:    []*protocol.Route{route},
			Upstreams: []*protocol.Upstream{newUpstream("app")},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindRoute, name: "r1", path: "spec.rules[0].policies[0]", msg: "cannot be attached at rule level"},
		})
	})

	t.Run("boundary: ipAccess at gateway/listener/route levels", func(t *testing.T) {
		spec := ipAccessSpec()
		gw := &protocol.Gateway{
			APIVersion: protocol.APIVersionV1Alpha1,
			Kind:       protocol.KindGateway,
			Metadata:   protocol.ObjectMeta{Name: protocol.DefaultGatewayName},
			Spec:       protocol.GatewaySpec{Policies: []protocol.PolicyAttachment{{Ref: "acl"}}},
			Origin:     testOrigin(),
		}
		lis := newListener("http", 80, protocol.ProtocolHTTP)
		lis.Spec.Policies = []protocol.PolicyAttachment{{Inline: &spec}}
		route := newHTTPRoute("r1", []string{"http"}, nil, "app")
		route.Spec.Policies = []protocol.PolicyAttachment{{Ref: "acl"}}
		route.Spec.Rules[0].Policies = []protocol.PolicyAttachment{{Ref: "rl"}}
		cs := &protocol.ConfigSet{
			Gateway:   gw,
			Listeners: []*protocol.Listener{lis},
			Routes:    []*protocol.Route{route},
			Upstreams: []*protocol.Upstream{newUpstream("app")},
			Policies: []*protocol.Policy{
				newPolicy("acl", ipAccessSpec()),
				newPolicy("rl", rateLimitSpec()),
			},
		}
		assertNoErrs(t, linkErrs(cs))
	})
}

// TestTLSRules 覆盖 TLS 必配/禁配（协议 §3.2）。
func TestTLSRules(t *testing.T) {
	t.Run("HTTPS without tls", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newListener("https", 443, protocol.ProtocolHTTPS)},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindListener, name: "https", path: "spec.tls", msg: "required for protocol HTTPS"},
		})
	})

	t.Run("TLS passthrough without tls", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newListener("tls", 8443, protocol.ProtocolTLS)},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindListener, name: "tls", path: "spec.tls", msg: "required for protocol TLS"},
		})
	})

	t.Run("HTTP with tls", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newTLSListener("http", 80, protocol.ProtocolHTTP, "/x.crt", "/x.key")},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindListener, name: "http", path: "spec.tls", msg: "not allowed for protocol HTTP"},
		})
	})

	t.Run("TCP with tls", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newTLSListener("tcp", 3306, protocol.ProtocolTCP, "/x.crt", "/x.key")},
		}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindListener, name: "tcp", path: "spec.tls", msg: "not allowed for protocol TCP"},
		})
	})

	t.Run("boundary: HTTPS with tls / plain HTTP,TCP,UDP without", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{
				newTLSListener("https", 443, protocol.ProtocolHTTPS, "/x.crt", "/x.key"),
				newTLSListener("tls", 8443, protocol.ProtocolTLS, "/x.crt", "/x.key"),
				newListener("http", 80, protocol.ProtocolHTTP),
				newListener("tcp", 3306, protocol.ProtocolTCP),
				newListener("udp", 53, protocol.ProtocolUDP),
			},
		}
		assertNoErrs(t, linkErrs(cs))
	})
}

// TestKubernetesService 覆盖 kubernetesService 端点来源在 EndpointSource 未注入时报错。
func TestKubernetesService(t *testing.T) {
	k8sUpstream := func() *protocol.Upstream {
		u := newUpstream("svc")
		u.Spec.Endpoints = nil
		u.Spec.KubernetesService = &protocol.KubernetesServiceSource{
			Namespace: "default", Name: "user", Port: 8080,
		}
		return u
	}

	t.Run("EndpointSource nil", func(t *testing.T) {
		cs := &protocol.ConfigSet{Upstreams: []*protocol.Upstream{k8sUpstream()}}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindUpstream, name: "svc", path: "spec.kubernetesService", msg: "k8s service discovery is not enabled"},
		})
	})

	t.Run("boundary: EndpointSource injected", func(t *testing.T) {
		cs := &protocol.ConfigSet{Upstreams: []*protocol.Upstream{k8sUpstream()}}
		_, errs := link(cs, Options{Mode: ModeXDS, EndpointSource: fakeEndpointSource{}}, okVerifier())
		assertNoErrs(t, errs)
	})
}
