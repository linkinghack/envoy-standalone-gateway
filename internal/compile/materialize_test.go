package compile

import (
	"testing"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	"google.golang.org/protobuf/proto"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// buildTLSConfigSet 构造含 HTTPS Listener（证书）+ 静态/DNS 两个 Upstream 的配置集，
// 用于两模式形态对拍。
func buildTLSConfigSet(t *testing.T) *protocol.ConfigSet {
	t.Helper()
	dir := t.TempDir()
	cert, key := writeSelfSignedCert(t, dir, "www", "www.example.com")
	cs := &protocol.ConfigSet{
		Listeners: []*protocol.Listener{newTLSListener("https", 443, protocol.ProtocolHTTPS, cert, key)},
		Routes:    []*protocol.Route{newHTTPRoute("www", []string{"https"}, []string{"www.example.com"}, "app")},
		Upstreams: []*protocol.Upstream{newUpstream("app")},
	}
	dns := newUpstream("dns-svc")
	dns.Spec.Endpoints = nil
	dns.Spec.DNS = &protocol.DNSEndpointSource{Hostname: "order.internal", Port: 9090}
	cs.Upstreams = append(cs.Upstreams, dns)
	return cs
}

// TestTwoModesAgree 两模式对拍（编译层 §3 F5：同一批逻辑资源，形态化决定引用还是
// 内联）：RouteConfiguration 内容两模式一致；xds 引 SDS/EDS，static 全内联。
func TestTwoModesAgree(t *testing.T) {
	cs := buildTLSConfigSet(t)
	xds, errs := Compile(cs, Options{Mode: ModeXDS})
	assertNoErrs(t, errs)
	st, errs := Compile(cs, Options{Mode: ModeStatic})
	assertNoErrs(t, errs)

	// 路由内容一致：xds 的 RDS 资源 vs static 内联进 HCM 的 route_config。
	rcXDS := xds.Routes["rc/https"]
	if rcXDS == nil {
		t.Fatal("xds: rc/https missing")
	}
	rcStatic := inlinedRouteConfig(t, st, "lis/https")
	if !proto.Equal(rcXDS, rcStatic) {
		t.Fatal("route configs differ between modes")
	}

	// Listener 数量与地址一致。
	if len(xds.Listeners) != len(st.Listeners) {
		t.Fatal("listener count differs between modes")
	}

	// 证书：xds 引 SDS（Secret 在 IR.Secrets）；static 文件路径内联。
	if xds.Secrets["crt/https/0"] == nil {
		t.Fatal("xds: crt/https/0 missing")
	}
	if len(st.Secrets) != 0 {
		t.Fatal("static: secrets should be inlined")
	}
	downX, _ := decodeDownstreamTLS(xds.Listeners["lis/https"].GetFilterChains()[0])
	if len(downX.GetCommonTlsContext().GetTlsCertificateSdsSecretConfigs()) != 1 {
		t.Fatal("xds: want exactly one SDS certificate ref")
	}
	downS, _ := decodeDownstreamTLS(st.Listeners["lis/https"].GetFilterChains()[0])
	if got := downS.GetCommonTlsContext().GetTlsCertificates(); len(got) != 1 ||
		got[0].GetCertificateChain().GetFilename() == "" {
		t.Fatal("static: cert file path should be inlined in transport socket")
	}

	// 端点：xds STATIC→EDS（CLA 抽入 IR.Endpoints）；static 内联。
	clX := xds.Clusters["us/app"]
	if clX.GetType() != clusterv3.Cluster_EDS || clX.GetLoadAssignment() != nil {
		t.Fatal("xds: static-endpoints cluster should become EDS without inline CLA")
	}
	if xds.Endpoints["us/app"] == nil {
		t.Fatal("xds: EDS resource us/app missing")
	}
	clS := st.Clusters["us/app"]
	if clS.GetType() != clusterv3.Cluster_STATIC || clS.GetLoadAssignment() == nil {
		t.Fatal("static: cluster should keep STATIC + inline CLA")
	}

	// DNS 类 cluster 两模式都保持 LOGICAL/STRICT_DNS 内联（解析机制本身）。
	if got := xds.Clusters["us/dns-svc"].GetType(); got != clusterv3.Cluster_LOGICAL_DNS {
		t.Fatalf("xds dns cluster type = %v, want LOGICAL_DNS (unchanged)", got)
	}
}
