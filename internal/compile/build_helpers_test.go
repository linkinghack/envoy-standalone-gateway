package compile

import (
	"testing"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// buildCS 跑 F2（真证书检查器）+ F3，返回产物与 build 阶段错误。
// F2 出错直接失败——各 Builder 测试假设输入已过链接。
func buildCS(t *testing.T, cs *protocol.ConfigSet) (*buildResult, []CompileError) {
	t.Helper()
	lk, errs := link(cs, Options{Mode: ModeStatic}, defaultCertVerifier())
	if hasErrors(errs) {
		t.Fatalf("unexpected link errors:\n%s", formatErrs(errs))
	}
	return build(cs, lk, nil)
}

// mustUnmarshal 解包 typed_config Any。
func mustUnmarshal(t *testing.T, a *anypb.Any, m proto.Message) {
	t.Helper()
	if err := a.UnmarshalTo(m); err != nil {
		t.Fatalf("unmarshal %s: %v", a.GetTypeUrl(), err)
	}
}

// findListener 按资源名查找 Listener。
func findListener(t *testing.T, res *buildResult, name string) *listenerv3.Listener {
	t.Helper()
	for _, l := range res.listeners {
		if l.GetName() == name {
			return l
		}
	}
	t.Fatalf("listener %q not found in build result", name)
	return nil
}

// findRouteConfig 按资源名查找 RouteConfiguration。
func findRouteConfig(t *testing.T, res *buildResult, name string) *routev3.RouteConfiguration {
	t.Helper()
	for _, rc := range res.routes {
		if rc.GetName() == name {
			return rc
		}
	}
	t.Fatalf("route config %q not found in build result", name)
	return nil
}

// findCluster 按资源名查找 Cluster。
func findCluster(t *testing.T, res *buildResult, name string) *clusterv3.Cluster {
	t.Helper()
	for _, c := range res.clusters {
		if c.GetName() == name {
			return c
		}
	}
	t.Fatalf("cluster %q not found in build result", name)
	return nil
}

// hcmOf 解出 filter chain 的 HCM 配置。
func hcmOf(t *testing.T, chain *listenerv3.FilterChain) *hcmv3.HttpConnectionManager {
	t.Helper()
	if len(chain.GetFilters()) != 1 {
		t.Fatalf("want exactly 1 network filter, got %d", len(chain.GetFilters()))
	}
	f := chain.GetFilters()[0]
	if f.GetName() != hcmFilterName {
		t.Fatalf("filter name = %q, want %q", f.GetName(), hcmFilterName)
	}
	hcm := &hcmv3.HttpConnectionManager{}
	mustUnmarshal(t, f.GetTypedConfig(), hcm)
	return hcm
}

// httpFilterNames 返回 HCM 的 HTTP filter 名序列。
func httpFilterNames(t *testing.T, hcm *hcmv3.HttpConnectionManager) []string {
	t.Helper()
	var names []string
	for _, f := range hcm.GetHttpFilters() {
		names = append(names, f.GetName())
	}
	return names
}
