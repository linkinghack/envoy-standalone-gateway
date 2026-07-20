package compile

import (
	"testing"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// TestDeterministicCompile 确定性（编译层 §5，A6）：同一 ConfigSet + 同一目标模式
// 两次 Compile，IR.Version 与确定性序列化产物字节一致。
// 配置覆盖：多 Listener（含 HTTPS 双证书）、多 Route/Upstream、envoyPatch、
// EnvoyResources——即编译产物的全部来源。
func TestDeterministicCompile(t *testing.T) {
	dir := t.TempDir()
	certA, keyA := writeSelfSignedCert(t, dir, "www", "www.example.com")
	certB, keyB := writeSelfSignedCert(t, dir, "api", "api.example.com")

	build := func() *protocol.ConfigSet {
		https := newTLSListener("https", 443, protocol.ProtocolHTTPS, certA, keyA)
		https.Spec.TLS.Certificates = append(https.Spec.TLS.Certificates,
			protocol.Certificate{CertFile: certB, KeyFile: keyB})
		app := newUpstream("app")
		app.Spec.EnvoyPatch = []protocol.EnvoyPatch{
			mergePatch("cluster", `{"upstream_connection_options": {"tcp_keepalive": {"keepalive_time": 60}}}`),
		}
		return &protocol.ConfigSet{
			Listeners: []*protocol.Listener{
				https,
				newListener("http", 80, protocol.ProtocolHTTP),
			},
			Routes: []*protocol.Route{
				newHTTPRoute("www", []string{"https"}, []string{"www.example.com"}, "app"),
				newHTTPRoute("api", []string{"https", "http"}, []string{"api.example.com"}, "app"),
				newHTTPRoute("redir", []string{"http"}, nil, "app"),
			},
			Upstreams: []*protocol.Upstream{app, newUpstream("app2")},
			EnvoyResources: []*protocol.EnvoyResources{envoyResourcesObj("extra", false, `{
				"@type": "type.googleapis.com/envoy.config.cluster.v3.Cluster",
				"name": "legacy",
				"connect_timeout": "2s",
				"type": "STRICT_DNS",
				"load_assignment": {"cluster_name": "legacy", "endpoints": [{"lb_endpoints": [{"endpoint": {"address": {"socket_address": {"address": "legacy.internal", "port_value": 80}}}}]}]}
			}`)},
		}
	}

	for _, mode := range []Mode{ModeStatic, ModeXDS} {
		t.Run(string(mode), func(t *testing.T) {
			first, errs := Compile(build(), Options{Mode: mode})
			assertNoErrs(t, errs)
			second, errs := Compile(build(), Options{Mode: mode})
			assertNoErrs(t, errs)

			if first.Version != second.Version {
				t.Fatalf("Version unstable: %q vs %q", first.Version, second.Version)
			}
			b1, err := first.MarshalDeterministic()
			if err != nil {
				t.Fatal(err)
			}
			b2, err := second.MarshalDeterministic()
			if err != nil {
				t.Fatal(err)
			}
			if string(b1) != string(b2) {
				t.Fatal("deterministic serialization differs between two compiles")
			}
		})
	}
}
