package compile

import (
	"fmt"
	"sort"

	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
)

// 本文件实现编译流水线 F6 资源校验（编译层 §3 F6）：
//  1. 全资源 PGV Validate()（含 Any 承载的 typed_config 深度校验：HCM、
//     transport socket、typed_per_filter_config 等；未注册的第三方扩展类型
//     跳过——其二进制内容 PGV 无从校验，表达力上限不被本库的类型表限制）；
//  2. 跨资源引用闭合：RDS/EDS/SDS 引用名、route → cluster（EnvoyResources
//     引入的资源同样纳入）；
//  3. 错误包装带资源名并经 SourceMap 回指用户对象。
//
// 合成产物（F4 patch/EnvoyResources）无豁免地过本阶段（协议 §7.1：
// 「写坏 = 发布被拦截」）。

// pgvValidator 是 go-control-plane PGV 生成的校验接口。
type pgvValidator interface{ Validate() error }

// validateIR 执行 F6：全部错误收集不中断。
func validateIR(out *ir.IR) []CompileError {
	var errs []CompileError

	// 1. PGV + Any 深度校验。
	check := func(kind ir.ResourceKind, name string, m proto.Message) {
		if err := validateResource(m); err != nil {
			errs = append(errs, validateError(out, ir.ResourceKey{Kind: kind, Name: name}, "%v", err))
		}
	}
	for _, n := range sortedKeysOf(out.Listeners) {
		check(ir.ResourceListener, n, out.Listeners[n])
	}
	for _, n := range sortedKeysOf(out.Clusters) {
		check(ir.ResourceCluster, n, out.Clusters[n])
	}
	for _, n := range sortedKeysOf(out.Routes) {
		check(ir.ResourceRoute, n, out.Routes[n])
	}
	for _, n := range sortedKeysOf(out.Endpoints) {
		check(ir.ResourceEndpoints, n, out.Endpoints[n])
	}
	for _, n := range sortedKeysOf(out.Secrets) {
		check(ir.ResourceSecret, n, out.Secrets[n])
	}
	if out.Bootstrap != nil {
		check(ir.ResourceBootstrap, "bootstrap", out.Bootstrap)
	}

	// 2. 跨资源引用闭合。
	errs = append(errs, checkCrossRefs(out)...)
	return errs
}

// validateResource 校验单个资源：PGV Validate() + Any typed_config 递归校验。
func validateResource(m proto.Message) error {
	return validateResourceAt(m, 0)
}

func validateResourceAt(m proto.Message, depth int) error {
	if depth > 64 { // 防御异常嵌套
		return nil
	}
	if v, ok := m.(pgvValidator); ok {
		if err := v.Validate(); err != nil {
			return err
		}
	}
	for _, child := range childMessages(m) {
		if a, isAny := child.(*anypb.Any); isAny {
			if _, err := protoregistry.GlobalTypes.FindMessageByURL(a.GetTypeUrl()); err != nil {
				continue // 未注册扩展类型（如用户经 EnvoyResources 引入的插件），跳过
			}
			inner, err := a.UnmarshalNew()
			if err != nil {
				return fmt.Errorf("decode typed_config %q: %v", a.GetTypeUrl(), err)
			}
			if err := validateResourceAt(inner, depth+1); err != nil {
				return fmt.Errorf("typed_config %q: %v", a.GetTypeUrl(), err)
			}
			continue
		}
		if err := validateResourceAt(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// childMessages 枚举消息的全部直接子消息（单字段/重复字段/map 值）。
func childMessages(m proto.Message) []proto.Message {
	var out []proto.Message
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsMap():
			if isMessageKind(fd.MapValue().Kind()) {
				v.Map().Range(func(_ protoreflect.MapKey, val protoreflect.Value) bool {
					out = append(out, val.Message().Interface())
					return true
				})
			}
		case fd.IsList():
			if isMessageKind(fd.Kind()) {
				l := v.List()
				for i := 0; i < l.Len(); i++ {
					out = append(out, l.Get(i).Message().Interface())
				}
			}
		default:
			if isMessageKind(fd.Kind()) {
				out = append(out, v.Message().Interface())
			}
		}
		return true
	})
	return out
}

func isMessageKind(k protoreflect.Kind) bool {
	return k == protoreflect.MessageKind || k == protoreflect.GroupKind
}

// checkCrossRefs 校验跨资源引用闭合（编译层 §3 F6）：
// Listener 的 RDS/SDS 引用、Cluster 的 EDS 引用、route → cluster。
func checkCrossRefs(out *ir.IR) []CompileError {
	var errs []CompileError

	for _, name := range sortedKeysOf(out.Listeners) {
		lis := out.Listeners[name]
		key := ir.ResourceKey{Kind: ir.ResourceListener, Name: name}
		for _, chain := range lis.GetFilterChains() {
			for _, f := range chain.GetFilters() {
				hcm := decodeHCM(f)
				if hcm == nil {
					continue
				}
				if rds := hcm.GetRds(); rds != nil {
					if _, ok := out.Routes[rds.GetRouteConfigName()]; !ok {
						errs = append(errs, validateError(out, key,
							"HCM RDS reference %q has no matching RouteConfiguration", rds.GetRouteConfigName()))
					}
				}
				if rc := hcm.GetRouteConfig(); rc != nil {
					errs = append(errs, checkRouteConfigRefs(out, rc, key)...)
				}
			}
			down, _ := decodeDownstreamTLS(chain)
			if down == nil {
				continue
			}
			ctx := down.GetCommonTlsContext()
			for _, sds := range ctx.GetTlsCertificateSdsSecretConfigs() {
				if _, ok := out.Secrets[sds.GetName()]; !ok {
					errs = append(errs, validateError(out, key,
						"SDS certificate reference %q has no matching Secret", sds.GetName()))
				}
			}
			if vsds := ctx.GetValidationContextSdsSecretConfig(); vsds != nil {
				if _, ok := out.Secrets[vsds.GetName()]; !ok {
					errs = append(errs, validateError(out, key,
						"SDS validation context reference %q has no matching Secret", vsds.GetName()))
				}
			}
		}
	}

	for _, name := range sortedKeysOf(out.Clusters) {
		cl := out.Clusters[name]
		if cl.GetEdsClusterConfig() == nil {
			continue
		}
		if _, ok := out.Endpoints[name]; !ok {
			errs = append(errs, validateError(out,
				ir.ResourceKey{Kind: ir.ResourceCluster, Name: name},
				"EDS cluster has no matching ClusterLoadAssignment %q", name))
		}
	}

	for _, name := range sortedKeysOf(out.Routes) {
		errs = append(errs, checkRouteConfigRefs(out, out.Routes[name],
			ir.ResourceKey{Kind: ir.ResourceRoute, Name: name})...)
	}
	return errs
}

// checkRouteConfigRefs 校验 RouteConfiguration 内全部 route 的 cluster 引用闭合。
func checkRouteConfigRefs(out *ir.IR, rc *routev3.RouteConfiguration, key ir.ResourceKey) []CompileError {
	var errs []CompileError
	seen := map[string]bool{} // 同 cluster 缺失只报一次
	for _, vh := range rc.GetVirtualHosts() {
		for _, r := range vh.GetRoutes() {
			a := r.GetRoute()
			if a == nil {
				continue
			}
			var names []string
			if c := a.GetCluster(); c != "" {
				names = append(names, c)
			}
			for _, wc := range a.GetWeightedClusters().GetClusters() {
				names = append(names, wc.GetName())
			}
			for _, c := range names {
				if _, ok := out.Clusters[c]; ok || seen[c] {
					continue
				}
				seen[c] = true
				errs = append(errs, validateError(out, key,
					"route (virtual host %q) references cluster %q which does not exist", vh.GetName(), c))
			}
		}
	}
	return errs
}

// sortedKeysOf 返回资源 map 键的排序副本（错误输出确定性，编译层 §5）。
func sortedKeysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
