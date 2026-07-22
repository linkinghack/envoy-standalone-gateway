package compile

import (
	"strings"
	"testing"
	"time"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	rbacconfigv3 "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	corsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3"
	extauthzv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	jwtv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/jwt_authn/v3"
	localratelimitv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/local_ratelimit/v3"
	rbacv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
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

func extAuthFilterOf(t *testing.T, hcm *hcmv3.HttpConnectionManager) *extauthzv3.ExtAuthz {
	t.Helper()
	for _, f := range hcm.GetHttpFilters() {
		if f.GetName() == extAuthzFilterName {
			cfg := &extauthzv3.ExtAuthz{}
			mustUnmarshal(t, f.GetTypedConfig(), cfg)
			return cfg
		}
	}
	t.Fatal("ext_authz filter not found")
	return nil
}

// TestHTTPFilterChain 覆盖 HCM filter 链：固定顺序、常驻、默认 pass-through。
func TestHTTPFilterChain(t *testing.T) {
	cs := policyCS(nil, nil, nil, nil)
	_, hcm := buildPolicyRoute(t, cs)
	names := httpFilterNames(t, hcm)
	want := []string{rbacFilterName, corsFilterName, jwtAuthnFilterName, localRateLimitFilterName, routerFilterName}
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

func TestIPAccessPerRoute(t *testing.T) {
	spec := protocol.PolicySpec{IPAccess: &protocol.IPAccessPolicy{
		Allow: []string{"10.1.2.3/8", "10.0.0.0/8"},
		Deny:  []string{"10.9.0.0/16"},
	}}
	cs := policyCS(nil, nil, []protocol.PolicyAttachment{{Inline: &spec}}, nil)
	route, hcm := buildPolicyRoute(t, cs)
	if httpFilterNames(t, hcm)[0] != rbacFilterName {
		t.Fatalf("RBAC must be first HTTP filter: %v", httpFilterNames(t, hcm))
	}
	perRoute := &rbacv3.RBACPerRoute{}
	mustUnmarshal(t, route.GetTypedPerFilterConfig()[rbacFilterName], perRoute)
	rules := perRoute.GetRbac().GetRules()
	if rules.GetAction() != rbacconfigv3.RBAC_ALLOW {
		t.Fatalf("RBAC action = %v, want ALLOW", rules.GetAction())
	}
	principal := rules.GetPolicies()["ip-access"].GetPrincipals()[0]
	ids := principal.GetAndIds().GetIds()
	if len(ids) != 2 || ids[0].GetRemoteIp().GetAddressPrefix() != "10.0.0.0" ||
		ids[0].GetRemoteIp().GetPrefixLen().GetValue() != 8 ||
		ids[1].GetNotId().GetRemoteIp().GetAddressPrefix() != "10.9.0.0" {
		t.Fatalf("IPAccess principal = %+v", principal)
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

// TestExtAuthGRPC 覆盖 gRPC filter、HTTP/2 auth cluster、fail-open 与 route disable。
func TestExtAuthGRPC(t *testing.T) {
	main := protocol.PolicySpec{ExtAuth: &protocol.ExtAuthPolicy{
		GRPC: &protocol.ExtAuthGRPC{Address: "127.0.0.1:9000"}, FailOpen: true,
	}}
	off := protocol.PolicySpec{ExtAuth: &protocol.ExtAuthPolicy{Disabled: true}}
	cs := policyCS(nil, nil, nil, []protocol.PolicyAttachment{{Inline: &main}})
	base := cs.Routes[0].Spec.Rules[0]
	base.Policies = []protocol.PolicyAttachment{{Inline: &off}}
	cs.Routes[0].Spec.Rules = append(cs.Routes[0].Spec.Rules, base)
	base.Policies = nil
	cs.Routes[0].Spec.Rules = append(cs.Routes[0].Spec.Rules, base)

	res, errs := buildCS(t, cs)
	assertNoErrs(t, errs)
	hcm := hcmOf(t, findListener(t, res, "lis/web").GetFilterChains()[0])
	wantOrder := []string{rbacFilterName, corsFilterName, jwtAuthnFilterName, extAuthzFilterName, localRateLimitFilterName, routerFilterName}
	if got := strings.Join(httpFilterNames(t, hcm), ","); got != strings.Join(wantOrder, ",") {
		t.Fatalf("filter order = %s, want %v", got, wantOrder)
	}
	cfg := extAuthFilterOf(t, hcm)
	if !cfg.GetFailureModeAllow() || cfg.GetTransportApiVersion() != corev3.ApiVersion_V3 {
		t.Fatalf("extAuth config = %+v", cfg)
	}
	clusterName := cfg.GetGrpcService().GetEnvoyGrpc().GetClusterName()
	cluster := findCluster(t, res, clusterName)
	if cluster.GetType() != clusterv3.Cluster_STATIC || cluster.GetTypedExtensionProtocolOptions() == nil {
		t.Fatalf("gRPC auth cluster = %+v", cluster)
	}
	routes := findRouteConfig(t, res, "rc/web").GetVirtualHosts()[0].GetRoutes()
	for _, idx := range []int{1, 2} {
		perRoute := &extauthzv3.ExtAuthzPerRoute{}
		mustUnmarshal(t, routes[idx].GetTypedPerFilterConfig()[extAuthzFilterName], perRoute)
		if !perRoute.GetDisabled() {
			t.Fatalf("route[%d] extAuth = %+v, want disabled", idx, perRoute)
		}
	}
	protected := &extauthzv3.ExtAuthzPerRoute{}
	mustUnmarshal(t, routes[0].GetTypedPerFilterConfig()[extAuthzFilterName], protected)
	if protected.GetCheckSettings() == nil {
		t.Fatalf("route[0] extAuth = %+v, want enabled marker", protected)
	}
}

// TestExtAuthHTTP 覆盖 HTTPS HTTP auth service、pathPrefix 与 TLS auth cluster。
func TestExtAuthHTTP(t *testing.T) {
	spec := protocol.PolicySpec{ExtAuth: &protocol.ExtAuthPolicy{HTTP: &protocol.ExtAuthHTTP{
		Address: "https://auth.example.com:9443", PathPrefix: "/verify", CAFile: "/etc/ssl/auth-ca.pem",
	}}}
	cs := policyCS(nil, nil, nil, []protocol.PolicyAttachment{{Inline: &spec}})
	res, errs := buildCS(t, cs)
	assertNoErrs(t, errs)
	hcm := hcmOf(t, findListener(t, res, "lis/web").GetFilterChains()[0])
	cfg := extAuthFilterOf(t, hcm)
	if cfg.GetHttpService().GetPathPrefix() != "/verify" || cfg.GetHttpService().GetServerUri().GetUri() != "https://auth.example.com:9443" {
		t.Fatalf("HTTP extAuth config = %+v", cfg.GetHttpService())
	}
	cluster := findCluster(t, res, cfg.GetHttpService().GetServerUri().GetCluster())
	if cluster.GetType() != clusterv3.Cluster_STRICT_DNS || cluster.GetTransportSocket() == nil {
		t.Fatalf("HTTPS auth cluster = %+v", cluster)
	}
}

// TestExtAuthConflict rejects two effective listener-level service configurations.
func TestExtAuthConflict(t *testing.T) {
	a := protocol.PolicySpec{ExtAuth: &protocol.ExtAuthPolicy{GRPC: &protocol.ExtAuthGRPC{Address: "127.0.0.1:9000"}}}
	b := protocol.PolicySpec{ExtAuth: &protocol.ExtAuthPolicy{GRPC: &protocol.ExtAuthGRPC{Address: "127.0.0.1:9001"}}}
	cs := policyCS(nil, nil, nil, []protocol.PolicyAttachment{{Inline: &a}})
	second := cs.Routes[0].Spec.Rules[0]
	second.Policies = []protocol.PolicyAttachment{{Inline: &b}}
	cs.Routes[0].Spec.Rules = append(cs.Routes[0].Spec.Rules, second)
	_, errs := buildCS(t, cs)
	if len(errs) != 1 || errs[0].Stage != StageBuild || !strings.Contains(errs[0].Message, "conflicts") {
		t.Fatalf("want one extAuth conflict, got:\n%s", formatErrs(errs))
	}
}

// TestUnsupportedPolicyType 未实现策略类型显式报 build 错误（不静默丢弃）。
func TestUnsupportedPolicyType(t *testing.T) {
	spec := protocol.PolicySpec{BasicAuth: &protocol.BasicAuthPolicy{Users: "user:hash"}}
	cs := policyCS(nil, nil, nil, []protocol.PolicyAttachment{{Inline: &spec}})
	_, errs := buildCS(t, cs)
	if len(errs) != 1 || errs[0].Stage != StageBuild || !strings.Contains(errs[0].Message, "basicAuth is not implemented in M0") {
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
