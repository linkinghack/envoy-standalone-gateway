package compile

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	filelogv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcpproxyv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	udpproxyv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/udp/udp_proxy/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// TestHTTPListenerBuild 覆盖明文 HTTP Listener：单 filter chain 无 match、
// HCM stat_prefix/RDS 命名、Gateway.http 默认值合并、access log。
func TestHTTPListenerBuild(t *testing.T) {
	cs := &protocol.ConfigSet{
		Listeners: []*protocol.Listener{newListener("web", 80, protocol.ProtocolHTTP)},
		Routes:    []*protocol.Route{newHTTPRoute("r1", []string{"web"}, nil, "app")},
		Upstreams: []*protocol.Upstream{newUpstream("app")},
	}
	res, errs := buildCS(t, cs)
	assertNoErrs(t, errs)

	lis := findListener(t, res, "lis/web")
	if got := lis.GetAddress().GetSocketAddress().GetPortValue(); got != 80 {
		t.Fatalf("port = %d, want 80", got)
	}
	if got := lis.GetAddress().GetSocketAddress().GetAddress(); got != "0.0.0.0" {
		t.Fatalf("address = %q, want 0.0.0.0 (F2 默认值)", got)
	}
	if len(lis.GetFilterChains()) != 1 {
		t.Fatalf("filter chains = %d, want 1", len(lis.GetFilterChains()))
	}
	chain := lis.GetFilterChains()[0]
	if chain.GetFilterChainMatch() != nil {
		t.Fatalf("HTTP listener must not have filter_chain_match, got %v", chain.GetFilterChainMatch())
	}
	if chain.GetTransportSocket() != nil {
		t.Fatal("HTTP listener must not have transport socket")
	}
	hcm := hcmOf(t, chain)
	if hcm.GetStatPrefix() != "lis/web" {
		t.Fatalf("stat_prefix = %q, want lis/web", hcm.GetStatPrefix())
	}
	if hcm.GetRds().GetRouteConfigName() != "rc/web" {
		t.Fatalf("rds name = %q, want rc/web", hcm.GetRds().GetRouteConfigName())
	}
	// Gateway.http 默认值合并（F2 填充）：idleTimeout 60s、maxRequestHeadersKb 60、serverHeader esgw。
	if hcm.GetCommonHttpProtocolOptions().GetIdleTimeout().GetSeconds() != 60 {
		t.Fatalf("idleTimeout = %v, want 60s", hcm.GetCommonHttpProtocolOptions().GetIdleTimeout())
	}
	if hcm.GetMaxRequestHeadersKb().GetValue() != 60 {
		t.Fatalf("maxRequestHeadersKb = %d, want 60", hcm.GetMaxRequestHeadersKb().GetValue())
	}
	if hcm.GetServerName() != "esgw" {
		t.Fatalf("serverName = %q, want esgw", hcm.GetServerName())
	}
	if !hcm.GetUseRemoteAddress().GetValue() || hcm.GetXffNumTrustedHops() != 0 {
		t.Fatalf("source address boundary = use_remote_address:%v trusted_hops:%d, want true/0",
			hcm.GetUseRemoteAddress(), hcm.GetXffNumTrustedHops())
	}
	// HTTP 明文默认 http2=false。
	if hcm.GetHttp2ProtocolOptions() != nil {
		t.Fatal("HTTP listener default must not set http2 options")
	}
	// 未配 accessLog → 无 access log。
	if len(hcm.GetAccessLog()) != 0 {
		t.Fatalf("accessLog = %v, want none", hcm.GetAccessLog())
	}
	// RouteConfiguration 同名 rc/web。
	findRouteConfig(t, res, "rc/web")
}

// TestHTTPSListenerSNI 表驱动覆盖证书 SAN → filter_chain_match（技术设计 §4 风险项）：
// 单证书/多证书/通配 SAN/无 SAN 有 CN/SAN 重叠报错/证书读取失败。
func TestHTTPSListenerSNI(t *testing.T) {
	t.Run("single cert SAN", func(t *testing.T) {
		dir := t.TempDir()
		cert, key := writeSelfSignedCert(t, dir, "api", "api.example.com", "api.internal")
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{
			newTLSListener("https", 443, protocol.ProtocolHTTPS, cert, key),
		}}
		res, errs := buildCS(t, cs)
		assertNoErrs(t, errs)
		lis := findListener(t, res, "lis/https")
		if len(lis.GetFilterChains()) != 1 {
			t.Fatalf("chains = %d, want 1", len(lis.GetFilterChains()))
		}
		got := lis.GetFilterChains()[0].GetFilterChainMatch().GetServerNames()
		want := []string{"api.example.com", "api.internal"} // 排序去重
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("server_names = %v, want %v", got, want)
		}
	})

	t.Run("multi cert one chain each", func(t *testing.T) {
		dir := t.TempDir()
		certA, keyA := writeSelfSignedCert(t, dir, "www", "www.example.com")
		certB, keyB := writeSelfSignedCert(t, dir, "blog", "blog.example.com")
		lis := newTLSListener("https", 443, protocol.ProtocolHTTPS, certA, keyA)
		lis.Spec.TLS.Certificates = append(lis.Spec.TLS.Certificates,
			protocol.Certificate{CertFile: certB, KeyFile: keyB})
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{lis}}
		res, errs := buildCS(t, cs)
		assertNoErrs(t, errs)
		chains := findListener(t, res, "lis/https").GetFilterChains()
		if len(chains) != 2 {
			t.Fatalf("chains = %d, want 2（每证书一条）", len(chains))
		}
		if got := chains[0].GetFilterChainMatch().GetServerNames(); len(got) != 1 || got[0] != "www.example.com" {
			t.Fatalf("chains[0] server_names = %v", got)
		}
		if got := chains[1].GetFilterChainMatch().GetServerNames(); len(got) != 1 || got[0] != "blog.example.com" {
			t.Fatalf("chains[1] server_names = %v", got)
		}
		// 共享 HCM 配置：两条 chain 的 HCM 一致（同一指针）。
		if chains[0].GetFilters()[0] != chains[1].GetFilters()[0] {
			t.Fatal("filter chains must share the same HCM filter")
		}
	})

	t.Run("wildcard SAN", func(t *testing.T) {
		dir := t.TempDir()
		cert, key := writeSelfSignedCert(t, dir, "wild", "*.example.com")
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{
			newTLSListener("https", 443, protocol.ProtocolHTTPS, cert, key),
		}}
		res, errs := buildCS(t, cs)
		assertNoErrs(t, errs)
		got := findListener(t, res, "lis/https").GetFilterChains()[0].GetFilterChainMatch().GetServerNames()
		if len(got) != 1 || got[0] != "*.example.com" {
			t.Fatalf("server_names = %v, want [*.example.com]", got)
		}
	})

	t.Run("no SAN fallback to CN", func(t *testing.T) {
		dir := t.TempDir()
		cert, key := writeSelfSignedCert(t, dir, "cnonly.example.com") // 无 SAN，CN = base
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{
			newTLSListener("https", 443, protocol.ProtocolHTTPS, cert, key),
		}}
		res, errs := buildCS(t, cs)
		assertNoErrs(t, errs)
		got := findListener(t, res, "lis/https").GetFilterChains()[0].GetFilterChainMatch().GetServerNames()
		if len(got) != 1 || got[0] != "cnonly.example.com" {
			t.Fatalf("server_names = %v, want CN fallback [cnonly.example.com]", got)
		}
	})

	t.Run("SAN overlap across certs is error", func(t *testing.T) {
		dir := t.TempDir()
		certA, keyA := writeSelfSignedCert(t, dir, "a", "dup.example.com")
		certB, keyB := writeSelfSignedCert(t, dir, "b", "dup.example.com")
		lis := newTLSListener("https", 443, protocol.ProtocolHTTPS, certA, keyA)
		lis.Spec.TLS.Certificates = append(lis.Spec.TLS.Certificates,
			protocol.Certificate{CertFile: certB, KeyFile: keyB})
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{lis}}
		_, errs := buildCS(t, cs)
		if len(errs) != 1 || errs[0].Stage != StageBuild ||
			!strings.Contains(errs[0].Message, "overlaps") ||
			errs[0].Source.Path != "spec.tls.certificates[1].certFile" {
			t.Fatalf("want one overlap build error at certificates[1], got:\n%s", formatErrs(errs))
		}
	})

	t.Run("unreadable cert is error", func(t *testing.T) {
		dir := t.TempDir()
		cert, key := writeSelfSignedCert(t, dir, "ok", "ok.example.com")
		_ = key
		lis := newTLSListener("https", 443, protocol.ProtocolHTTPS, filepath.Join(dir, "gone.crt"), filepath.Join(dir, "gone.key"))
		_ = cert
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{lis}}
		// 绕过 F2 证书检查（okVerifier），直接对已链接集合跑 build。
		lk, lerrs := link(cs, Options{Mode: ModeStatic}, okVerifier())
		assertNoErrs(t, lerrs)
		_, errs := build(cs, lk, nil)
		if len(errs) != 1 || errs[0].Stage != StageBuild || !strings.Contains(errs[0].Message, "extract server names") {
			t.Fatalf("want one extract build error, got:\n%s", formatErrs(errs))
		}
	})
}

// TestHTTPSListenerTLSContext 覆盖 transport socket：证书路径、minVersion、ALPN、http2。
func TestHTTPSListenerTLSContext(t *testing.T) {
	dir := t.TempDir()
	cert, key := writeSelfSignedCert(t, dir, "api", "api.example.com")
	lis := newTLSListener("https", 443, protocol.ProtocolHTTPS, cert, key)
	lis.Spec.TLS.MinVersion = protocol.TLSVersion13
	cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{lis}}
	res, errs := buildCS(t, cs)
	assertNoErrs(t, errs)

	ts := findListener(t, res, "lis/https").GetFilterChains()[0].GetTransportSocket()
	if ts.GetName() != tlsTransportSocketName {
		t.Fatalf("transport socket = %q", ts.GetName())
	}
	dtc := &tlsv3.DownstreamTlsContext{}
	mustUnmarshal(t, ts.GetTypedConfig(), dtc)
	certs := dtc.GetCommonTlsContext().GetTlsCertificates()
	if len(certs) != 1 || certs[0].GetCertificateChain().GetFilename() != cert || certs[0].GetPrivateKey().GetFilename() != key {
		t.Fatalf("tls certificates = %v", certs)
	}
	if got := dtc.GetCommonTlsContext().GetTlsParams().GetTlsMinimumProtocolVersion(); got != tlsv3.TlsParameters_TLSv1_3 {
		t.Fatalf("min version = %v, want TLSv1_3", got)
	}
	if got := dtc.GetCommonTlsContext().GetAlpnProtocols(); strings.Join(got, ",") != "h2,http/1.1" {
		t.Fatalf("alpn = %v, want [h2 http/1.1]（F2 默认值）", got)
	}
	// HTTPS 默认 http2=true。
	hcm := hcmOf(t, findListener(t, res, "lis/https").GetFilterChains()[0])
	if hcm.GetHttp2ProtocolOptions() == nil {
		t.Fatal("HTTPS listener default must set http2 options")
	}
}

// TestGatewayHTTPMerge 覆盖 Gateway.http 默认值与 Listener 覆盖合并、serverHeader 透传、access log。
func TestGatewayHTTPMerge(t *testing.T) {
	t.Run("gateway http overrides", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Gateway: &protocol.Gateway{
				APIVersion: protocol.APIVersionV1Alpha1, Kind: protocol.KindGateway,
				Metadata: protocol.ObjectMeta{Name: "default"},
				Spec: protocol.GatewaySpec{HTTP: &protocol.HTTPDefaults{
					IdleTimeout:         &protocol.Duration{Duration: 5 * time.Second},
					MaxRequestHeadersKB: ptr(int32(120)),
					ServerHeader:        ptr(""),
				}},
			},
			Listeners: []*protocol.Listener{newListener("web", 80, protocol.ProtocolHTTP)},
		}
		res, errs := buildCS(t, cs)
		assertNoErrs(t, errs)
		hcm := hcmOf(t, findListener(t, res, "lis/web").GetFilterChains()[0])
		if hcm.GetCommonHttpProtocolOptions().GetIdleTimeout().GetSeconds() != 5 {
			t.Fatalf("idleTimeout = %v, want 5s", hcm.GetCommonHttpProtocolOptions().GetIdleTimeout())
		}
		if hcm.GetMaxRequestHeadersKb().GetValue() != 120 {
			t.Fatalf("maxRequestHeadersKb = %d, want 120", hcm.GetMaxRequestHeadersKb().GetValue())
		}
		// serverHeader "" = 透传上游。
		if hcm.GetServerHeaderTransformation() != hcmv3.HttpConnectionManager_PASS_THROUGH {
			t.Fatalf("serverHeaderTransformation = %v, want PASS_THROUGH", hcm.GetServerHeaderTransformation())
		}
	})

	t.Run("listener http2 override", func(t *testing.T) {
		lis := newListener("web", 80, protocol.ProtocolHTTP)
		lis.Spec.HTTP = &protocol.ListenerHTTP{HTTP2: ptr(true)} // h2c
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{lis}}
		res, errs := buildCS(t, cs)
		assertNoErrs(t, errs)
		hcm := hcmOf(t, findListener(t, res, "lis/web").GetFilterChains()[0])
		if hcm.GetHttp2ProtocolOptions() == nil {
			t.Fatal("listener http2=true override must set http2 options")
		}
	})
}

// TestAccessLog 覆盖 access log 映射：json/text 格式、path 省略 = stdout。
func TestAccessLog(t *testing.T) {
	gatewayWithLog := func(format protocol.AccessLogFormat, path string) *protocol.Gateway {
		return &protocol.Gateway{
			APIVersion: protocol.APIVersionV1Alpha1, Kind: protocol.KindGateway,
			Metadata: protocol.ObjectMeta{Name: "default"},
			Spec: protocol.GatewaySpec{AccessLog: &protocol.AccessLog{
				Enabled: true, Format: format, Path: path,
			}},
		}
	}
	buildHCM := func(t *testing.T, gw *protocol.Gateway) *hcmv3.HttpConnectionManager {
		t.Helper()
		cs := &protocol.ConfigSet{
			Gateway:   gw,
			Listeners: []*protocol.Listener{newListener("web", 80, protocol.ProtocolHTTP)},
		}
		res, errs := buildCS(t, cs)
		assertNoErrs(t, errs)
		return hcmOf(t, findListener(t, res, "lis/web").GetFilterChains()[0])
	}
	fileLog := func(t *testing.T, hcm *hcmv3.HttpConnectionManager) *filelogv3.FileAccessLog {
		t.Helper()
		if len(hcm.GetAccessLog()) != 1 {
			t.Fatalf("accessLog count = %d, want 1", len(hcm.GetAccessLog()))
		}
		fal := &filelogv3.FileAccessLog{}
		mustUnmarshal(t, hcm.GetAccessLog()[0].GetTypedConfig(), fal)
		return fal
	}

	t.Run("json format default stdout", func(t *testing.T) {
		fal := fileLog(t, buildHCM(t, gatewayWithLog(protocol.AccessLogFormatJSON, "")))
		if fal.GetPath() != "/dev/stdout" {
			t.Fatalf("path = %q, want /dev/stdout", fal.GetPath())
		}
		if fal.GetLogFormat().GetJsonFormat() == nil {
			t.Fatal("json format must set JsonFormat")
		}
	})

	t.Run("text format with path", func(t *testing.T) {
		fal := fileLog(t, buildHCM(t, gatewayWithLog(protocol.AccessLogFormatText, "/var/log/esgw/access.log")))
		if fal.GetPath() != "/var/log/esgw/access.log" {
			t.Fatalf("path = %q", fal.GetPath())
		}
		if fal.GetLogFormat() != nil && fal.GetLogFormat().GetJsonFormat() != nil {
			t.Fatal("text format must not set JsonFormat（Envoy 默认文本格式）")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		gw := gatewayWithLog(protocol.AccessLogFormatJSON, "")
		gw.Spec.AccessLog.Enabled = false
		hcm := buildHCM(t, gw)
		if len(hcm.GetAccessLog()) != 0 {
			t.Fatalf("disabled accessLog must emit none, got %v", hcm.GetAccessLog())
		}
	})
}

// TestL4Listeners 覆盖 TCP、TLS passthrough 与 UDP 的 Envoy typed filter 映射。
func TestL4Listeners(t *testing.T) {
	t.Run("TCP proxy", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newListener("mysql", 3306, protocol.ProtocolTCP)},
			Routes:    []*protocol.Route{newForwardRoute("mysql", []string{"mysql"}, "db")},
			Upstreams: []*protocol.Upstream{newUpstream("db")},
		}
		res, errs := buildCS(t, cs)
		assertNoErrs(t, errs)
		lis := findListener(t, res, "lis/mysql")
		if len(lis.GetFilterChains()) != 1 || len(lis.GetListenerFilters()) != 0 {
			t.Fatalf("TCP listener chains=%d listener_filters=%d, want 1/0", len(lis.GetFilterChains()), len(lis.GetListenerFilters()))
		}
		filter := lis.GetFilterChains()[0].GetFilters()[0]
		if filter.GetName() != tcpProxyFilterName {
			t.Fatalf("filter = %q, want %q", filter.GetName(), tcpProxyFilterName)
		}
		proxy := &tcpproxyv3.TcpProxy{}
		mustUnmarshal(t, filter.GetTypedConfig(), proxy)
		if proxy.GetStatPrefix() != "lis/mysql" || proxy.GetCluster() != "us/db" {
			t.Fatalf("tcp proxy = stat_prefix %q cluster %q", proxy.GetStatPrefix(), proxy.GetCluster())
		}
		if len(res.routes) != 0 {
			t.Fatalf("L4 must not generate RouteConfiguration, got %d", len(res.routes))
		}
	})

	t.Run("TLS passthrough SNI chains", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newListener("tls", 8443, protocol.ProtocolTLS)},
			Routes: []*protocol.Route{
				newForwardRoute("z-route", []string{"tls"}, "z", "Z.EXAMPLE.com", "a.example.com"),
				newForwardRoute("a-route", []string{"tls"}, "a", "b.example.com"),
			},
			Upstreams: []*protocol.Upstream{newUpstream("a"), newUpstream("z")},
		}
		res, errs := buildCS(t, cs)
		assertNoErrs(t, errs)
		lis := findListener(t, res, "lis/tls")
		if len(lis.GetListenerFilters()) != 1 || lis.GetListenerFilters()[0].GetName() != tlsInspectorFilterName {
			t.Fatalf("TLS passthrough listener filters = %v", lis.GetListenerFilters())
		}
		chains := lis.GetFilterChains()
		if len(chains) != 2 {
			t.Fatalf("chains = %d, want 2", len(chains))
		}
		if got := strings.Join(chains[0].GetFilterChainMatch().GetServerNames(), ","); got != "a.example.com,z.example.com" {
			t.Fatalf("chains[0] server_names = %q", got)
		}
		if chains[0].GetFilterChainMatch().GetTransportProtocol() != "tls" || chains[0].GetTransportSocket() != nil {
			t.Fatal("TLS passthrough must match inspected TLS without terminating it")
		}
		proxy := &tcpproxyv3.TcpProxy{}
		mustUnmarshal(t, chains[0].GetFilters()[0].GetTypedConfig(), proxy)
		if proxy.GetCluster() != "us/z" {
			t.Fatalf("chains[0] cluster = %q, want us/z", proxy.GetCluster())
		}
	})

	t.Run("UDP proxy", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{newListener("dns", 5353, protocol.ProtocolUDP)},
			Routes:    []*protocol.Route{newForwardRoute("dns", []string{"dns"}, "resolver")},
			Upstreams: []*protocol.Upstream{newUpstream("resolver")},
		}
		res, errs := buildCS(t, cs)
		assertNoErrs(t, errs)
		lis := findListener(t, res, "lis/dns")
		if lis.GetAddress().GetSocketAddress().GetProtocol() != corev3.SocketAddress_UDP {
			t.Fatalf("socket protocol = %v, want UDP", lis.GetAddress().GetSocketAddress().GetProtocol())
		}
		if lis.GetUdpListenerConfig() == nil || len(lis.GetFilterChains()) != 0 || len(lis.GetListenerFilters()) != 1 {
			t.Fatalf("UDP listener config/chains/filters = %v/%d/%d", lis.GetUdpListenerConfig(), len(lis.GetFilterChains()), len(lis.GetListenerFilters()))
		}
		filter := lis.GetListenerFilters()[0]
		if filter.GetName() != udpProxyFilterName {
			t.Fatalf("filter = %q, want %q", filter.GetName(), udpProxyFilterName)
		}
		proxy := &udpproxyv3.UdpProxyConfig{}
		mustUnmarshal(t, filter.GetTypedConfig(), proxy)
		cluster := udpProxyCluster(filter)
		if proxy.GetStatPrefix() != "lis/dns" || cluster != "us/resolver" {
			t.Fatalf("udp proxy = stat_prefix %q cluster %q", proxy.GetStatPrefix(), cluster)
		}
	})
}

// ptr 返回 T 的指针（测试辅助）。
func ptr[T any](v T) *T { return &v }
