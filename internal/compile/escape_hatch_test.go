package compile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"

	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// compileYAMLDir 把 testdata 文件与补充 YAML 写入临时目录后经 LoadDir+Compile 编译。
// 协议 §7.1 文档示例省略了 apiVersion（§7 语境假设信封），加载器要求显式版本，
// 故缺失时在文件头补 apiVersion: esgw/v1alpha1（对象本体保持逐字）。
func compileYAMLDir(t *testing.T, mode Mode, testdataFile, supplement string) (*ir.IR, []CompileError) {
	t.Helper()
	dir := t.TempDir()
	raw, err := os.ReadFile(testdataFile)
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if !strings.Contains(string(raw), "apiVersion:") {
		raw = append([]byte("apiVersion: esgw/v1alpha1\n"), raw...)
	}
	if err := os.WriteFile(filepath.Join(dir, "object.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "supplement.yaml"), []byte(supplement), 0o600); err != nil {
		t.Fatal(err)
	}
	cs, loadErrs := protocol.LoadDir(dir)
	if len(loadErrs) != 0 {
		t.Fatalf("load errors: %v", loadErrs)
	}
	return Compile(cs, Options{Mode: mode})
}

const p71Supplement = `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: http}
spec: {port: 80, protocol: HTTP}
---
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: api}
spec:
  listeners: [http]
  rules:
    - match: {path: {prefix: /}}
      backends: [{upstream: user-svc}]
`

// TestDocExample71EnvoyPatch 协议 §7.1 文档示例逐字（正例）：
// merge 合入 upstream_connection_options.tcp_keepalive，jsonPatch 替换 dns_lookup_family。
func TestDocExample71EnvoyPatch(t *testing.T) {
	for _, mode := range []Mode{ModeStatic, ModeXDS} {
		t.Run(string(mode), func(t *testing.T) {
			out, errs := compileYAMLDir(t, mode, "testdata/escape-hatch/patch-upstream.yaml", p71Supplement)
			assertNoErrs(t, errs)
			cl := out.Clusters["us/user-svc"]
			if cl == nil {
				t.Fatal("cluster us/user-svc missing")
			}
			if got := cl.GetUpstreamConnectionOptions().GetTcpKeepalive().GetKeepaliveTime().GetValue(); got != 300 {
				t.Fatalf("tcp_keepalive.keepalive_time = %d, want 300 (merge patch)", got)
			}
			if got := cl.GetDnsLookupFamily(); got != clusterv3.Cluster_V4_ONLY {
				t.Fatalf("dns_lookup_family = %v, want V4_ONLY (jsonPatch)", got)
			}
			if mode == ModeXDS {
				// F5 xDS 形态：STATIC → EDS，CLA 抽入 IR.Endpoints。
				if cl.GetType() != clusterv3.Cluster_EDS {
					t.Fatalf("xds cluster type = %v, want EDS", cl.GetType())
				}
				if out.Endpoints["us/user-svc"] == nil {
					t.Fatal("xds: EDS resource us/user-svc missing")
				}
			} else if cl.GetLoadAssignment() == nil {
				t.Fatal("static: cluster should keep inline load_assignment")
			}
		})
	}
}

const p72Supplement = `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: http}
spec: {port: 80, protocol: HTTP}
---
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: api}
spec:
  listeners: [http]
  rules:
    - match: {path: {prefix: /}}
      backends: [{upstream: app}]
---
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: app}
spec:
  endpoints: [{address: 127.0.0.1, port: 9000}]
`

// TestDocExample72EnvoyResources 协议 §7.2 文档示例逐字（正例）：
// 原生 Cluster 按 @type 分发进 IR.Clusters，SourceMap 归属 EnvoyResources 对象。
func TestDocExample72EnvoyResources(t *testing.T) {
	for _, mode := range []Mode{ModeStatic, ModeXDS} {
		t.Run(string(mode), func(t *testing.T) {
			out, errs := compileYAMLDir(t, mode, "testdata/escape-hatch/envoy-resources.yaml", p72Supplement)
			assertNoErrs(t, errs)
			cl := out.Clusters["legacy-cluster"]
			if cl == nil {
				t.Fatal("cluster legacy-cluster missing")
			}
			if cl.GetType() != clusterv3.Cluster_STRICT_DNS {
				t.Fatalf("type = %v, want STRICT_DNS", cl.GetType())
			}
			if cl.GetConnectTimeout().GetSeconds() != 3 {
				t.Fatalf("connect_timeout = %v, want 3s", cl.GetConnectTimeout())
			}
			src := out.SourceMap[ir.ResourceKey{Kind: ir.ResourceCluster, Name: "legacy-cluster"}]
			if src.Kind != protocol.KindEnvoyResources || src.Name != "custom-lua" || src.Path != "spec.resources[0]" {
				t.Fatalf("SourceMap = %+v, want EnvoyResources/custom-lua spec.resources[0]", src)
			}
		})
	}
}

// envoyResourcesObj 构造一个 EnvoyResources 对象（单资源）。
func envoyResourcesObj(name string, allowOverride bool, resources ...string) *protocol.EnvoyResources {
	er := &protocol.EnvoyResources{
		APIVersion: protocol.APIVersionV1Alpha1,
		Kind:       protocol.KindEnvoyResources,
		Metadata:   protocol.ObjectMeta{Name: name},
		Spec:       protocol.EnvoyResourcesSpec{AllowOverride: allowOverride},
		Origin:     testOrigin(),
	}
	for _, r := range resources {
		er.Spec.Resources = append(er.Spec.Resources, protocol.RawJSON(r))
	}
	return er
}

// baseHTTPConfig 构造最小可编译配置：HTTP Listener + Route → Upstream。
func baseHTTPConfig() *protocol.ConfigSet {
	return &protocol.ConfigSet{
		Listeners: []*protocol.Listener{newListener("http", 80, protocol.ProtocolHTTP)},
		Routes:    []*protocol.Route{newHTTPRoute("api", []string{"http"}, nil, "app")},
		Upstreams: []*protocol.Upstream{newUpstream("app")},
	}
}

// TestEnvoyResourcesConflict 覆盖重名合并规则：默认报错；allowOverride 整体替换
// 并更新 SourceMap 归属（编译层 §3 F4 规则 3）。
func TestEnvoyResourcesConflict(t *testing.T) {
	const clusterJSON = `{
		"@type": "type.googleapis.com/envoy.config.cluster.v3.Cluster",
		"name": "us/app",
		"connect_timeout": "9s",
		"type": "STATIC",
		"load_assignment": {"cluster_name": "us/app", "endpoints": [{"lb_endpoints": [{"endpoint": {"address": {"socket_address": {"address": "9.9.9.9", "port_value": 1}}}}]}]}
	}`

	t.Run("conflict rejected by default", func(t *testing.T) {
		cs := baseHTTPConfig()
		cs.EnvoyResources = []*protocol.EnvoyResources{envoyResourcesObj("er", false, clusterJSON)}
		out, errs := Compile(cs, Options{Mode: ModeStatic})
		if out != nil {
			t.Fatal("IR should be nil on conflict")
		}
		if len(errs) != 1 || errs[0].Stage != StagePatch || errs[0].Source.Kind != protocol.KindEnvoyResources {
			t.Fatalf("want one patch-stage EnvoyResources error, got:\n%s", formatErrs(errs))
		}
	})

	t.Run("allowOverride replaces and re-owns SourceMap", func(t *testing.T) {
		cs := baseHTTPConfig()
		cs.EnvoyResources = []*protocol.EnvoyResources{envoyResourcesObj("er", true, clusterJSON)}
		out, errs := Compile(cs, Options{Mode: ModeStatic})
		assertNoErrs(t, errs)
		cl := out.Clusters["us/app"]
		if cl.GetConnectTimeout().GetSeconds() != 9 {
			t.Fatalf("connect_timeout = %v, want 9s (overridden)", cl.GetConnectTimeout())
		}
		src := out.SourceMap[ir.ResourceKey{Kind: ir.ResourceCluster, Name: "us/app"}]
		if src.Kind != protocol.KindEnvoyResources {
			t.Fatalf("SourceMap = %+v, want EnvoyResources ownership", src)
		}
	})

	t.Run("unsupported @type", func(t *testing.T) {
		cs := baseHTTPConfig()
		cs.EnvoyResources = []*protocol.EnvoyResources{envoyResourcesObj("er", false,
			`{"@type": "type.googleapis.com/envoy.config.bootstrap.v3.Bootstrap", "node": {"id": "x"}}`)}
		_, errs := Compile(cs, Options{Mode: ModeStatic})
		if len(errs) != 1 || errs[0].Stage != StagePatch {
			t.Fatalf("want patch-stage error, got:\n%s", formatErrs(errs))
		}
	})
}
