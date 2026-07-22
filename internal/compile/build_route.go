package compile

import (
	"fmt"
	"sort"
	"strings"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	matcherv3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// buildVirtualHost 把一个 Route 对象翻译为 VirtualHost vh/<route>
// （编译层 §3 Builder 表）：domains = hostnames（精确 > 通配 > 兜底排序），
// rules 保序翻译为 routes（顺序即优先级，协议 P4）。
func (ctx *buildContext) buildVirtualHost(l *protocol.Listener, r *protocol.Route, jwtAsm *jwtAssembly, extAuthAsm *extAuthAssembly) (*routev3.VirtualHost, []CompileError) {
	var errs []CompileError
	vh := &routev3.VirtualHost{
		Name:    virtualHostName(r.Metadata.Name),
		Domains: routeDomains(r),
	}
	for i := range r.Spec.Rules {
		rr, rerrs := ctx.buildRule(l, r, i, jwtAsm, extAuthAsm)
		errs = append(errs, rerrs...)
		if rr != nil {
			vh.Routes = append(vh.Routes, rr)
		}
	}
	return vh, errs
}

// routeDomains 返回 Route 的 vhost domains：hostnames 省略 = 兜底 "*"（协议 §3.3）；
// 按 精确 > 通配（*. 前缀）> 兜底 分组、组内字典序（编译层 §5 排序规约）。
func routeDomains(r *protocol.Route) []string {
	hostnames := r.Spec.Hostnames
	if len(hostnames) == 0 {
		return []string{"*"}
	}
	out := append([]string(nil), hostnames...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := domainRank(out[i]), domainRank(out[j])
		if ri != rj {
			return ri < rj
		}
		return out[i] < out[j]
	})
	return out
}

// domainRank 域名匹配精度分组：0 精确 > 1 通配 > 2 兜底（协议 §3.3 要点 1）。
func domainRank(d string) int {
	switch {
	case d == "*":
		return 2
	case strings.HasPrefix(d, "*."):
		return 1
	default:
		return 0
	}
}

// buildRule 把一条 rule 翻译为 routev3.Route：match / rewrite / 动作三选一 /
// timeout / retry_policy，外加四级归一化后的策略装配（headerModifier 落 route 字段，
// cors/rateLimit/jwt 落 typed_per_filter_config）。
func (ctx *buildContext) buildRule(l *protocol.Listener, r *protocol.Route, idx int, jwtAsm *jwtAssembly, extAuthAsm *extAuthAssembly) (*routev3.Route, []CompileError) {
	var errs []CompileError
	rule := &r.Spec.Rules[idx]
	rulePath := fmt.Sprintf("spec.rules[%d]", idx)
	route := &routev3.Route{Match: buildRouteMatch(&rule.Match)}

	switch {
	case len(rule.Backends) > 0:
		route.Action = &routev3.Route_Route{Route: ctx.buildRouteAction(rule)}
	case rule.Redirect != nil:
		red, err := buildRedirectAction(rule.Redirect)
		if err != nil {
			errs = append(errs, buildError(r.Origin, protocol.KindRoute, r.Metadata.Name,
				rulePath+".redirect.code", "%v", err))
		} else {
			route.Action = &routev3.Route_Redirect{Redirect: red}
		}
	case rule.DirectResponse != nil:
		route.Action = &routev3.Route_DirectResponse{
			DirectResponse: buildDirectResponseAction(rule.DirectResponse),
		}
	}

	// 策略归一化（Gateway → Listener → Route → rule，就近覆盖）后装配。
	eff := normalizePolicies(ctx.lk,
		ctx.cs.Gateway.Spec.Policies, l.Spec.Policies, r.Spec.Policies, rule.Policies)
	if err := ctx.registerExtAuth(extAuthAsm, eff.extAuth); err != nil {
		errs = append(errs, buildError(r.Origin, protocol.KindRoute, r.Metadata.Name,
			rulePath+".policies", "%v", err))
	}
	for _, typ := range eff.unsupported {
		errs = append(errs, buildError(r.Origin, protocol.KindRoute, r.Metadata.Name,
			rulePath+".policies", "policy type %s is not implemented in M0 (P1)", typ))
	}
	applyHeaderModifier(route, eff.headerModifier)
	tpc, terrs := ctx.typedPerFilterConfig(eff, jwtAsm, r, idx)
	errs = append(errs, terrs...)
	route.TypedPerFilterConfig = tpc
	return route, errs
}

// buildRouteMatch 翻译 match：path 三选一、methods（:method 头正则 OR）、headers、queryParams。
func buildRouteMatch(m *protocol.RuleMatch) *routev3.RouteMatch {
	out := &routev3.RouteMatch{}
	if p := m.Path; p != nil {
		switch {
		case p.Exact != "":
			out.PathSpecifier = &routev3.RouteMatch_Path{Path: p.Exact}
		case p.Prefix != "":
			out.PathSpecifier = &routev3.RouteMatch_Prefix{Prefix: p.Prefix}
		case p.Regex != "":
			out.PathSpecifier = &routev3.RouteMatch_SafeRegex{
				SafeRegex: &matcherv3.RegexMatcher{Regex: p.Regex},
			}
		}
	}
	if len(m.Methods) > 0 {
		out.Headers = append(out.Headers, &routev3.HeaderMatcher{
			Name: ":method",
			HeaderMatchSpecifier: &routev3.HeaderMatcher_StringMatch{
				StringMatch: &matcherv3.StringMatcher{
					MatchPattern: &matcherv3.StringMatcher_SafeRegex{
						SafeRegex: &matcherv3.RegexMatcher{Regex: "^(?:" + strings.Join(m.Methods, "|") + ")$"},
					},
				},
			},
		})
	}
	for i := range m.Headers {
		out.Headers = append(out.Headers, headerMatcher(&m.Headers[i]))
	}
	for i := range m.QueryParams {
		out.QueryParameters = append(out.QueryParameters, queryParamMatcher(&m.QueryParams[i]))
	}
	return out
}

// headerMatcher 翻译 header 匹配（exact | regex | present 三选一）。
func headerMatcher(kv *protocol.KeyValueMatch) *routev3.HeaderMatcher {
	h := &routev3.HeaderMatcher{Name: kv.Name}
	switch {
	case kv.Present != nil:
		h.HeaderMatchSpecifier = &routev3.HeaderMatcher_PresentMatch{PresentMatch: *kv.Present}
	case kv.Regex != "":
		h.HeaderMatchSpecifier = &routev3.HeaderMatcher_StringMatch{
			StringMatch: &matcherv3.StringMatcher{
				MatchPattern: &matcherv3.StringMatcher_SafeRegex{
					SafeRegex: &matcherv3.RegexMatcher{Regex: kv.Regex},
				},
			},
		}
	default:
		h.HeaderMatchSpecifier = &routev3.HeaderMatcher_StringMatch{
			StringMatch: &matcherv3.StringMatcher{
				MatchPattern: &matcherv3.StringMatcher_Exact{Exact: kv.Exact},
			},
		}
	}
	return h
}

// queryParamMatcher 翻译 queryParam 匹配（同 headers 形态）。
func queryParamMatcher(kv *protocol.KeyValueMatch) *routev3.QueryParameterMatcher {
	q := &routev3.QueryParameterMatcher{Name: kv.Name}
	switch {
	case kv.Present != nil:
		q.QueryParameterMatchSpecifier = &routev3.QueryParameterMatcher_PresentMatch{PresentMatch: *kv.Present}
	case kv.Regex != "":
		q.QueryParameterMatchSpecifier = &routev3.QueryParameterMatcher_StringMatch{
			StringMatch: &matcherv3.StringMatcher{
				MatchPattern: &matcherv3.StringMatcher_SafeRegex{
					SafeRegex: &matcherv3.RegexMatcher{Regex: kv.Regex},
				},
			},
		}
	default:
		q.QueryParameterMatchSpecifier = &routev3.QueryParameterMatcher_StringMatch{
			StringMatch: &matcherv3.StringMatcher{
				MatchPattern: &matcherv3.StringMatcher_Exact{Exact: kv.Exact},
			},
		}
	}
	return q
}

// buildRouteAction 翻译 backends 形态动作：单后端无权重 → cluster；
// 多后端或显式权重 → weighted_clusters（未标权重 = 1）。附 rewrite/timeout/retry/hashOn 联动。
func (ctx *buildContext) buildRouteAction(rule *protocol.Rule) *routev3.RouteAction {
	action := &routev3.RouteAction{}
	if len(rule.Backends) == 1 && rule.Backends[0].Weight == nil {
		action.ClusterSpecifier = &routev3.RouteAction_Cluster{
			Cluster: clusterResourceName(rule.Backends[0].Upstream),
		}
	} else {
		wc := &routev3.WeightedCluster{}
		for _, b := range rule.Backends {
			w := int32(1)
			if b.Weight != nil {
				w = *b.Weight
			}
			wc.Clusters = append(wc.Clusters, &routev3.WeightedCluster_ClusterWeight{
				Name:   clusterResourceName(b.Upstream),
				Weight: wrapperspb.UInt32(uint32(w)),
			})
		}
		// total_weight 已废弃：Envoy 按各 cluster 权重自动求和。
		action.ClusterSpecifier = &routev3.RouteAction_WeightedClusters{WeightedClusters: wc}
	}

	if rw := rule.Rewrite; rw != nil {
		switch {
		case rw.PathPrefix != "":
			action.PrefixRewrite = rw.PathPrefix
		case rw.Path != "":
			// 整路径替换：safe_regex 全路径匹配后整体替换（正则作用于 path，不含 query）。
			action.RegexRewrite = &matcherv3.RegexMatchAndSubstitute{
				Pattern:      &matcherv3.RegexMatcher{Regex: "^/.*$"},
				Substitution: rw.Path,
			}
		case rw.Regex != nil:
			action.RegexRewrite = &matcherv3.RegexMatchAndSubstitute{
				Pattern:      &matcherv3.RegexMatcher{Regex: rw.Regex.Pattern},
				Substitution: rw.Regex.Substitution,
			}
		}
		if rw.Host != "" {
			action.HostRewriteSpecifier = &routev3.RouteAction_HostRewriteLiteral{
				HostRewriteLiteral: rw.Host,
			}
		}
	}

	// timeout：F2 已填默认值（15s）；0s = 不限（Envoy 语义：0 关闭路由级超时）。
	action.Timeout = durationpb.New(rule.Timeout.Duration)

	if rt := rule.Retry; rt != nil {
		rp := &routev3.RetryPolicy{NumRetries: wrapperspb.UInt32(uint32(rt.Attempts))}
		on := make([]string, 0, len(rt.On))
		for _, o := range rt.On {
			on = append(on, retryOnString(o))
		}
		rp.RetryOn = strings.Join(on, ",")
		if rt.PerTryTimeout != nil {
			rp.PerTryTimeout = durationpb.New(rt.PerTryTimeout.Duration)
		}
		action.RetryPolicy = rp
	}

	// hashOn 联动（编译层 §3 Builder 表）：ringHash/maglev 上游的哈希键落 route hash_policy。
	seen := map[string]bool{}
	for _, b := range rule.Backends {
		u := ctx.lk.upstreams[b.Upstream]
		if u == nil || u.Spec.LoadBalancer == nil {
			continue
		}
		switch u.Spec.LoadBalancer.Policy {
		case protocol.LBPolicyRingHash, protocol.LBPolicyMaglev:
		default:
			continue
		}
		for _, h := range u.Spec.LoadBalancer.HashOn {
			if seen[h.Header] {
				continue
			}
			seen[h.Header] = true
			action.HashPolicy = append(action.HashPolicy, &routev3.RouteAction_HashPolicy{
				PolicySpecifier: &routev3.RouteAction_HashPolicy_Header_{
					Header: &routev3.RouteAction_HashPolicy_Header{HeaderName: h.Header},
				},
			})
		}
	}
	return action
}

// retryOnString 映射 retry.on 枚举 → Envoy retry_on 串（协议 §3.3 要点 3，枚举值同名直译）。
func retryOnString(o protocol.RetryOn) string {
	return string(o)
}

// buildRedirectAction 翻译 redirect 形态：scheme 改写 + 状态码映射。
func buildRedirectAction(rd *protocol.Redirect) (*routev3.RedirectAction, error) {
	out := &routev3.RedirectAction{}
	if rd.Scheme != "" {
		out.SchemeRewriteSpecifier = &routev3.RedirectAction_SchemeRedirect{SchemeRedirect: rd.Scheme}
	}
	switch rd.Code {
	case 0, 301: // code 省略默认 301（协议 §3.3 示例；记录于 T4 进展）
		out.ResponseCode = routev3.RedirectAction_MOVED_PERMANENTLY
	case 302:
		out.ResponseCode = routev3.RedirectAction_FOUND
	case 303:
		out.ResponseCode = routev3.RedirectAction_SEE_OTHER
	case 307:
		out.ResponseCode = routev3.RedirectAction_TEMPORARY_REDIRECT
	case 308:
		out.ResponseCode = routev3.RedirectAction_PERMANENT_REDIRECT
	default:
		return nil, fmt.Errorf("unsupported redirect code %d (want 301 | 302 | 303 | 307 | 308)", rd.Code)
	}
	return out, nil
}

// buildDirectResponseAction 翻译 directResponse 形态。
func buildDirectResponseAction(dr *protocol.DirectResponse) *routev3.DirectResponseAction {
	out := &routev3.DirectResponseAction{Status: uint32(dr.Status)}
	if dr.Body != "" {
		out.Body = &corev3.DataSource{
			Specifier: &corev3.DataSource_InlineString{InlineString: dr.Body},
		}
	}
	return out
}
