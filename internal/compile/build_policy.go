package compile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	rbacconfigv3 "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	corsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3"
	extauthzv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	jwtv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/jwt_authn/v3"
	localratelimitv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/local_ratelimit/v3"
	rbacv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// ─── 策略归一化（协议 §3.5 规则 3，技术设计 §4 风险项）───

// effectivePolicies 是一条 rule 最终生效的策略集：
// 同类型就近覆盖整体替换（rule > Route > Listener > Gateway），每种类型至多一条。
type effectivePolicies struct {
	headerModifier *protocol.HeaderModifierPolicy
	cors           *protocol.CORSPolicy
	rateLimit      *protocol.RateLimitPolicy
	jwt            *protocol.JWTPolicy
	extAuth        *protocol.ExtAuthPolicy
	ipAccess       *protocol.IPAccessPolicy
	unsupported    []string // 尚未实现的策略类型（basicAuth），按出现序
}

// normalizePolicies 按就近覆盖合并四级 policies（levels 调用顺序须为
// Gateway → Listener → Route → rule，后者覆盖前者的同类型条目）。
// 引用与内联等价（协议 §3.5 规则 2）；引用悬空已在 F2 报错，此处跳过。
func normalizePolicies(lk *linked, levels ...[]protocol.PolicyAttachment) effectivePolicies {
	var eff effectivePolicies
	for _, level := range levels {
		for _, att := range level {
			spec := att.Inline
			if spec == nil {
				pol, ok := lk.policies[att.Ref]
				if !ok {
					continue // 悬空引用已报 F2 错误
				}
				spec = &pol.Spec
			}
			switch {
			case spec.HeaderModifier != nil:
				eff.headerModifier = spec.HeaderModifier
			case spec.CORS != nil:
				eff.cors = spec.CORS
			case spec.RateLimit != nil:
				eff.rateLimit = spec.RateLimit
			case spec.JWT != nil:
				eff.jwt = spec.JWT
			case spec.ExtAuth != nil:
				eff.extAuth = spec.ExtAuth
			case spec.IPAccess != nil:
				eff.ipAccess = spec.IPAccess
			default:
				// basicAuth 尚未实现，显式报错而非静默丢弃。
				eff.unsupported = append(eff.unsupported, policyTypeKey(spec))
			}
		}
	}
	return eff
}

// ─── PolicyBuilder：HCM filter 链 + per-rule typed_per_filter_config ───

// httpFilters 生成 HCM 的 HTTP filter 链。固定顺序（编译层 §3，协议 §3.5 规则 4）：
//
//	cors → jwt_authn → ext_authz → local_ratelimit → router
//
// 完整目标链序为 rbac → cors → jwt_authn → ext_authz → local_ratelimit → router；
// M0 不实现 rbac/ext_authz（P1），此处在链结构中预留其位置（cors 前、jwt_authn 后），
// 后续加入时无需挪动既有 filter。所有策略 filter 常驻链上、默认 pass-through，
// 实际生效范围由 per-rule typed_per_filter_config 控制，避免 filter chain 结构抖动。
func httpFilters(jwtAsm *jwtAssembly, extAuthAsm *extAuthAssembly) ([]*hcmv3.HttpFilter, []CompileError) {
	rbacCfg, err := marshalAny(&rbacv3.RBAC{})
	if err != nil {
		return nil, []CompileError{{Stage: StageBuild, Severity: SeverityError, Message: err.Error()}}
	}
	corsCfg, err := marshalAny(&corsv3.Cors{})
	if err != nil {
		return nil, []CompileError{{Stage: StageBuild, Severity: SeverityError, Message: err.Error()}}
	}
	jwtCfg, err := marshalAny(jwtAsm.filterConfig())
	if err != nil {
		return nil, []CompileError{{Stage: StageBuild, Severity: SeverityError, Message: err.Error()}}
	}
	// filter 层无 token bucket = 全局限流关闭（pass-through），per-rule 配置开启。
	lrlCfg, err := marshalAny(&localratelimitv3.LocalRateLimit{StatPrefix: "local_ratelimit"})
	if err != nil {
		return nil, []CompileError{{Stage: StageBuild, Severity: SeverityError, Message: err.Error()}}
	}
	routerCfg, err := marshalAny(&routerv3.Router{})
	if err != nil {
		return nil, []CompileError{{Stage: StageBuild, Severity: SeverityError, Message: err.Error()}}
	}
	mk := func(name string, cfg *anypb.Any) *hcmv3.HttpFilter {
		return &hcmv3.HttpFilter{
			Name:       name,
			ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: cfg},
		}
	}
	filters := []*hcmv3.HttpFilter{
		mk(rbacFilterName, rbacCfg),
		mk(corsFilterName, corsCfg),
		mk(jwtAuthnFilterName, jwtCfg),
	}
	if extAuthAsm.enabled() {
		extCfg, err := marshalAny(extAuthAsm.config)
		if err != nil {
			return nil, []CompileError{{Stage: StageBuild, Severity: SeverityError, Message: err.Error()}}
		}
		filters = append(filters, mk(extAuthzFilterName, extCfg))
	}
	filters = append(filters, mk(localRateLimitFilterName, lrlCfg), mk(routerFilterName, routerCfg))
	return filters, nil
}

// typedPerFilterConfig 生成一条 rule 的 per-route 策略配置（编译层 §3 策略表）：
// cors / local_ratelimit / jwt_authn 各一条 Any；未生效的类型不出现（filter 默认 pass-through）。
func (ctx *buildContext) typedPerFilterConfig(eff effectivePolicies, jwtAsm *jwtAssembly,
	r *protocol.Route, ruleIdx int,
) (map[string]*anypb.Any, []CompileError) {
	var errs []CompileError
	rulePath := fmt.Sprintf("spec.rules[%d].policies", ruleIdx)
	tpc := map[string]*anypb.Any{}
	put := func(filterName string, cfg *anypb.Any, err error) {
		if err != nil {
			errs = append(errs, buildError(r.Origin, protocol.KindRoute, r.Metadata.Name, rulePath,
				"marshal per-rule config for %s: %v", filterName, err))
			return
		}
		tpc[filterName] = cfg
	}
	if eff.cors != nil {
		cfg, err := marshalAny(buildCorsPolicy(eff.cors))
		put(corsFilterName, cfg, err)
	}
	if eff.ipAccess != nil {
		cfg, err := marshalAny(buildIPAccess(eff.ipAccess))
		put(rbacFilterName, cfg, err)
	}
	if eff.rateLimit != nil {
		cfg, err := marshalAny(buildLocalRateLimit(eff.rateLimit))
		put(localRateLimitFilterName, cfg, err)
	}
	if eff.jwt != nil {
		reqName, jerr := ctx.registerJWT(jwtAsm, eff.jwt)
		if jerr != nil {
			errs = append(errs, buildError(r.Origin, protocol.KindRoute, r.Metadata.Name, rulePath, "%v", jerr))
		} else {
			cfg, err := marshalAny(&jwtv3.PerRouteConfig{
				RequirementSpecifier: &jwtv3.PerRouteConfig_RequirementName{RequirementName: reqName},
			})
			put(jwtAuthnFilterName, cfg, err)
		}
	}
	if eff.extAuth != nil {
		perRoute := &extauthzv3.ExtAuthzPerRoute{}
		if eff.extAuth.Disabled {
			perRoute.Override = &extauthzv3.ExtAuthzPerRoute_Disabled{Disabled: true}
		} else {
			// Empty CheckSettings is an explicit enabled marker used by the Listener
			// finalization pass to distinguish protected from unconfigured routes.
			perRoute.Override = &extauthzv3.ExtAuthzPerRoute_CheckSettings{
				CheckSettings: &extauthzv3.CheckSettings{},
			}
		}
		cfg, err := marshalAny(perRoute)
		put(extAuthzFilterName, cfg, err)
	}
	if len(tpc) == 0 {
		return nil, errs
	}
	return tpc, errs
}

func buildIPAccess(p *protocol.IPAccessPolicy) *rbacv3.RBACPerRoute {
	principals := []*rbacconfigv3.Principal{}
	allow := cidrPrincipals(p.Allow)
	if len(allow) == 0 {
		principals = append(principals, &rbacconfigv3.Principal{
			Identifier: &rbacconfigv3.Principal_Any{Any: true},
		})
	} else {
		principals = append(principals, orPrincipal(allow))
	}
	if deny := cidrPrincipals(p.Deny); len(deny) > 0 {
		principals = append(principals, &rbacconfigv3.Principal{
			Identifier: &rbacconfigv3.Principal_NotId{NotId: orPrincipal(deny)},
		})
	}
	principal := principals[0]
	if len(principals) > 1 {
		principal = &rbacconfigv3.Principal{
			Identifier: &rbacconfigv3.Principal_AndIds{AndIds: &rbacconfigv3.Principal_Set{Ids: principals}},
		}
	}
	return &rbacv3.RBACPerRoute{Rbac: &rbacv3.RBAC{Rules: &rbacconfigv3.RBAC{
		Action: rbacconfigv3.RBAC_ALLOW,
		Policies: map[string]*rbacconfigv3.Policy{"ip-access": {
			Permissions: []*rbacconfigv3.Permission{{Rule: &rbacconfigv3.Permission_Any{Any: true}}},
			Principals:  []*rbacconfigv3.Principal{principal},
		}},
	}}}
}

func cidrPrincipals(raw []string) []*rbacconfigv3.Principal {
	prefixes := make([]netip.Prefix, 0, len(raw))
	seen := map[string]bool{}
	for _, value := range raw {
		prefix := netip.MustParsePrefix(value).Masked()
		key := prefix.String()
		if !seen[key] {
			seen[key] = true
			prefixes = append(prefixes, prefix)
		}
	}
	sort.Slice(prefixes, func(i, j int) bool { return prefixes[i].String() < prefixes[j].String() })
	out := make([]*rbacconfigv3.Principal, 0, len(prefixes))
	for _, prefix := range prefixes {
		out = append(out, &rbacconfigv3.Principal{Identifier: &rbacconfigv3.Principal_RemoteIp{
			RemoteIp: &corev3.CidrRange{
				AddressPrefix: prefix.Addr().String(), PrefixLen: wrapperspb.UInt32(uint32(prefix.Bits())),
			},
		}})
	}
	return out
}

func orPrincipal(ids []*rbacconfigv3.Principal) *rbacconfigv3.Principal {
	if len(ids) == 1 {
		return ids[0]
	}
	return &rbacconfigv3.Principal{
		Identifier: &rbacconfigv3.Principal_OrIds{OrIds: &rbacconfigv3.Principal_Set{Ids: ids}},
	}
}

// applyHeaderModifier 把 headerModifier 策略落到 route 层字段（不占 filter，编译层 §3 策略表）。
// set → OVERWRITE_IF_EXISTS_OR_ADD；add → APPEND_IF_EXISTS_OR_ADD；map 按 key 排序（确定性）。
func applyHeaderModifier(route *routev3.Route, hm *protocol.HeaderModifierPolicy) {
	if hm == nil {
		return
	}
	if req := hm.Request; req != nil {
		route.RequestHeadersToAdd = append(route.RequestHeadersToAdd, headerAdds(req)...)
		route.RequestHeadersToRemove = append(route.RequestHeadersToRemove, req.Remove...)
	}
	if resp := hm.Response; resp != nil {
		route.ResponseHeadersToAdd = append(route.ResponseHeadersToAdd, headerAdds(resp)...)
		route.ResponseHeadersToRemove = append(route.ResponseHeadersToRemove, resp.Remove...)
	}
}

// headerAdds 把 HeaderOps 的 set/add 映射为 HeaderValueOption（按 key 排序输出）。
func headerAdds(ops *protocol.HeaderOps) []*corev3.HeaderValueOption {
	var out []*corev3.HeaderValueOption
	for _, k := range sortedKeys(ops.Set) {
		out = append(out, &corev3.HeaderValueOption{
			Header:       &corev3.HeaderValue{Key: k, Value: ops.Set[k]},
			AppendAction: corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
		})
	}
	for _, k := range sortedKeys(ops.Add) {
		out = append(out, &corev3.HeaderValueOption{
			Header:       &corev3.HeaderValue{Key: k, Value: ops.Add[k]},
			AppendAction: corev3.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD,
		})
	}
	return out
}

// buildCorsPolicy 生成 per-route CorsPolicy（cors filter 的 per-route 配置消息，
// 编译层 §3 策略表）。allowOrigins 支持通配（协议 §3.5）：含 "*" 的条目转
// safe_regex；单独的 "*" = 放行全部源（Envoy 语义：allow_origin_string_match 为空即放行全部）。
func buildCorsPolicy(p *protocol.CORSPolicy) *corsv3.CorsPolicy {
	c := &corsv3.CorsPolicy{
		AllowMethods: strings.Join(p.AllowMethods, ","),
		AllowHeaders: strings.Join(p.AllowHeaders, ","),
	}
	wildcardAll := false
	for _, o := range p.AllowOrigins {
		if o == "*" {
			wildcardAll = true
			break
		}
		c.AllowOriginStringMatch = append(c.AllowOriginStringMatch, originMatcher(o))
	}
	if wildcardAll {
		c.AllowOriginStringMatch = nil
	}
	if p.AllowCredentials {
		c.AllowCredentials = wrapperspb.Bool(true)
	}
	if p.MaxAge != nil {
		c.MaxAge = fmt.Sprintf("%d", int64(p.MaxAge.Duration/time.Second))
	}
	return c
}

// originMatcher 把单个 origin 转为 StringMatcher：无通配 = 精确匹配；
// 含 "*" = 分段 QuoteMeta 后以 ".*" 连接为 safe_regex。
func originMatcher(o string) *matcherv3.StringMatcher {
	if !strings.Contains(o, "*") {
		return &matcherv3.StringMatcher{
			MatchPattern: &matcherv3.StringMatcher_Exact{Exact: o},
		}
	}
	var b strings.Builder
	for i, seg := range strings.Split(o, "*") {
		if i > 0 {
			b.WriteString(".*")
		}
		b.WriteString(regexp.QuoteMeta(seg))
	}
	return &matcherv3.StringMatcher{
		MatchPattern: &matcherv3.StringMatcher_SafeRegex{
			SafeRegex: &matcherv3.RegexMatcher{Regex: "^" + b.String() + "$"},
		},
	}
}

// buildLocalRateLimit 生成 per-rule 本地令牌桶限流配置。
// burst = 桶容量（默认 = requests，F2 已填默认值）；tokens_per_fill = requests；fill 周期 = unit。
//
// 已知偏离（记录于 T4 进展）：rateLimit.key（clientIP | header:<name>）M0 不生效——
// Envoy 本地限流的 descriptor entry 是静态键值，无法表达「按客户端 IP / 请求头值」动态分桶
// （动态键属全局限流 rate limit service 的能力），per-rule 退化为全局限流桶。
func buildLocalRateLimit(p *protocol.RateLimitPolicy) *localratelimitv3.LocalRateLimit {
	var unit time.Duration
	switch p.Unit {
	case protocol.RateLimitUnitSecond:
		unit = time.Second
	case protocol.RateLimitUnitMinute:
		unit = time.Minute
	case protocol.RateLimitUnitHour:
		unit = time.Hour
	}
	burst := p.Requests
	if p.Burst != nil {
		burst = *p.Burst
	}
	return &localratelimitv3.LocalRateLimit{
		StatPrefix: "local_ratelimit",
		TokenBucket: &typev3.TokenBucket{
			MaxTokens:     uint32(burst),
			TokensPerFill: wrapperspb.UInt32(uint32(p.Requests)),
			FillInterval:  durationpb.New(unit),
		},
	}
}

// ─── JWT：providers 聚合去重 + requirement_map per-route（编译层 §3 策略表）───

// jwtAssembly 聚合一个 Listener 上全部 rule 生效 jwt 策略的 providers 与 requirement_map 条目。
type jwtAssembly struct {
	providers    map[string]*jwtv3.JwtProvider    // provider name → provider
	requirements map[string]*jwtv3.JwtRequirement // requirement name → requirement
}

func newJwtAssembly() *jwtAssembly {
	return &jwtAssembly{
		providers:    map[string]*jwtv3.JwtProvider{},
		requirements: map[string]*jwtv3.JwtRequirement{},
	}
}

// jwtProviderKey 是 providers 聚合去重键（C4 决议）：
//
//	issuer + "|" + 规范化 jwks 来源（"uri:" + TrimSpace(uri) | "file:" + file）
//
// audiences 不参与去重：同一 provider 的不同受众经 requirement_map 的
// provider_and_audiences 表达。issuer/jwks 均相同的策略视为同一 provider。
func jwtProviderKey(j *protocol.JWTPolicy) string {
	src := ""
	if j.JWKS != nil {
		switch {
		case j.JWKS.URI != "":
			src = "uri:" + strings.TrimSpace(j.JWKS.URI)
		case j.JWKS.File != "":
			src = "file:" + j.JWKS.File
		}
	}
	return j.Issuer + "|" + src
}

// jwtProviderName 由去重键派生确定性 provider 名：jwt/<sha256(key) 前 8 位十六进制>。
// 内容寻址而非序号，保证跨 Listener 同名同 provider、且与收集顺序无关（编译层 §5）。
func jwtProviderName(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "jwt/" + hex.EncodeToString(sum[:4])
}

// jwtAllowMissingRequirement 是 jwt.optional=true 的 requirement 名（协议 §3.5 规则 3a：
// 就近覆盖实现局部关闭 → 不强制校验；token 存在时仍校验，映射 Envoy allow_missing）。
const jwtAllowMissingRequirement = "allow-missing"

// registerJWT 登记一条生效 jwt 策略，返回该 rule 的 requirement 名。
// optional=true → allow-missing（无需 provider/jwks）；否则要求配置 jwks。
func (ctx *buildContext) registerJWT(asm *jwtAssembly, j *protocol.JWTPolicy) (string, error) {
	if j.Optional {
		asm.requirements[jwtAllowMissingRequirement] = &jwtv3.JwtRequirement{
			RequiresType: &jwtv3.JwtRequirement_AllowMissing{AllowMissing: &emptypb.Empty{}},
		}
		return jwtAllowMissingRequirement, nil
	}
	if j.JWKS == nil {
		return "", fmt.Errorf("jwt policy requires jwks (uri | file) unless optional: true (issuer %q)", j.Issuer)
	}
	key := jwtProviderKey(j)
	pname := jwtProviderName(key)
	if _, ok := asm.providers[pname]; !ok {
		p, err := ctx.buildJWTProvider(j)
		if err != nil {
			return "", err
		}
		asm.providers[pname] = p
	}
	if len(j.Audiences) > 0 {
		reqName := pname + ":" + strings.Join(j.Audiences, ",")
		asm.requirements[reqName] = &jwtv3.JwtRequirement{
			RequiresType: &jwtv3.JwtRequirement_ProviderAndAudiences{
				ProviderAndAudiences: &jwtv3.ProviderWithAudiences{
					ProviderName: pname,
					Audiences:    j.Audiences,
				},
			},
		}
		return reqName, nil
	}
	asm.requirements[pname] = &jwtv3.JwtRequirement{
		RequiresType: &jwtv3.JwtRequirement_ProviderName{ProviderName: pname},
	}
	return pname, nil
}

// buildJWTProvider 生成单个 JwtProvider；远程 jwks 同时登记自动生成的抓取集群。
func (ctx *buildContext) buildJWTProvider(j *protocol.JWTPolicy) (*jwtv3.JwtProvider, error) {
	p := &jwtv3.JwtProvider{
		Issuer:               j.Issuer,
		Audiences:            j.Audiences,
		ForwardPayloadHeader: j.ForwardPayloadHeader,
	}
	switch {
	case j.JWKS.URI != "":
		cluster, err := ctx.jwksFetchCluster(j.JWKS.URI)
		if err != nil {
			return nil, err
		}
		p.JwksSourceSpecifier = &jwtv3.JwtProvider_RemoteJwks{
			RemoteJwks: &jwtv3.RemoteJwks{
				HttpUri: &corev3.HttpUri{
					Uri:              j.JWKS.URI,
					HttpUpstreamType: &corev3.HttpUri_Cluster{Cluster: cluster},
					Timeout:          durationpb.New(protocol.DefaultConnectTimeout), // PGV 必填
				},
			},
		}
	case j.JWKS.File != "":
		p.JwksSourceSpecifier = &jwtv3.JwtProvider_LocalJwks{
			LocalJwks: &corev3.DataSource{
				Specifier: &corev3.DataSource_Filename{Filename: j.JWKS.File},
			},
		}
	}
	return p, nil
}

// jwksFetchCluster 为远程 JWKS URI 生成（或复用）抓取集群 jwt-jwks/<host:port>：
// STRICT_DNS + https 时上游 TLS（SNI = host），connectTimeout 取协议默认 5s。
func (ctx *buildContext) jwksFetchCluster(rawURI string) (string, error) {
	u, err := url.Parse(rawURI)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("invalid jwks uri %q: %v", rawURI, err)
	}
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	var portNum int
	if _, err := fmt.Sscanf(port, "%d", &portNum); err != nil || portNum < 1 || portNum > 65535 {
		return "", fmt.Errorf("invalid jwks uri %q: bad port %q", rawURI, port)
	}
	name := "jwt-jwks/" + u.Hostname() + ":" + port
	if _, ok := ctx.jwks[name]; ok {
		return name, nil
	}
	cl := &clusterv3.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_STRICT_DNS},
		ConnectTimeout:       durationpb.New(protocol.DefaultConnectTimeout),
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: name,
			Endpoints: []*endpointv3.LocalityLbEndpoints{{
				LbEndpoints: []*endpointv3.LbEndpoint{lbEndpoint(u.Hostname(), int32(portNum), nil)},
			}},
		},
	}
	if u.Scheme == "https" {
		ts, err := upstreamTLSSocket(&protocol.UpstreamTLS{Enabled: true, SNI: u.Hostname()}, nil)
		if err != nil {
			return "", err
		}
		cl.TransportSocket = ts
	}
	ctx.jwks[name] = cl
	return name, nil
}

// filterConfig 生成 jwt_authn filter 配置：providers + requirement_map。
// providers 为空且无 requirement 时整条 filter pass-through。
func (a *jwtAssembly) filterConfig() *jwtv3.JwtAuthentication {
	return &jwtv3.JwtAuthentication{
		Providers:      a.providers,
		RequirementMap: a.requirements,
	}
}

// sortedKeys 返回 map 的排序键（确定性输出）。
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
