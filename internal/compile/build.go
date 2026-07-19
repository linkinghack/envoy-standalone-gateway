package compile

import (
	"sort"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// buildResult 是 F3 的产物：逻辑 Envoy v3 资源集合（编译层 §3 F3）。
// 全部切片按资源 name 排序（编译层 §5 确定性）；T5 将其装入 IR 并做形态化。
type buildResult struct {
	listeners []*listenerv3.Listener
	routes    []*routev3.RouteConfiguration
	clusters  []*clusterv3.Cluster
}

// buildContext 是一次 F3 构建的共享上下文。
// extract 抽象证书 SAN 提取（IO），默认实现读 PEM 文件，测试可注入假实现。
type buildContext struct {
	cs      *protocol.ConfigSet
	lk      *linked
	extract serverNameExtractor
	jwks    map[string]*clusterv3.Cluster // 远程 JWKS 自动生成的抓取集群（按 name 去重）
}

// build 执行 F3 构建：已链接 ConfigSet → 逻辑 Envoy v3 资源。
// 纯函数：除注入的证书 SAN 提取外不读时间/随机/环境（编译层 §5 禁止项）。
// 调用方须先跑 F2（link 填充默认值并校验）；同阶段收集不中断。
func build(cs *protocol.ConfigSet, lk *linked, extract serverNameExtractor) (*buildResult, []CompileError) {
	if extract == nil {
		extract = defaultServerNameExtractor
	}
	ctx := &buildContext{cs: cs, lk: lk, extract: extract, jwks: map[string]*clusterv3.Cluster{}}
	res := &buildResult{}
	var errs []CompileError

	for _, l := range sortedListeners(cs.Listeners) {
		if !isHTTPProtocol(l.Spec.Protocol) {
			errs = append(errs, buildError(l.Origin, protocol.KindListener, l.Metadata.Name, "spec.protocol",
				"protocol %s is not implemented in M0 (L4/UDP listeners are P1)", l.Spec.Protocol))
			continue
		}
		lis, rc, lerrs := ctx.buildHTTPListener(l)
		errs = append(errs, lerrs...)
		if lis != nil {
			res.listeners = append(res.listeners, lis)
			res.routes = append(res.routes, rc)
		}
	}
	for _, u := range sortedUpstreams(cs.Upstreams) {
		cl, uerrs := buildUpstream(u)
		errs = append(errs, uerrs...)
		if cl != nil {
			res.clusters = append(res.clusters, cl)
		}
	}
	// 远程 JWKS 抓取集群（PolicyBuilder 装配时收集），并入集群集合。
	for _, cl := range ctx.jwks {
		res.clusters = append(res.clusters, cl)
	}
	// map 输出按 name 排序（编译层 §5 确定性）。
	sort.Slice(res.clusters, func(i, j int) bool { return res.clusters[i].GetName() < res.clusters[j].GetName() })
	return res, errs
}

// sortedListeners 返回按 name 排序的 Listener 副本（map 输出确定性）。
func sortedListeners(in []*protocol.Listener) []*protocol.Listener {
	out := append([]*protocol.Listener(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Metadata.Name < out[j].Metadata.Name })
	return out
}

// sortedUpstreams 返回按 name 排序的 Upstream 副本。
func sortedUpstreams(in []*protocol.Upstream) []*protocol.Upstream {
	out := append([]*protocol.Upstream(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Metadata.Name < out[j].Metadata.Name })
	return out
}

// sortedRoutes 返回按 name 排序的 Route 副本。
func sortedRoutes(in []*protocol.Route) []*protocol.Route {
	out := append([]*protocol.Route(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Metadata.Name < out[j].Metadata.Name })
	return out
}
