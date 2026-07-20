package compile

import (
	"fmt"
	"sort"
	"strings"

	bootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
)

// 本文件实现编译流水线 F5 形态化（编译层 §3 F5）：按下发模式决定引用还是内联。
// 这是编译层唯一感知模式的阶段，实现为独立纯函数，两模式可对拍：
//   - ModeXDS：Listener 引 RDS（F3 已挂）/SDS（证书进 IR.Secrets，命名
//     crt/<listener>/<n>）；STATIC Cluster 的内联 CLA 抽到 IR.Endpoints 并改引 EDS；
//   - ModeStatic：route_config 内联进 HCM（IR.Routes 不再携带）、证书文件路径
//     直落 transport socket（含 secret patch 回写）、端点内联（F3 形态即终态）。
//
// EnvoyResources 引入的资源保持用户书写的形态，不做内联/引用转换；
// 编译产物的证书/路由绑定全部按资源名重新定位（listener 级 patch 可能整体
// 替换过资源，见 patch.go synthesis 注释）。

// materialize 执行 F5：F4 工作集 → IR。纯函数：不读文件系统（SD5）。
// patch 造成的结构破坏（如删掉证书/路由）在此暴露并报 patch 阶段错误。
func materialize(syn *synthesis, mode Mode) (*ir.IR, []CompileError) {
	if mode != ModeXDS && mode != ModeStatic {
		return nil, []CompileError{{
			Stage:    StageBuild,
			Severity: SeverityError,
			Message:  fmt.Sprintf("invalid mode %q (want %s | %s)", mode, ModeXDS, ModeStatic),
		}}
	}
	out := &ir.IR{
		Listeners: map[string]*listenerv3.Listener{},
		Clusters:  map[string]*clusterv3.Cluster{},
		Routes:    map[string]*routev3.RouteConfiguration{},
		Endpoints: map[string]*endpointv3.ClusterLoadAssignment{},
		Secrets:   map[string]*tlsv3.Secret{},
		SourceMap: syn.sourceMap,
	}
	var errs []CompileError

	// Listener 形态化（证书 SDS 引用 / 回写 + route_config 内联）。
	inlined := map[string]bool{} // static 模式被内联的 RouteConfiguration 名
	for _, lis := range syn.listeners {
		if !syn.compiledListeners[lis.GetName()] {
			continue // EnvoyResources 引入的 Listener 保持用户形态
		}
		if mode == ModeXDS {
			errs = append(errs, syn.referenceSDS(lis)...)
		} else {
			errs = append(errs, syn.inlineListener(lis, inlined)...)
		}
	}

	// Cluster/Endpoints 形态化：xDS 抽 CLA 引 EDS；static 保持内联。
	endpoints := syn.endpoints
	if mode == ModeXDS {
		var eerrs []CompileError
		endpoints, eerrs = syn.extractEDS()
		errs = append(errs, eerrs...)
	}
	if hasErrors(errs) {
		return nil, errs
	}

	// 装配 IR 集合（map 键 = 资源名；哈希阶段按名排序，编译层 §5）。
	for _, lis := range syn.listeners {
		out.Listeners[lis.GetName()] = lis
	}
	for _, cl := range syn.clusters {
		out.Clusters[cl.GetName()] = cl
	}
	for _, rc := range syn.routes {
		if inlined[rc.GetName()] {
			continue // static：已内联进 HCM
		}
		out.Routes[rc.GetName()] = rc
	}
	for _, cla := range endpoints {
		out.Endpoints[cla.GetClusterName()] = cla
	}
	for _, sec := range syn.secrets {
		if mode == ModeStatic && syn.compiledSecrets[sec.GetName()] {
			continue // static：已回写进 transport socket
		}
		out.Secrets[sec.GetName()] = sec
	}
	out.Bootstrap = buildBootstrap()
	return out, errs
}

// referenceSDS（ModeXDS）：编译产物 Listener 的证书改引 SDS crt/<listener>/<n>，
// 证书本体由调用方装配进 IR.Secrets。
func (s *synthesis) referenceSDS(lis *listenerv3.Listener) []CompileError {
	var errs []CompileError
	name := strings.TrimPrefix(lis.GetName(), "lis/")
	used := map[string]bool{}
	certIdx := 0
	for _, chain := range lis.GetFilterChains() {
		down, ts := decodeDownstreamTLS(chain)
		if down == nil {
			continue
		}
		secretName := secretResourceName(name, certIdx)
		certIdx++
		if s.secretByName(secretName) == nil {
			errs = append(errs, s.materializeErr(secretName,
				"TLS filter chain %d has no certificate Secret %q (was it removed by a patch?)", certIdx-1, secretName))
			continue
		}
		used[secretName] = true
		ctx := down.GetCommonTlsContext()
		ctx.TlsCertificates = nil
		ctx.TlsCertificateSdsSecretConfigs = []*tlsv3.SdsSecretConfig{{
			Name:      secretName,
			SdsConfig: adsConfigSource(),
		}}
		if err := remarshalTransportSocket(ts, down); err != nil {
			errs = append(errs, s.materializeErr(secretName, "remarshal DownstreamTlsContext: %v", err))
		}
	}
	errs = append(errs, s.checkOrphanSecrets(name, used)...)
	return errs
}

// inlineListener（ModeStatic）：route_config 内联进 HCM；证书（含 secret patch
// 结果）回写 transport socket 的文件路径形态。
func (s *synthesis) inlineListener(lis *listenerv3.Listener, inlined map[string]bool) []CompileError {
	var errs []CompileError
	name := strings.TrimPrefix(lis.GetName(), "lis/")

	// route_config 内联（F3 挂的是 ADS 上的 RDS）。
	if f := findHCMFilter(lis); f != nil {
		if hcm := decodeHCM(f); hcm != nil {
			if rds := hcm.GetRds(); rds != nil {
				rcName := rds.GetRouteConfigName()
				rc := s.routeConfig(rcName)
				if rc == nil {
					errs = append(errs, s.materializeErr(listenerResourceName(name),
						"RouteConfiguration %q referenced by HCM RDS not found (was it removed by a patch?)", rcName))
				} else {
					hcm.RouteSpecifier = &hcmv3.HttpConnectionManager_RouteConfig{RouteConfig: rc}
					if err := remarshalFilter(f, hcm); err != nil {
						errs = append(errs, s.materializeErr(listenerResourceName(name), "remarshal HttpConnectionManager: %v", err))
					}
					inlined[rcName] = true
				}
			}
		}
	}

	// 证书回写：Secret（可能已被 patch）→ transport socket 内联证书。
	used := map[string]bool{}
	certIdx := 0
	for _, chain := range lis.GetFilterChains() {
		down, ts := decodeDownstreamTLS(chain)
		if down == nil {
			continue
		}
		secretName := secretResourceName(name, certIdx)
		certIdx++
		secret := s.secretByName(secretName)
		if secret == nil {
			errs = append(errs, s.materializeErr(secretName,
				"TLS filter chain %d has no certificate Secret %q (was it removed by a patch?)", certIdx-1, secretName))
			continue
		}
		used[secretName] = true
		tc := secret.GetTlsCertificate()
		if tc == nil {
			errs = append(errs, s.materializeErr(secretName,
				"patched Secret %q is no longer a tls_certificate; static mode cannot inline other secret types", secretName))
			continue
		}
		down.GetCommonTlsContext().TlsCertificates = []*tlsv3.TlsCertificate{tc}
		if err := remarshalTransportSocket(ts, down); err != nil {
			errs = append(errs, s.materializeErr(secretName, "remarshal DownstreamTlsContext: %v", err))
		}
	}
	errs = append(errs, s.checkOrphanSecrets(name, used)...)
	return errs
}

// checkOrphanSecrets 报告未被任何 filter chain 引用的编译产物证书
// （如 listener 级 patch 删掉了对应 chain）。
func (s *synthesis) checkOrphanSecrets(listener string, used map[string]bool) []CompileError {
	var errs []CompileError
	for name := range s.compiledSecrets {
		if strings.HasPrefix(name, "crt/"+listener+"/") && !used[name] {
			errs = append(errs, s.materializeErr(name,
				"certificate Secret %q is not referenced by any TLS filter chain (was the chain removed by a patch?)", name))
		}
	}
	return errs
}

// extractEDS（ModeXDS）：STATIC Cluster 的内联 CLA 抽到 IR.Endpoints 并改引 EDS
// （ADS）。DNS 类 Cluster（LOGICAL_DNS/STRICT_DNS）的 CLA 是解析机制本身，保持内联。
func (s *synthesis) extractEDS() ([]*endpointv3.ClusterLoadAssignment, []CompileError) {
	endpoints := append([]*endpointv3.ClusterLoadAssignment(nil), s.endpoints...)
	var errs []CompileError
	cls := append([]*clusterv3.Cluster(nil), s.clusters...)
	sort.Slice(cls, func(i, j int) bool { return cls[i].GetName() < cls[j].GetName() })
	for _, cl := range cls {
		if cl.GetType() != clusterv3.Cluster_STATIC || cl.GetLoadAssignment() == nil {
			continue
		}
		cla := cl.GetLoadAssignment()
		if s.hasResource(ir.ResourceKey{Kind: ir.ResourceEndpoints, Name: cla.GetClusterName()}) {
			errs = append(errs, s.materializeErr(cla.GetClusterName(),
				"EDS resource %q conflicts with an existing ClusterLoadAssignment", cla.GetClusterName()))
			continue
		}
		cl.LoadAssignment = nil
		cl.ClusterDiscoveryType = &clusterv3.Cluster_Type{Type: clusterv3.Cluster_EDS}
		cl.EdsClusterConfig = &clusterv3.Cluster_EdsClusterConfig{EdsConfig: adsConfigSource()}
		endpoints = append(endpoints, cla)
		// SourceMap：EDS 资源归属同一 Upstream。
		s.sourceMap[ir.ResourceKey{Kind: ir.ResourceEndpoints, Name: cla.GetClusterName()}] = s.sourceMap[ir.ResourceKey{Kind: ir.ResourceCluster, Name: cl.GetName()}]
	}
	return endpoints, errs
}

// secretByName 按名查找工作集中的 Secret。
func (s *synthesis) secretByName(name string) *tlsv3.Secret {
	for _, sec := range s.secrets {
		if sec.GetName() == name {
			return sec
		}
	}
	return nil
}

// materializeErr 构造 F5 暴露的错误（一律归为 patch 阶段后果，经 SourceMap 回指）。
func (s *synthesis) materializeErr(resourceName, format string, args ...any) CompileError {
	src := ir.SourceRef{Name: resourceName}
	for k, v := range s.sourceMap {
		if k.Name == resourceName {
			src = v
			break
		}
	}
	return CompileError{
		Stage:    StagePatch,
		Source:   src,
		Message:  fmt.Sprintf(format, args...),
		Severity: SeverityError,
	}
}

// decodeDownstreamTLS 解出 filter chain 的 DownstreamTlsContext（非 TLS 返回 nil）。
func decodeDownstreamTLS(chain *listenerv3.FilterChain) (*tlsv3.DownstreamTlsContext, *corev3.TransportSocket) {
	ts := chain.GetTransportSocket()
	if ts == nil || ts.GetTypedConfig() == nil {
		return nil, nil
	}
	if !strings.HasSuffix(ts.GetTypedConfig().GetTypeUrl(), "DownstreamTlsContext") {
		return nil, nil
	}
	down := &tlsv3.DownstreamTlsContext{}
	if err := ts.GetTypedConfig().UnmarshalTo(down); err != nil {
		return nil, nil
	}
	return down, ts
}

// remarshalFilter 把修改后的消息重新封回 Listener filter 的 Any 容器。
func remarshalFilter(f *listenerv3.Filter, m proto.Message) error {
	cfg, err := anypb.New(m)
	if err != nil {
		return err
	}
	f.ConfigType = &listenerv3.Filter_TypedConfig{TypedConfig: cfg}
	return nil
}

// remarshalTransportSocket 把修改后的消息重新封回 transport socket 的 Any 容器。
func remarshalTransportSocket(ts *corev3.TransportSocket, m proto.Message) error {
	cfg, err := anypb.New(m)
	if err != nil {
		return err
	}
	ts.ConfigType = &corev3.TransportSocket_TypedConfig{TypedConfig: cfg}
	return nil
}

// adsConfigSource 返回逻辑资源统一的 ADS 配置源（编译层 §1 要点 2）。
func adsConfigSource() *corev3.ConfigSource {
	return &corev3.ConfigSource{
		ConfigSourceSpecifier: &corev3.ConfigSource_Ads{Ads: &corev3.AggregatedConfigSource{}},
	}
}

// buildBootstrap 生成 Bootstrap 骨架（编译层 §1：admin、node 默认）。
// xDS 的 dynamic_resources（ADS 端点）属下发层配置，T6 渲染/装配时接入（见 T5 进展记录）。
func buildBootstrap() *bootstrapv3.Bootstrap {
	return &bootstrapv3.Bootstrap{
		Node:  &corev3.Node{Id: "esgw", Cluster: "esgw"},
		Admin: &bootstrapv3.Admin{Address: socketAddress("127.0.0.1", 9901)},
	}
}
