package compile

import (
	"strings"
	"testing"
	"time"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	corsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3"
	jwtv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/jwt_authn/v3"
	localratelimitv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/local_ratelimit/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// policyCS 构造 单 HTTP Listener + 单 Route（一条 catch-all rule）+ 上游 的 ConfigSet，
// Gateway/Listener/Route/rule 四级 policies 可分别指定。
func policyCS(gw, lis, rt, rl []protocol.PolicyAttachment, policies ...*protocol.Policy) *protocol.ConfigSet {
	g := &protocol.Gateway{
		APIVersion: protocol.APIVersionV1Alpha1, Kind: protocol.KindGateway,
		Metadata: protocol.ObjectMeta{Name: "default"},
		Spec:     protocol.GatewaySpec{Policies: gw},
	}
	l := newListener("web", 80, protocol.ProtocolHTTP)
	l.Spec.Policies = lis
	r := newHTTPRoute("r1", []string{"web"}, nil, "app")
	r.Spec.Policies = rt
	r.Spec.Rules[0].Policies = rl
	return &protocol.ConfigSet{
		Gateway:   g,
		Listeners: []*protocol.Listener{l},
		Routes:    []*protocol.Route{r},
		Upstreams: []*protocol.Upstream{newUpstream("app")},
		Policies:  policies,
	}
}

// buildPolicyRoute 构建 policyCS 并返回该 rule 的 routev3.Route 与 HCM。
func buildPolicyRoute(t *testing.T, cs *protocol.ConfigSet) (*routev3.Route, *hcmv3.HttpConnectionManager) {
	t.Helper()
	res, errs := buildCS(t, cs)
	assertNoErrs(t, errs)
	route := findRouteConfig(t, res, "rc/web").GetVirtualHosts()[0].GetRoutes()[0]
	hcm := hcmOf(t, findListener(t, res, "lis/web").GetFilterChains()[0])
	return route, hcm
}

// jwtFilterOf 解出 HCM 的 jwt_authn filter 配置。
func jwtFilterOf(t *testing.T, hcm *hcmv3.HttpConnectionManager) *jwtv3.JwtAuthentication {
	t.Helper()
	for _, f := range hcm.GetHttpFilters() {
		if f.GetName() == jwtAuthnFilterName {
			j := &jwtv3.JwtAuthentication{}
			mustUnmarshal(t, f.GetTypedConfig(), j)
			return j
		}
	}
	t.Fatal("jwt_authn filter not found")
	return nil
}

// TestHTTPFilterChain 覆盖 HCM filter 链：固定顺序、常驻、默认 pass-through。
func TestHTTPFilterChain(t *testing.T) {
	cs := policyCS(nil, nil, nil, nil)
	_, hcm := buildPolicyRoute(t, cs)
	names := httpFilterNames(t, hcm)
	want := []string{corsFilterName, jwtAuthnFilterName, localRateLimitFilterName, routerFilterName}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("filter chain = %v, want %v（cors → jwt_authn → local_ratelimit → router）", names, want)
	}
	// 默认 pass-through：jwt 无 provider；local_ratelimit 无 token bucket；cors 空配置。
	jwt := jwtFilterOf(t, hcm)
	if len(jwt.GetProviders()) != 0 || len(jwt.GetRequirementMap()) != 0 {
		t.Fatalf("jwt filter = %v, want empty（pass-through）", jwt)
	}
	for _, f := range hcm.GetHttpFilters() {
		switch f.GetName() {
		case localRateLimitFilterName:
			lrl := &localratelimitv3.LocalRateLimit{}
			mustUnmarshal(t, f.GetTypedConfig(), lrl)
			if lrl.GetTokenBucket() != nil {
				t.Fatalf("filter-level token bucket = %v, want nil（pass-through）", lrl.GetTokenBucket())
			}
		case corsFilterName:
			c := &corsv3.Cors{}
			mustUnmarshal(t, f.GetTypedConfig(), c)
		}
	}
}

// TestHeaderModifierMapping 覆盖 headerModifier 落 route 层字段（不占 filter）。
func TestHeaderModifierMapping(t *testing.T) {
	hm := protocol.PolicySpec{HeaderModifier: &protocol.HeaderModifierPolicy{
		Request: &protocol.HeaderOps{
			Set:    map[string]string{"x-b": "2", "x-a": "1"}, // 乱序 map → 输出按 key 排序
			Add:    map[string]string{"x-add": "v"},
			Remove: []string{"x-old"},
		},
		Response: &protocol.HeaderOps{Remove: []string{"server"}},
	}}
	cs := policyCS(nil, nil, nil, []protocol.PolicyAttachment{{Inline: &hm}})
	route, _ := buildPolicyRoute(t, cs)

	adds := route.GetRequestHeadersToAdd()
	if len(adds) != 3 {
		t.Fatalf("request_headers_to_add = %v", adds)
	}
	// set 按 key 排序（x-a, x-b）→ OVERWRITE；add → APPEND。
	if adds[0].GetHeader().GetKey() != "x-a" || adds[1].GetHeader().GetKey() != "x-b" {
		t.Fatalf("set order = %v, want sorted by key", adds)
	}
	if adds[0].GetAppendAction() != corev3.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD {
		t.Fatalf("set append action = %v", adds[0].GetAppendAction())
	}
	if adds[2].GetHeader().GetKey() != "x-add" ||
		adds[2].GetAppendAction() != corev3.HeaderValueOption_APPEND_IF_EXISTS_OR_ADD {
		t.Fatalf("add entry = %v", adds[2])
	}
	if strings.Join(route.GetRequestHeadersToRemove(), ",") != "x-old" {
		t.Fatalf("request_headers_to_remove = %v", route.GetRequestHeadersToRemove())
	}
	if strings.Join(route.GetResponseHeadersToRemove(), ",") != "server" {
		t.Fatalf("response_headers_to_remove = %v", route.GetResponseHeadersToRemove())
	}
}

// TestCORSPerRule 覆盖 cors per-rule typed_per_filter_config 与通配 origin 转换。
func TestCORSPerRule(t *testing.T) {
	maxAge := protocol.Duration{Duration: 24 * time.Hour}
	spec := protocol.PolicySpec{CORS: &protocol.CORSPolicy{
		AllowOrigins:     []string{"https://*.example.com", "https://a.com"},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"authorization"},
		AllowCredentials: true,
		MaxAge:           &maxAge,
	}}
	cs := policyCS(nil, nil, nil, []protocol.PolicyAttachment{{Inline: &spec}})
	route, _ := buildPolicyRoute(t, cs)

	anyCfg := route.GetTypedPerFilterConfig()[corsFilterName]
	if anyCfg == nil {
		t.Fatalf("typed_per_filter_config = %v, want cors entry", route.GetTypedPerFilterConfig())
	}
	c := &corsv3.CorsPolicy{}
	mustUnmarshal(t, anyCfg, c)
	matchers := c.GetAllowOriginStringMatch()
	if len(matchers) != 2 {
		t.Fatalf("origin matchers = %v", matchers)
	}
	if matchers[0].GetSafeRegex().GetRegex() != `^https://.*\.example\.com$` {
		t.Fatalf("wildcard origin regex = %q", matchers[0].GetSafeRegex().GetRegex())
	}
	if matchers[1].GetExact() != "https://a.com" {
		t.Fatalf("exact origin = %q", matchers[1].GetExact())
	}
	if c.GetAllowMethods() != "GET,POST" || c.GetAllowHeaders() != "authorization" ||
		!c.GetAllowCredentials().GetValue() || c.GetMaxAge() != "86400" {
		t.Fatalf("cors policy = %v", c)
	}
}

// TestCORSWildcardAll 覆盖 allowOrigins ["*"] → 空 match 列表（放行全部源）。
func TestCORSWildcardAll(t *testing.T) {
	spec := protocol.PolicySpec{CORS: &protocol.CORSPolicy{AllowOrigins: []string{"*"}}}
	cs := policyCS(nil, nil, nil, []protocol.PolicyAttachment{{Inline: &spec}})
	route, _ := buildPolicyRoute(t, cs)
	c := &corsv3.CorsPolicy{}
	mustUnmarshal(t, route.GetTypedPerFilterConfig()[corsFilterName], c)
	if len(c.GetAllowOriginStringMatch()) != 0 {
		t.Fatalf("matchers = %v, want empty（\"*\" = 放行全部源）", c.GetAllowOriginStringMatch())
	}
}

// TestRateLimitPerRule 覆盖 rateLimit per-rule 令牌桶。
func TestRateLimitPerRule(t *testing.T) {
	spec := protocol.PolicySpec{RateLimit: &protocol.RateLimitPolicy{
		Requests: 10, Unit: protocol.RateLimitUnitMinute, Key: "clientIP",
	}}
	cs := policyCS(nil, nil, nil, []protocol.PolicyAttachment{{Inline: &spec}})
	route, _ := buildPolicyRoute(t, cs)

	anyCfg := route.GetTypedPerFilterConfig()[localRateLimitFilterName]
	if anyCfg == nil {
		t.Fatalf("typed_per_filter_config = %v, want local_ratelimit entry", route.GetTypedPerFilterConfig())
	}
	lrl := &localratelimitv3.LocalRateLimit{}
	mustUnmarshal(t, anyCfg, lrl)
	tb := lrl.GetTokenBucket()
	if tb.GetMaxTokens() != 10 || tb.GetTokensPerFill().GetValue() != 10 || tb.GetFillInterval().GetSeconds() != 60 {
		t.Fatalf("token bucket = %v, want 10/minute（burst 默认 = requests）", tb)
	}
}

// TestJWTAggregation 覆盖 jwt providers 聚合去重（C4）+ requirement_map per-route。
func TestJWTAggregation(t *testing.T) {
	jwtMain := newPolicy("jwt-main", protocol.PolicySpec{JWT: &protocol.JWTPolicy{
		Issuer:               "https://auth.example.com",
		Audiences:            []string{"api.example.com"},
		JWKS:                 &protocol.JWKS{URI: "https://auth.example.com/.well-known/jwks.json"},
		ForwardPayloadHeader: "x-jwt-payload",
	}})
	// 同 issuer + 同 jwks、不同 audiences 的内联策略 → 同一 provider（C4 去重）。
	jwtAltAud := protocol.PolicySpec{JWT: &protocol.JWTPolicy{
		Issuer:    "https://auth.example.com",
		Audiences: []string{"other.example.com"},
		JWKS:      &protocol.JWKS{URI: "https://auth.example.com/.well-known/jwks.json"},
	}}
	cs := policyCS(
		nil, nil,
		[]protocol.PolicyAttachment{{Ref: "jwt-main"}},
		[]protocol.PolicyAttachment{{Inline: &jwtAltAud}},
		jwtMain,
	)
	route, hcm := buildPolicyRoute(t, cs)

	jwt := jwtFilterOf(t, hcm)
	if len(jwt.GetProviders()) != 1 {
		t.Fatalf("providers = %d, want 1（issuer+jwks 去重，audiences 不参与）", len(jwt.GetProviders()))
	}
	var pname string
	for n, p := range jwt.GetProviders() {
		pname = n
		if p.GetIssuer() != "https://auth.example.com" {
			t.Fatalf("provider = %v", p)
		}
		if p.GetRemoteJwks().GetHttpUri().GetUri() != "https://auth.example.com/.well-known/jwks.json" {
			t.Fatalf("remote jwks = %v", p.GetRemoteJwks())
		}
	}
	// 该 rule（内联覆盖 route 级）requirement 指向 provider_and_audiences(other)。
	reqName := pname + ":other.example.com"
	perRoute := &jwtv3.PerRouteConfig{}
	mustUnmarshal(t, route.GetTypedPerFilterConfig()[jwtAuthnFilterName], perRoute)
	if perRoute.GetRequirementName() != reqName {
		t.Fatalf("requirement name = %q, want %q", perRoute.GetRequirementName(), reqName)
	}
	req := jwt.GetRequirementMap()[reqName]
	if req.GetProviderAndAudiences().GetProviderName() != pname ||
		strings.Join(req.GetProviderAndAudiences().GetAudiences(), ",") != "other.example.com" {
		t.Fatalf("requirement = %v", req)
	}
	// 远程 JWKS 自动生成抓取集群。
	res, _ := buildCS(t, cs)
	jwksCluster := findCluster(t, res, "jwt-jwks/auth.example.com:443")
	if jwksCluster.GetType() != clusterv3.Cluster_STRICT_DNS {
		t.Fatalf("jwks cluster type = %v", jwksCluster.GetType())
	}
	if jwksCluster.GetTransportSocket() == nil {
		t.Fatal("https jwks cluster must have TLS transport socket")
	}
	if jwt.GetProviders()[pname].GetRemoteJwks().GetHttpUri().GetCluster() != "jwt-jwks/auth.example.com:443" {
		t.Fatalf("remote jwks cluster ref = %q", jwt.GetProviders()[pname].GetRemoteJwks().GetHttpUri().GetCluster())
	}
}

// TestJWTOptional 覆盖 jwt.optional=true → allow-missing（局部关闭强制校验）。
func TestJWTOptional(t *testing.T) {
	jwtMain := newPolicy("jwt-main", protocol.PolicySpec{JWT: &protocol.JWTPolicy{
		Issuer: "https://auth.example.com",
		JWKS:   &protocol.JWKS{File: "/etc/esgw/jwks.json"},
	}})
	jwtOff := newPolicy("jwt-off", protocol.PolicySpec{JWT: &protocol.JWTPolicy{Optional: true}})
	cs := policyCS(
		nil, nil,
		[]protocol.PolicyAttachment{{Ref: "jwt-main"}},
		[]protocol.PolicyAttachment{{Ref: "jwt-off"}},
		jwtMain, jwtOff,
	)
	route, hcm := buildPolicyRoute(t, cs)

	perRoute := &jwtv3.PerRouteConfig{}
	mustUnmarshal(t, route.GetTypedPerFilterConfig()[jwtAuthnFilterName], perRoute)
	if perRoute.GetRequirementName() != jwtAllowMissingRequirement {
		t.Fatalf("requirement name = %q, want allow-missing", perRoute.GetRequirementName())
	}
	jwt := jwtFilterOf(t, hcm)
	if jwt.GetRequirementMap()[jwtAllowMissingRequirement].GetAllowMissing() == nil {
		t.Fatalf("requirement_map = %v, want allow-missing entry", jwt.GetRequirementMap())
	}
	// 就近覆盖后该 rule 无强制 provider requirement；file jwks 不产生抓取集群。
	res, _ := buildCS(t, cs)
	for _, c := range res.clusters {
		if strings.HasPrefix(c.GetName(), "jwt-jwks/") {
			t.Fatalf("file jwks must not generate fetch cluster, got %q", c.GetName())
		}
	}
}

// TestJWTMissingJWKS 非 optional 且未配 jwks → build 错误。
func TestJWTMissingJWKS(t *testing.T) {
	spec := protocol.PolicySpec{JWT: &protocol.JWTPolicy{Issuer: "https://auth.example.com"}}
	cs := policyCS(nil, nil, nil, []protocol.PolicyAttachment{{Inline: &spec}})
	_, errs := buildCS(t, cs)
	if len(errs) != 1 || errs[0].Stage != StageBuild || !strings.Contains(errs[0].Message, "requires jwks") {
		t.Fatalf("want jwks-required build error, got:\n%s", formatErrs(errs))
	}
}

// TestUnsupportedPolicyType M0 未实现策略类型显式报 build 错误（不静默丢弃）。
func TestUnsupportedPolicyType(t *testing.T) {
	spec := protocol.PolicySpec{ExtAuth: &protocol.ExtAuthPolicy{
		GRPC: &protocol.ExtAuthGRPC{Address: "127.0.0.1:9000"},
	}}
	cs := policyCS(nil, nil, nil, []protocol.PolicyAttachment{{Inline: &spec}})
	_, errs := buildCS(t, cs)
	if len(errs) != 1 || errs[0].Stage != StageBuild || !strings.Contains(errs[0].Message, "extAuth is not implemented in M0") {
		t.Fatalf("want not-implemented build error, got:\n%s", formatErrs(errs))
	}
}

// TestPolicyFourLevelEffective 覆盖四级挂接端到端生效：Gateway 级 cors 流进每个 rule。
func TestPolicyFourLevelEffective(t *testing.T) {
	corsGW := newPolicy("cors-gw", protocol.PolicySpec{CORS: &protocol.CORSPolicy{
		AllowOrigins: []string{"https://a.com"},
	}})
	cs := policyCS([]protocol.PolicyAttachment{{Ref: "cors-gw"}}, nil, nil, nil, corsGW)
	route, _ := buildPolicyRoute(t, cs)
	if route.GetTypedPerFilterConfig()[corsFilterName] == nil {
		t.Fatal("gateway-level cors must reach every rule")
	}
}
