package compile

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"sort"

	accesslogv3 "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	filelogv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	tlsinspectorv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/listener/tls_inspector/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// serverNameExtractor 从 PEM 证书文件提取 SNI server names（抽象 IO，便于测试注入）。
// 返回排序去重后的域名列表；证书无 SAN 且无 CN 时返回空列表。
type serverNameExtractor func(certFile string) ([]string, error)

// defaultServerNameExtractor 读 PEM 首个证书块，返回 SAN DNSNames；无 SAN 回退 Subject CN
// （通配 SAN 如 *.example.com 原样保留，Envoy filter_chain_match.server_names 原生支持）。
func defaultServerNameExtractor(certFile string) ([]string, error) {
	data, err := os.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("no PEM certificate found in %s", certFile)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate %s: %w", certFile, err)
	}
	names := append([]string(nil), cert.DNSNames...)
	if len(names) == 0 && cert.Subject.CommonName != "" {
		names = []string{cert.Subject.CommonName}
	}
	sort.Strings(names)
	// 去重（同证书内重复 SAN）。
	out := names[:0]
	for i, n := range names {
		if i > 0 && names[i-1] == n {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

// buildHTTPListener 构建一个 HTTP/HTTPS Listener 及其 RouteConfiguration（编译层 §3 F3）。
func (ctx *buildContext) buildHTTPListener(l *protocol.Listener) (*listenerv3.Listener, *routev3.RouteConfiguration, []CompileError) {
	var errs []CompileError
	name := l.Metadata.Name

	// RouteConfiguration：挂到本 Listener 的 HTTP 形态 Route，按 Route name 排序（确定性）。
	rc := &routev3.RouteConfiguration{Name: routeConfigName(name)}
	jwtAsm := newJwtAssembly()
	for _, r := range sortedRoutes(ctx.cs.Routes) {
		if len(r.Spec.Rules) == 0 || !attachesTo(r, name) {
			continue
		}
		vh, verrs := ctx.buildVirtualHost(l, r, jwtAsm)
		errs = append(errs, verrs...)
		if vh != nil {
			rc.VirtualHosts = append(rc.VirtualHosts, vh)
		}
	}

	hcm, herrs := ctx.buildHCM(l, jwtAsm)
	errs = append(errs, herrs...)
	if hcm == nil {
		return nil, nil, errs
	}
	hcmAny, err := marshalAny(hcm)
	if err != nil {
		return nil, nil, append(errs, buildError(l.Origin, protocol.KindListener, name, "",
			"marshal HttpConnectionManager: %v", err))
	}
	hcmFilter := &listenerv3.Filter{
		Name:       hcmFilterName,
		ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: hcmAny},
	}

	// HTTPS：每证书一条 filter chain，按证书 SAN 生成 filter_chain_match（SNI），
	// 全部 chain 共享同一份 HCM 配置（编译层 §3 Builder 表）。
	var chains []*listenerv3.FilterChain
	if l.Spec.Protocol == protocol.ProtocolHTTPS {
		chains, errs = ctx.buildHTTPSFilterChains(l, hcmFilter, errs)
	} else {
		chains = []*listenerv3.FilterChain{{Filters: []*listenerv3.Filter{hcmFilter}}}
	}
	if len(chains) == 0 {
		return nil, nil, errs
	}
	lis := &listenerv3.Listener{
		Name:         listenerResourceName(name),
		Address:      socketAddress(l.Spec.Address, l.Spec.Port),
		FilterChains: chains,
	}
	if l.Spec.Protocol == protocol.ProtocolHTTPS {
		// SNI filter_chain_match 依赖 tls_inspector listener filter 提取
		// ClientHello server_name；缺它所有带 server_names 的 chain 都无法
		// 命中、连接被直接关闭（T7 e2e 实测复现：握手即 EOF）。
		tiAny, err := marshalAny(&tlsinspectorv3.TlsInspector{})
		if err != nil {
			return nil, nil, append(errs, buildError(l.Origin, protocol.KindListener, name, "",
				"marshal TlsInspector: %v", err))
		}
		lis.ListenerFilters = []*listenerv3.ListenerFilter{{
			Name:       tlsInspectorFilterName,
			ConfigType: &listenerv3.ListenerFilter_TypedConfig{TypedConfig: tiAny},
		}}
	}
	return lis, rc, errs
}

// attachesTo 报告 Route 是否挂到指定 Listener。
func attachesTo(r *protocol.Route, listener string) bool {
	for _, n := range r.Spec.Listeners {
		if n == listener {
			return true
		}
	}
	return false
}

// buildHTTPSFilterChains 为 HTTPS Listener 生成每证书一条的 filter chain。
// 证书间 server name 重叠（SNI 二义）= 编译错误（技术设计 §4 风险项）。
func (ctx *buildContext) buildHTTPSFilterChains(l *protocol.Listener, hcmFilter *listenerv3.Filter, errs []CompileError) ([]*listenerv3.FilterChain, []CompileError) {
	var chains []*listenerv3.FilterChain
	claimed := map[string]int{} // server name → 首次声明的证书下标
	for i, c := range l.Spec.TLS.Certificates {
		base := fmt.Sprintf("spec.tls.certificates[%d]", i)
		names, err := ctx.extract(c.CertFile)
		if err != nil {
			errs = append(errs, buildError(l.Origin, protocol.KindListener, l.Metadata.Name,
				base+".certFile", "extract server names from certificate: %v", err))
			continue
		}
		if len(names) == 0 {
			errs = append(errs, buildError(l.Origin, protocol.KindListener, l.Metadata.Name,
				base+".certFile", "certificate %q has neither SAN nor CN; cannot derive SNI server names", c.CertFile))
			continue
		}
		for _, n := range names {
			if prev, ok := claimed[n]; ok {
				errs = append(errs, buildError(l.Origin, protocol.KindListener, l.Metadata.Name,
					base+".certFile",
					"server name %q overlaps with certificates[%d] (SNI ambiguity; explicit sniHosts disambiguation is P1)", n, prev))
				continue
			}
			claimed[n] = i
		}
		ts, terr := downstreamTLSSocket(l, &c)
		if terr != nil {
			errs = append(errs, buildError(l.Origin, protocol.KindListener, l.Metadata.Name,
				base, "%v", terr))
			continue
		}
		chains = append(chains, &listenerv3.FilterChain{
			FilterChainMatch: &listenerv3.FilterChainMatch{ServerNames: names},
			TransportSocket:  ts,
			Filters:          []*listenerv3.Filter{hcmFilter},
		})
	}
	return chains, errs
}

// buildHCM 生成共享的 HttpConnectionManager：
// stat_prefix = lis/<name>、RDS 名 rc/<listener>（编译层 §5 命名规约）；
// Gateway.spec.http 默认值（F2 已填充）与 Listener.spec.http 覆盖在此汇合。
func (ctx *buildContext) buildHCM(l *protocol.Listener, jwtAsm *jwtAssembly) (*hcmv3.HttpConnectionManager, []CompileError) {
	gw := &ctx.cs.Gateway.Spec
	filters, ferrs := httpFilters(jwtAsm)
	if ferrs != nil {
		return nil, ferrs
	}
	hcm := &hcmv3.HttpConnectionManager{
		StatPrefix: listenerResourceName(l.Metadata.Name),
		RouteSpecifier: &hcmv3.HttpConnectionManager_Rds{
			Rds: &hcmv3.Rds{
				RouteConfigName: routeConfigName(l.Metadata.Name),
				ConfigSource: &corev3.ConfigSource{
					// 逻辑资源固定挂 ADS；static 形态由 F5 形态化内联（编译层 §1 要点 2）。
					ConfigSourceSpecifier: &corev3.ConfigSource_Ads{Ads: &corev3.AggregatedConfigSource{}},
				},
			},
		},
		HttpFilters: filters,
		CommonHttpProtocolOptions: &corev3.HttpProtocolOptions{
			IdleTimeout: durationpb.New(gw.HTTP.IdleTimeout.Duration),
		},
		MaxRequestHeadersKb: wrapperspb.UInt32(uint32(*gw.HTTP.MaxRequestHeadersKB)),
		// vhost 域名匹配剥离 Host/:authority 端口后缀（nginx server_name 同语义）：
		// 非标准端口部署时 Host 带 :port，不剥离会导致兜底 404（T7 e2e 实测复现）。
		StripPortMode: &hcmv3.HttpConnectionManager_StripAnyHostPort{
			StripAnyHostPort: true,
		},
	}
	// serverHeader："" 表示透传上游（协议 §3.1）。
	if *gw.HTTP.ServerHeader == "" {
		hcm.ServerHeaderTransformation = hcmv3.HttpConnectionManager_PASS_THROUGH
	} else {
		hcm.ServerName = *gw.HTTP.ServerHeader
	}
	// Listener.spec.http.http2（HTTPS 默认 true、HTTP 默认 false，F2 已填默认值）。
	// http3 为 P2 字段：M0 不生成 QUIC，预留不生效（记录于 T4 进展）。
	if l.Spec.HTTP != nil && l.Spec.HTTP.HTTP2 != nil && *l.Spec.HTTP.HTTP2 {
		hcm.Http2ProtocolOptions = &corev3.Http2ProtocolOptions{}
	}
	// access log（Gateway.spec.accessLog；path 省略 = stdout）。
	if al := gw.AccessLog; al != nil && al.Enabled {
		acc, err := buildAccessLog(al)
		if err != nil {
			return nil, []CompileError{buildError(ctx.cs.Gateway.Origin, protocol.KindGateway,
				ctx.cs.Gateway.Metadata.Name, "spec.accessLog", "%v", err)}
		}
		hcm.AccessLog = []*accesslogv3.AccessLog{acc}
	}
	return hcm, nil
}

// buildAccessLog 生成文件 access log：json → JsonFormat（空字典 = Envoy 默认字段集）；
// text 或未指定 → 省略 LogFormat 用 Envoy 默认文本格式；path 省略 → /dev/stdout。
func buildAccessLog(al *protocol.AccessLog) (*accesslogv3.AccessLog, error) {
	path := al.Path
	if path == "" {
		path = "/dev/stdout"
	}
	fal := &filelogv3.FileAccessLog{Path: path}
	if al.Format == protocol.AccessLogFormatJSON {
		fal.AccessLogFormat = &filelogv3.FileAccessLog_LogFormat{
			LogFormat: &corev3.SubstitutionFormatString{
				Format: &corev3.SubstitutionFormatString_JsonFormat{JsonFormat: &structpb.Struct{}},
			},
		}
	}
	cfg, err := marshalAny(fal)
	if err != nil {
		return nil, fmt.Errorf("marshal FileAccessLog: %w", err)
	}
	return &accesslogv3.AccessLog{
		Name:       fileAccessLogName,
		ConfigType: &accesslogv3.AccessLog_TypedConfig{TypedConfig: cfg},
	}, nil
}

// downstreamTLSSocket 生成一条 filter chain 的 TLS transport socket：
// 证书链/私钥走文件路径（static 形态；xDS 形态由 F5 转 SDS 引用 crt/<listener>/<n>）。
// clientCA（mTLS）为 P1 字段，值透传不设计专项测试。
func downstreamTLSSocket(l *protocol.Listener, c *protocol.Certificate) (*corev3.TransportSocket, error) {
	tlsCfg := l.Spec.TLS
	ctx := &tlsv3.CommonTlsContext{
		TlsParams: &tlsv3.TlsParameters{
			TlsMinimumProtocolVersion: tlsProtocolVersion(tlsCfg.MinVersion),
		},
		AlpnProtocols: tlsCfg.ALPN,
		TlsCertificates: []*tlsv3.TlsCertificate{{
			CertificateChain: &corev3.DataSource{
				Specifier: &corev3.DataSource_Filename{Filename: c.CertFile},
			},
			PrivateKey: &corev3.DataSource{
				Specifier: &corev3.DataSource_Filename{Filename: c.KeyFile},
			},
		}},
	}
	down := &tlsv3.DownstreamTlsContext{CommonTlsContext: ctx}
	if tlsCfg.ClientCA != "" {
		ctx.ValidationContextType = &tlsv3.CommonTlsContext_ValidationContext{
			ValidationContext: &tlsv3.CertificateValidationContext{
				TrustedCa: &corev3.DataSource{
					Specifier: &corev3.DataSource_Filename{Filename: tlsCfg.ClientCA},
				},
			},
		}
		down.RequireClientCertificate = wrapperspb.Bool(true)
	}
	cfg, err := marshalAny(down)
	if err != nil {
		return nil, fmt.Errorf("marshal DownstreamTlsContext: %w", err)
	}
	return &corev3.TransportSocket{
		Name:       tlsTransportSocketName,
		ConfigType: &corev3.TransportSocket_TypedConfig{TypedConfig: cfg},
	}, nil
}

// tlsProtocolVersion 映射协议 tls.minVersion 枚举（"1.2" 默认）。
func tlsProtocolVersion(v protocol.TLSVersion) tlsv3.TlsParameters_TlsProtocol {
	if v == protocol.TLSVersion13 {
		return tlsv3.TlsParameters_TLSv1_3
	}
	return tlsv3.TlsParameters_TLSv1_2
}

// socketAddress 生成监听/端点地址（支持 IPv6 字面量，如 "::"）。
func socketAddress(address string, port int32) *corev3.Address {
	return &corev3.Address{
		Address: &corev3.Address_SocketAddress{
			SocketAddress: &corev3.SocketAddress{
				Address: address,
				PortSpecifier: &corev3.SocketAddress_PortValue{
					PortValue: uint32(port),
				},
			},
		},
	}
}
