package compile

import (
	"encoding/json"
	"fmt"
	"strings"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// 本文件实现编译流水线 F4 合成（编译层 §3 F4，顺序敏感、规则固定）：
//  1. envoyPatch 先于 EnvoyResources；同对象多个 patch 按书写顺序应用；
//  2. patch 施加于 target 指向的本对象产出资源（经 SourceMap 定位）；
//     merge = protojson → JSON Merge Patch（RFC 7386）→ protojson 回转；
//     jsonPatch = RFC 6902；回转失败（字段不存在/类型不符）= 编译错误并回指 patch 位置；
//  3. EnvoyResources 按 @type 分发入 IR 集合；重名默认报错，
//     allowOverride: true 整体替换并更新 SourceMap 归属；
//  4. 合成产物无豁免地进入 F6 校验。
//
// patch 域内的 JSON 一律使用 proto 原始字段名（snake_case，
// protojson UseProtoNames），与协议 §7 文档示例的写法一致。

// synthesis 是 F4 的工作集：F3 逻辑资源 + 编译产物标记 + SourceMap。
// 全部切片保持 F3 产出的按名排序（确定性）；F5 将其形态化为 IR。
//
// 证书与 HCM 的绑定一律按资源名关联（crt/<listener>/<n> ↔ 第 n 条 TLS filter
// chain、rc/<listener> ↔ HCM RDS 名），不持有跨资源指针——listener 级 patch 会
// 整体替换 Listener 资源，指针绑定会悬空；F5 按名重新定位（见 materialize.go）。
type synthesis struct {
	listeners []*listenerv3.Listener
	routes    []*routev3.RouteConfiguration
	clusters  []*clusterv3.Cluster
	endpoints []*endpointv3.ClusterLoadAssignment // 仅 EnvoyResources 直接提供的 CLA
	secrets   []*tlsv3.Secret                     // 编译产物证书 + EnvoyResources 提供的 Secret

	compiledListeners map[string]bool // F3 产出的 Listener 资源名（lis/<n>）
	compiledSecrets   map[string]bool // F3 产出的证书 Secret 名（crt/<listener>/<n>）
	sourceMap         map[ir.ResourceKey]ir.SourceRef
}

// synthesize 执行 F4：由 F3 产物建立工作集，依次应用 envoyPatch 与 EnvoyResources。
// 纯函数：不读文件系统（证书 SAN 提取已在 F3 完成，SD5）。
func synthesize(cs *protocol.ConfigSet, res *buildResult) (*synthesis, []CompileError) {
	syn := &synthesis{
		listeners:         append([]*listenerv3.Listener(nil), res.listeners...),
		routes:            append([]*routev3.RouteConfiguration(nil), res.routes...),
		clusters:          append([]*clusterv3.Cluster(nil), res.clusters...),
		compiledListeners: map[string]bool{},
		compiledSecrets:   map[string]bool{},
		sourceMap:         map[ir.ResourceKey]ir.SourceRef{},
	}
	syn.initSourceMap(cs)
	var errs []CompileError
	errs = append(errs, syn.extractCompiledSecrets(cs)...)
	errs = append(errs, syn.applyEnvoyPatches(cs)...)
	errs = append(errs, syn.mergeEnvoyResources(cs)...)
	return syn, errs
}

// initSourceMap 建立编译产物的初始溯源：lis/<n>/rc/<lis> → Listener、us/<n> → Upstream、
// bootstrap → Gateway（隐式默认 Gateway 无文件来源）。jwt-jwks/* 抓取集群由策略装配
// 派生（C4），M0 不回指单一 Policy。
func (s *synthesis) initSourceMap(cs *protocol.ConfigSet) {
	for _, l := range cs.Listeners {
		src := ir.SourceRef{File: l.Origin.File, Kind: protocol.KindListener, Name: l.Metadata.Name}
		s.sourceMap[ir.ResourceKey{Kind: ir.ResourceListener, Name: listenerResourceName(l.Metadata.Name)}] = src
		s.sourceMap[ir.ResourceKey{Kind: ir.ResourceRoute, Name: routeConfigName(l.Metadata.Name)}] = src
	}
	for _, u := range cs.Upstreams {
		s.sourceMap[ir.ResourceKey{Kind: ir.ResourceCluster, Name: clusterResourceName(u.Metadata.Name)}] = ir.SourceRef{File: u.Origin.File, Kind: protocol.KindUpstream, Name: u.Metadata.Name}
	}
	gw := ir.SourceRef{Kind: protocol.KindGateway, Name: protocol.DefaultGatewayName}
	if cs.Gateway != nil {
		gw.File = cs.Gateway.Origin.File
	}
	s.sourceMap[ir.ResourceKey{Kind: ir.ResourceBootstrap, Name: "bootstrap"}] = gw
}

// extractCompiledSecrets 把编译产物 Listener 的证书从 filter chain 的
// DownstreamTlsContext 提取为 Secret crt/<listener>/<n>（C1：Listener → secret 的
// patch 目标；F5 按模式决定 SDS 引用还是回写内联，均按名重新定位）。
func (s *synthesis) extractCompiledSecrets(cs *protocol.ConfigSet) []CompileError {
	var errs []CompileError
	byName := map[string]*protocol.Listener{}
	for _, l := range cs.Listeners {
		byName[l.Metadata.Name] = l
	}
	for _, lis := range s.listeners {
		name := strings.TrimPrefix(lis.GetName(), "lis/")
		pl := byName[name]
		if pl == nil {
			continue // 非编译产物（防御；EnvoyResources 合并在本阶段之后）
		}
		s.compiledListeners[lis.GetName()] = true
		certIdx := 0
		for _, chain := range lis.GetFilterChains() {
			ts := chain.GetTransportSocket()
			if ts == nil || ts.GetTypedConfig() == nil {
				continue
			}
			down := &tlsv3.DownstreamTlsContext{}
			if err := ts.GetTypedConfig().UnmarshalTo(down); err != nil {
				continue // 非 TLS transport socket（防御）
			}
			if tc := firstTlsCertificate(down); tc != nil {
				secret := &tlsv3.Secret{
					Name: secretResourceName(name, certIdx),
					Type: &tlsv3.Secret_TlsCertificate{TlsCertificate: tc},
				}
				s.secrets = append(s.secrets, secret)
				s.compiledSecrets[secret.GetName()] = true
				s.sourceMap[ir.ResourceKey{Kind: ir.ResourceSecret, Name: secret.GetName()}] = ir.SourceRef{
					File: pl.Origin.File, Kind: protocol.KindListener, Name: name,
					Path: fmt.Sprintf("spec.tls.certificates[%d]", certIdx),
				}
			}
			certIdx++
		}
	}
	return errs
}

// firstTlsCertificate 返回 DownstreamTlsContext 的首张证书（F3 每 chain 恰好一张）。
func firstTlsCertificate(down *tlsv3.DownstreamTlsContext) *tlsv3.TlsCertificate {
	tcs := down.GetCommonTlsContext().GetTlsCertificates()
	if len(tcs) == 0 {
		return nil
	}
	return tcs[0]
}

// routeConfig 按名查找工作集中的 RouteConfiguration。
func (s *synthesis) routeConfig(name string) *routev3.RouteConfiguration {
	for _, rc := range s.routes {
		if rc.GetName() == name {
			return rc
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// envoyPatch
// ---------------------------------------------------------------------------

// patchTarget 是解析后的 envoyPatch.target：资源类别 + 可选子定位（rule 名）。
type patchTarget struct {
	kind string // listener | secret | virtualHost | route | cluster | endpoints
	sub  string // route/<ruleName> 的 ruleName
}

// parsePatchTarget 解析 target 字符串（"route/<name>" 允许一段子定位）。
func parsePatchTarget(target string) patchTarget {
	kind, sub, _ := strings.Cut(target, "/")
	return patchTarget{kind: kind, sub: sub}
}

// patchTargets 是 C1 决议的 target 合法取值定表（按对象 kind）：
// Listener→listener/secret；Route→virtualHost/route；Upstream→cluster/endpoints；
// Gateway→bootstrap 不允许（C2 留 M1，故 Gateway 无任何合法 target）。
var patchTargets = map[protocol.Kind]map[string]bool{
	protocol.KindListener: {"listener": true, "secret": true},
	protocol.KindRoute:    {"virtualHost": true, "route": true},
	protocol.KindUpstream: {"cluster": true, "endpoints": true},
	protocol.KindGateway:  {},
}

// applyEnvoyPatches 按配置顺序应用全部对象的 envoyPatch（同对象按书写顺序）。
func (s *synthesis) applyEnvoyPatches(cs *protocol.ConfigSet) []CompileError {
	var errs []CompileError
	if cs.Gateway != nil {
		for i := range cs.Gateway.Spec.EnvoyPatch {
			errs = append(errs, patchError(cs.Gateway.Origin, protocol.KindGateway, cs.Gateway.Metadata.Name,
				fmt.Sprintf("spec.envoyPatch[%d].target", i),
				"Gateway has no patchable target in v0 (bootstrap patch is not allowed; see decision C2, planned for M1)"))
		}
	}
	for _, l := range cs.Listeners {
		errs = append(errs, s.patchObject(l.Origin, protocol.KindListener, l.Metadata.Name, l.Spec.EnvoyPatch, l)...)
	}
	for _, r := range cs.Routes {
		errs = append(errs, s.patchObject(r.Origin, protocol.KindRoute, r.Metadata.Name, r.Spec.EnvoyPatch, r)...)
	}
	for _, u := range cs.Upstreams {
		errs = append(errs, s.patchObject(u.Origin, protocol.KindUpstream, u.Metadata.Name, u.Spec.EnvoyPatch, u)...)
	}
	return errs
}

// patchObject 应用单个对象的 patch 列表；target 合法性按 C1 定表校验，
// 目标缺失/定位失败/patch 回转失败均为编译错误并回指 patch 位置。
func (s *synthesis) patchObject(origin protocol.Origin, kind protocol.Kind, name string, patches []protocol.EnvoyPatch, obj any) []CompileError {
	var errs []CompileError
	for i := range patches {
		p := &patches[i]
		base := fmt.Sprintf("spec.envoyPatch[%d]", i)
		t := parsePatchTarget(p.Target)
		if !patchTargets[kind][t.kind] {
			errs = append(errs, patchError(origin, kind, name, base+".target",
				"invalid target %q for %s (allowed: %s)", p.Target, kind, allowedTargets(kind)))
			continue
		}
		targets, terr := s.locateTargets(kind, name, t, obj)
		if terr != nil {
			errs = append(errs, patchError(origin, kind, name, base+".target", "%v", terr))
			continue
		}
		for _, lt := range targets {
			if err := lt.apply(p); err != nil {
				errs = append(errs, patchError(origin, kind, name, base,
					"target %q: %v", p.Target, err))
			}
		}
	}
	return errs
}

// allowedTargets 返回某 kind 的合法 target 列表（错误消息用）。
func allowedTargets(kind protocol.Kind) string {
	set := patchTargets[kind]
	if len(set) == 0 {
		return "(none)"
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	return strings.Join(out, " | ")
}

// locatedTarget 是一个已定位的 patch 目标：replace 把 patch 后的新资源写回工作集。
type locatedTarget struct {
	msg     proto.Message
	replace func(m proto.Message)
}

// apply 对目标执行一次 patch：protojson → patch → protojson 回转，成功后写回。
func (lt locatedTarget) apply(p *protocol.EnvoyPatch) error {
	doc, err := patchMarshal.Marshal(lt.msg)
	if err != nil {
		return fmt.Errorf("marshal target resource: %v", err)
	}
	var patched []byte
	switch p.Op {
	case protocol.PatchOpMerge:
		patched, err = jsonpatch.MergePatch(doc, p.Value)
	case protocol.PatchOpJSONPatch:
		var ops jsonpatch.Patch
		ops, derr := jsonpatch.DecodePatch(p.Value)
		if derr != nil {
			return fmt.Errorf("decode jsonPatch: %v", derr)
		}
		patched, err = ops.Apply(doc)
	}
	if err != nil {
		return fmt.Errorf("apply %s: %v", p.Op, err)
	}
	out := lt.msg.ProtoReflect().New().Interface()
	if err := protojson.Unmarshal(patched, out); err != nil {
		return fmt.Errorf("round-trip back to proto (unknown field or type mismatch): %v", err)
	}
	lt.replace(out)
	return nil
}

// patchMarshal 是 patch 域的 protojson 序列化：proto 原始字段名（snake_case），
// 与协议 §7 文档示例一致；EmitDefaultValues 使未显式设置的字段也出现在
// patch 文档中（缺省字段 protojson 默认省略，jsonPatch 的 replace 需要目标键存在，
// 协议 §7.1 示例正是 replace /dns_lookup_family 这种写法）。
var patchMarshal = protojson.MarshalOptions{UseProtoNames: true, EmitDefaultValues: true}

// locateTargets 按 C1 定表把 target 解析为工作集中的具体资源。
func (s *synthesis) locateTargets(kind protocol.Kind, name string, t patchTarget, obj any) ([]locatedTarget, error) {
	switch kind {
	case protocol.KindListener:
		return s.locateListenerTargets(name, t)
	case protocol.KindRoute:
		return s.locateRouteTargets(name, t, obj.(*protocol.Route))
	case protocol.KindUpstream:
		return s.locateUpstreamTargets(name, t)
	}
	return nil, fmt.Errorf("no patchable target for %s", kind)
}

func (s *synthesis) locateListenerTargets(name string, t patchTarget) ([]locatedTarget, error) {
	switch t.kind {
	case "listener":
		rn := listenerResourceName(name)
		for i, lis := range s.listeners {
			if lis.GetName() == rn {
				idx := i
				return []locatedTarget{{lis, func(m proto.Message) { s.listeners[idx] = m.(*listenerv3.Listener) }}}, nil
			}
		}
		return nil, fmt.Errorf("listener produced no resource %q", rn)
	case "secret":
		// 证书 Secret 按名定位：crt/<listener>/<n>，patch 依次施加于该 Listener
		// 的全部编译产物证书（多证书时同一 patch 作用于每一张，见 T5 进展记录）。
		var out []locatedTarget
		for i, sec := range s.secrets {
			if !s.compiledSecrets[sec.GetName()] || !strings.HasPrefix(sec.GetName(), "crt/"+name+"/") {
				continue
			}
			idx := i
			out = append(out, locatedTarget{sec, func(m proto.Message) { s.secrets[idx] = m.(*tlsv3.Secret) }})
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("listener %q has no certificates (secret target applies to the listener's certificate Secrets crt/%s/<n>)", name, name)
		}
		return out, nil
	}
	return nil, fmt.Errorf("invalid target kind %q", t.kind)
}

func (s *synthesis) locateRouteTargets(name string, t patchTarget, r *protocol.Route) ([]locatedTarget, error) {
	vhName := virtualHostName(name)
	switch t.kind {
	case "virtualHost":
		var out []locatedTarget
		for _, rc := range s.routes {
			for i, vh := range rc.GetVirtualHosts() {
				if vh.GetName() != vhName {
					continue
				}
				rcv, idx := rc, i
				out = append(out, locatedTarget{vh, func(m proto.Message) { rcv.VirtualHosts[idx] = m.(*routev3.VirtualHost) }})
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("route produced no virtual host %q", vhName)
		}
		return out, nil
	case "route":
		if t.sub == "" {
			return nil, fmt.Errorf("rule-level patch requires target \"route/<ruleName>\" (rules need an optional name to be addressable, see protocol §3.3 note 4)")
		}
		idx := -1
		for i := range r.Spec.Rules {
			if r.Spec.Rules[i].Name == t.sub {
				idx = i
				break
			}
		}
		if idx < 0 {
			return nil, fmt.Errorf("no rule named %q in route %q (unnamed rules cannot be addressed by rule-level patch)", t.sub, name)
		}
		var out []locatedTarget
		for _, rc := range s.routes {
			for _, vh := range rc.GetVirtualHosts() {
				if vh.GetName() != vhName || idx >= len(vh.GetRoutes()) {
					continue
				}
				vhv, ridx := vh, idx
				out = append(out, locatedTarget{vhv.Routes[ridx], func(m proto.Message) { vhv.Routes[ridx] = m.(*routev3.Route) }})
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("route produced no virtual host %q carrying rule %q", vhName, t.sub)
		}
		return out, nil
	}
	return nil, fmt.Errorf("invalid target kind %q", t.kind)
}

func (s *synthesis) locateUpstreamTargets(name string, t patchTarget) ([]locatedTarget, error) {
	rn := clusterResourceName(name)
	for i, cl := range s.clusters {
		if cl.GetName() != rn {
			continue
		}
		switch t.kind {
		case "cluster":
			idx := i
			return []locatedTarget{{cl, func(m proto.Message) { s.clusters[idx] = m.(*clusterv3.Cluster) }}}, nil
		case "endpoints":
			cla := cl.GetLoadAssignment()
			if cla == nil {
				return nil, fmt.Errorf("cluster %q has no inline endpoints to patch", rn)
			}
			clRef := cl
			return []locatedTarget{{cla, func(m proto.Message) { clRef.LoadAssignment = m.(*endpointv3.ClusterLoadAssignment) }}}, nil
		}
	}
	return nil, fmt.Errorf("upstream produced no resource %q", rn)
}

// ---------------------------------------------------------------------------
// EnvoyResources 合并
// ---------------------------------------------------------------------------

// envoyResourceTypes 是 EnvoyResources 支持分发的 @type 全集（v0 资源级，C2）。
var envoyResourceTypes = map[string]func() proto.Message{
	"type.googleapis.com/envoy.config.listener.v3.Listener":                func() proto.Message { return &listenerv3.Listener{} },
	"type.googleapis.com/envoy.config.cluster.v3.Cluster":                  func() proto.Message { return &clusterv3.Cluster{} },
	"type.googleapis.com/envoy.config.route.v3.RouteConfiguration":         func() proto.Message { return &routev3.RouteConfiguration{} },
	"type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment":   func() proto.Message { return &endpointv3.ClusterLoadAssignment{} },
	"type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.Secret": func() proto.Message { return &tlsv3.Secret{} },
}

// mergeEnvoyResources 把全部 EnvoyResources 对象的原生资源按 @type 分发进工作集
// （编译层 §3 F4 规则 3）：与编译产物（或先合并的原生资源）重名默认报错，
// allowOverride: true 整体替换并更新 SourceMap 归属。
func (s *synthesis) mergeEnvoyResources(cs *protocol.ConfigSet) []CompileError {
	var errs []CompileError
	for _, er := range cs.EnvoyResources {
		for i, raw := range er.Spec.Resources {
			path := fmt.Sprintf("spec.resources[%d]", i)
			key, msg, err := decodeEnvoyResource(raw)
			if err != nil {
				errs = append(errs, patchError(er.Origin, protocol.KindEnvoyResources, er.Metadata.Name, path, "%v", err))
				continue
			}
			src := ir.SourceRef{File: er.Origin.File, Kind: protocol.KindEnvoyResources, Name: er.Metadata.Name, Path: path}
			if err := s.insertResource(key, msg); err != nil {
				if !er.Spec.AllowOverride {
					errs = append(errs, patchError(er.Origin, protocol.KindEnvoyResources, er.Metadata.Name, path,
						"%v (set allowOverride: true to replace the compiled resource)", err))
					continue
				}
				s.replaceResource(key, msg)
			}
			s.sourceMap[key] = src
		}
	}
	return errs
}

// decodeEnvoyResource 解析一条带 @type 的原生资源 JSON：分发到具体 proto 类型并反序列化。
func decodeEnvoyResource(raw protocol.RawJSON) (ir.ResourceKey, proto.Message, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ir.ResourceKey{}, nil, fmt.Errorf("invalid JSON object: %v", err)
	}
	var typeURL string
	if err := json.Unmarshal(fields["@type"], &typeURL); err != nil {
		return ir.ResourceKey{}, nil, fmt.Errorf("invalid \"@type\": %v", err)
	}
	newMsg, ok := envoyResourceTypes[typeURL]
	if !ok {
		return ir.ResourceKey{}, nil, fmt.Errorf("unsupported \"@type\" %q (supported: Listener | Cluster | RouteConfiguration | ClusterLoadAssignment | Secret)", typeURL)
	}
	delete(fields, "@type") // protojson 对非 Any 消息不认识 @type 键
	body, err := json.Marshal(fields)
	if err != nil {
		return ir.ResourceKey{}, nil, err
	}
	msg := newMsg()
	if err := protojson.Unmarshal(body, msg); err != nil {
		return ir.ResourceKey{}, nil, fmt.Errorf("decode %s: %v", typeURL, err)
	}
	key := ir.ResourceKey{Kind: resourceKindOf(msg), Name: resourceNameOf(msg)}
	return key, msg, nil
}

// resourceKindOf 返回资源对应的 IR 类别。
func resourceKindOf(msg proto.Message) ir.ResourceKind {
	switch msg.(type) {
	case *listenerv3.Listener:
		return ir.ResourceListener
	case *clusterv3.Cluster:
		return ir.ResourceCluster
	case *routev3.RouteConfiguration:
		return ir.ResourceRoute
	case *endpointv3.ClusterLoadAssignment:
		return ir.ResourceEndpoints
	case *tlsv3.Secret:
		return ir.ResourceSecret
	}
	return ""
}

// resourceNameOf 返回资源名（CLA 用 cluster_name）。
func resourceNameOf(msg proto.Message) string {
	switch m := msg.(type) {
	case *listenerv3.Listener:
		return m.GetName()
	case *clusterv3.Cluster:
		return m.GetName()
	case *routev3.RouteConfiguration:
		return m.GetName()
	case *endpointv3.ClusterLoadAssignment:
		return m.GetClusterName()
	case *tlsv3.Secret:
		return m.GetName()
	}
	return ""
}

// insertResource 向工作集插入原生资源；重名时报错（由调用方按 allowOverride 决策）。
func (s *synthesis) insertResource(key ir.ResourceKey, msg proto.Message) error {
	if s.hasResource(key) {
		return fmt.Errorf("resource %s conflicts with an existing resource of the same name", key)
	}
	s.appendResource(key, msg)
	return nil
}

// replaceResource 整体替换同名资源（allowOverride: true）。
func (s *synthesis) replaceResource(key ir.ResourceKey, msg proto.Message) {
	switch key.Kind {
	case ir.ResourceListener:
		for i, l := range s.listeners {
			if l.GetName() == key.Name {
				s.listeners[i] = msg.(*listenerv3.Listener)
				return
			}
		}
	case ir.ResourceCluster:
		for i, c := range s.clusters {
			if c.GetName() == key.Name {
				s.clusters[i] = msg.(*clusterv3.Cluster)
				return
			}
		}
	case ir.ResourceRoute:
		for i, rc := range s.routes {
			if rc.GetName() == key.Name {
				s.routes[i] = msg.(*routev3.RouteConfiguration)
				return
			}
		}
	case ir.ResourceEndpoints:
		for i, cla := range s.endpoints {
			if cla.GetClusterName() == key.Name {
				s.endpoints[i] = msg.(*endpointv3.ClusterLoadAssignment)
				return
			}
		}
	case ir.ResourceSecret:
		for i, sec := range s.secrets {
			if sec.GetName() == key.Name {
				s.secrets[i] = msg.(*tlsv3.Secret)
				return
			}
		}
	}
	s.appendResource(key, msg)
}

// hasResource 报告工作集中是否已存在同名资源。
func (s *synthesis) hasResource(key ir.ResourceKey) bool {
	switch key.Kind {
	case ir.ResourceListener:
		for _, l := range s.listeners {
			if l.GetName() == key.Name {
				return true
			}
		}
	case ir.ResourceCluster:
		for _, c := range s.clusters {
			if c.GetName() == key.Name {
				return true
			}
		}
	case ir.ResourceRoute:
		for _, rc := range s.routes {
			if rc.GetName() == key.Name {
				return true
			}
		}
	case ir.ResourceEndpoints:
		for _, cla := range s.endpoints {
			if cla.GetClusterName() == key.Name {
				return true
			}
		}
	case ir.ResourceSecret:
		for _, sec := range s.secrets {
			if sec.GetName() == key.Name {
				return true
			}
		}
	}
	return false
}

// appendResource 追加资源到对应集合。
func (s *synthesis) appendResource(key ir.ResourceKey, msg proto.Message) {
	switch key.Kind {
	case ir.ResourceListener:
		s.listeners = append(s.listeners, msg.(*listenerv3.Listener))
	case ir.ResourceCluster:
		s.clusters = append(s.clusters, msg.(*clusterv3.Cluster))
	case ir.ResourceRoute:
		s.routes = append(s.routes, msg.(*routev3.RouteConfiguration))
	case ir.ResourceEndpoints:
		s.endpoints = append(s.endpoints, msg.(*endpointv3.ClusterLoadAssignment))
	case ir.ResourceSecret:
		s.secrets = append(s.secrets, msg.(*tlsv3.Secret))
	}
}

// decodeHCM 从 Listener filter 解出 HCM（非 HCM 返回 nil）。
func decodeHCM(f *listenerv3.Filter) *hcmv3.HttpConnectionManager {
	tc := f.GetTypedConfig()
	if tc == nil || !strings.HasSuffix(tc.GetTypeUrl(), "HttpConnectionManager") {
		return nil
	}
	hcm := &hcmv3.HttpConnectionManager{}
	if err := tc.UnmarshalTo(hcm); err != nil {
		return nil
	}
	return hcm
}

// findHCMFilter 返回 Listener 中的 HCM filter（编译产物各 chain 共享同一指针）。
func findHCMFilter(lis *listenerv3.Listener) *listenerv3.Filter {
	for _, chain := range lis.GetFilterChains() {
		for _, f := range chain.GetFilters() {
			if decodeHCM(f) != nil {
				return f
			}
		}
	}
	return nil
}
