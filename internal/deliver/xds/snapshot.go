// Package xds 是下发层 xDS 路线组件（M-DELIVER/xds，下发层设计 §2）。
// 本文件为 Snapshot 装配纯函数 FromIR；ADS server（SnapshotCache 生命周期 +
// callbacks ACK/NACK 跟踪）见 server.go；接入 bootstrap 渲染见 bootstrap.go。
package xds

import (
	"fmt"
	"sort"

	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	resource "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
)

// downstreamTLSContextTypeURL 是 DownstreamTlsContext 的 Any type URL
// （Listener filter chain transport socket 的 TLS 形态判定）。
const downstreamTLSContextTypeURL = "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.DownstreamTlsContext"

// FromIR 把 IR 装配为 xDS Snapshot（纯函数：无 IO、无全局状态，不修改入参）。
//
// 映射（下发层 §2.1）：Endpoints→EDS、Clusters→CDS、Routes→RDS、
// Listeners→LDS、Secrets→SDS；IR.Bootstrap 不下发（接入 bootstrap 是独立
// 产物，§2.7）。version = IR.Version（全类型共用，§2.2 规则 2）。
//
// 自检：Snapshot.Consistent() 校验 EDS/RDS 引用闭合（双保险——跨资源引用
// 完整性编译 F6 已全量检查，此处防装配层自身 bug）；SDS 引用闭合不在
// go-control-plane v0.14 Consistent() 覆盖范围，由 checkSDSClosure 补齐。
// 失败即报错（Apply 同步路径 stage=assemble），非法 snapshot 不出管理面。
func FromIR(i *ir.IR) (*cache.Snapshot, error) {
	if i == nil {
		return nil, fmt.Errorf("xds: nil IR")
	}
	if i.Version == "" {
		return nil, fmt.Errorf("xds: empty IR.Version")
	}
	snap, err := cache.NewSnapshot(i.Version, map[resource.Type][]types.Resource{
		resource.EndpointType: toResources(i.Endpoints),
		resource.ClusterType:  toResources(i.Clusters),
		resource.RouteType:    toResources(i.Routes),
		resource.ListenerType: toResources(i.Listeners),
		resource.SecretType:   toResources(i.Secrets),
	})
	if err != nil {
		return nil, fmt.Errorf("xds: assemble snapshot: %w", err)
	}
	if err := snap.Consistent(); err != nil {
		return nil, fmt.Errorf("xds: snapshot inconsistent: %w", err)
	}
	if err := checkSDSClosure(i); err != nil {
		return nil, err
	}
	return snap, nil
}

// toResources 把 IR 一类资源（map[name]资源）转为 Snapshot 装配输入切片，
// 按 name 排序保证输入确定性（与 IR 哈希的排序约定一致，编译层 §5）。
func toResources[T types.Resource](m map[string]T) []types.Resource {
	out := make([]types.Resource, 0, len(m))
	for _, name := range sortedKeys(m) {
		out = append(out, m[name])
	}
	return out
}

// checkSDSClosure 校验 Listener 经 SDS 引用的 Secret 名全部存在于
// IR.Secrets（go-control-plane v0.14 的 Consistent() 不提取 SDS 引用，
// 见 pkg/cache/v3 getListenerReferences 只收集 RDS 名）。错误信息含
// listener 与 secret 名，可定位。
func checkSDSClosure(i *ir.IR) error {
	for _, name := range sortedKeys(i.Listeners) {
		refs, err := sdsSecretNames(i.Listeners[name])
		if err != nil {
			return err
		}
		for _, sec := range refs {
			if _, ok := i.Secrets[sec]; !ok {
				return fmt.Errorf("xds: listener %q references missing SDS secret %q", name, sec)
			}
		}
	}
	return nil
}

// sdsSecretNames 提取 Listener 全部 filter chain（含 default filter chain）
// transport socket 中 DownstreamTlsContext 的 SDS 引用名（证书与验证上下文）。
func sdsSecretNames(lis *listenerv3.Listener) ([]string, error) {
	chains := lis.GetFilterChains()
	if def := lis.GetDefaultFilterChain(); def != nil {
		chains = append(chains, def)
	}
	var names []string
	for idx, chain := range chains {
		a := chain.GetTransportSocket().GetTypedConfig()
		if a == nil || a.GetTypeUrl() != downstreamTLSContextTypeURL {
			continue
		}
		down := &tlsv3.DownstreamTlsContext{}
		if err := anypb.UnmarshalTo(a, down, proto.UnmarshalOptions{}); err != nil {
			return nil, fmt.Errorf("xds: listener %q filter chain %d: decode DownstreamTlsContext: %w",
				lis.GetName(), idx, err)
		}
		ctx := down.GetCommonTlsContext()
		for _, sds := range ctx.GetTlsCertificateSdsSecretConfigs() {
			names = append(names, sds.GetName())
		}
		if vc := ctx.GetValidationContextSdsSecretConfig(); vc != nil {
			names = append(names, vc.GetName())
		}
	}
	return names, nil
}

// sortedKeys 返回 map 键的排序副本。
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
