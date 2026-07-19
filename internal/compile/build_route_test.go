package compile

import (
	"strings"
	"testing"
	"time"

	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// TestVirtualHostDomains 覆盖 vhost 命名与 domains 排序（精确 > 通配 > 兜底，编译层 §5）。
func TestVirtualHostDomains(t *testing.T) {
	cases := []struct {
		name      string
		hostnames []string
		want      []string
	}{
		{"empty is fallback", nil, []string{"*"}},
		{
			"exact before wildcard before fallback",
			[]string{"*", "*.example.com", "api.example.com"},
			[]string{"api.example.com", "*.example.com", "*"},
		},
		{
			"lexicographic within group",
			[]string{"b.example.com", "a.example.com"},
			[]string{"a.example.com", "b.example.com"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newHTTPRoute("r1", []string{"web"}, tc.hostnames, "app")
			cs := &protocol.ConfigSet{
				Listeners: []*protocol.Listener{newListener("web", 80, protocol.ProtocolHTTP)},
				Routes:    []*protocol.Route{r},
				Upstreams: []*protocol.Upstream{newUpstream("app")},
			}
			res, errs := buildCS(t, cs)
			assertNoErrs(t, errs)
			vh := findRouteConfig(t, res, "rc/web").GetVirtualHosts()[0]
			if vh.GetName() != "vh/r1" {
				t.Fatalf("vhost name = %q, want vh/r1", vh.GetName())
			}
			if strings.Join(vh.GetDomains(), ",") != strings.Join(tc.want, ",") {
				t.Fatalf("domains = %v, want %v", vh.GetDomains(), tc.want)
			}
		})
	}
}

// TestRuleMatch 覆盖 match 翻译：path 三形态、methods、headers、queryParams。
func TestRuleMatch(t *testing.T) {
	present := true
	rule := protocol.Rule{
		Match: protocol.RuleMatch{
			Path:    &protocol.PathMatch{Regex: "^/v[0-9]+/items$"},
			Methods: []string{"GET", "POST"},
			Headers: []protocol.KeyValueMatch{
				{Name: "x-exact", Exact: "a"},
				{Name: "x-re", Regex: "^v[0-9]+$"},
				{Name: "x-present", Present: &present},
			},
			QueryParams: []protocol.KeyValueMatch{{Name: "debug", Exact: "1"}},
		},
		Backends: []protocol.BackendRef{{Upstream: "app"}},
	}
	route := buildSingleRuleRoute(t, rule)

	m := route.GetMatch()
	if m.GetSafeRegex().GetRegex() != "^/v[0-9]+/items$" {
		t.Fatalf("path safe_regex = %v", m.GetPathSpecifier())
	}
	if len(m.GetHeaders()) != 4 {
		t.Fatalf("headers = %d, want 4（:method + 3）", len(m.GetHeaders()))
	}
	if m.GetHeaders()[0].GetName() != ":method" ||
		m.GetHeaders()[0].GetStringMatch().GetSafeRegex().GetRegex() != "^(?:GET|POST)$" {
		t.Fatalf("methods matcher = %v", m.GetHeaders()[0])
	}
	if m.GetHeaders()[1].GetStringMatch().GetExact() != "a" {
		t.Fatalf("header exact = %v", m.GetHeaders()[1])
	}
	if m.GetHeaders()[2].GetStringMatch().GetSafeRegex().GetRegex() != "^v[0-9]+$" {
		t.Fatalf("header regex = %v", m.GetHeaders()[2])
	}
	if !m.GetHeaders()[3].GetPresentMatch() {
		t.Fatalf("header present = %v", m.GetHeaders()[3])
	}
	if len(m.GetQueryParameters()) != 1 || m.GetQueryParameters()[0].GetName() != "debug" ||
		m.GetQueryParameters()[0].GetStringMatch().GetExact() != "1" {
		t.Fatalf("query params = %v", m.GetQueryParameters())
	}

	// exact / prefix path 形态。
	exact := buildSingleRuleRoute(t, protocol.Rule{
		Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Exact: "/a"}},
		Backends: []protocol.BackendRef{{Upstream: "app"}},
	})
	if exact.GetMatch().GetPath() != "/a" {
		t.Fatalf("exact path = %v", exact.GetMatch().GetPathSpecifier())
	}
	prefix := buildSingleRuleRoute(t, protocol.Rule{
		Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Prefix: "/v1/"}},
		Backends: []protocol.BackendRef{{Upstream: "app"}},
	})
	if prefix.GetMatch().GetPrefix() != "/v1/" {
		t.Fatalf("prefix path = %v", prefix.GetMatch().GetPathSpecifier())
	}
}

// TestRuleActions 覆盖动作三选一：backends（单/加权）、redirect、directResponse。
func TestRuleActions(t *testing.T) {
	t.Run("single backend without weight", func(t *testing.T) {
		route := buildSingleRuleRoute(t, protocol.Rule{
			Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Prefix: "/"}},
			Backends: []protocol.BackendRef{{Upstream: "app"}},
		})
		if route.GetRoute().GetCluster() != "us/app" {
			t.Fatalf("cluster = %q, want us/app", route.GetRoute().GetCluster())
		}
	})

	t.Run("weighted clusters", func(t *testing.T) {
		route := buildSingleRuleRoute(t, protocol.Rule{
			Match: protocol.RuleMatch{Path: &protocol.PathMatch{Prefix: "/"}},
			Backends: []protocol.BackendRef{
				{Upstream: "app", Weight: ptr(int32(90))},
				{Upstream: "canary", Weight: ptr(int32(10))},
			},
		})
		wc := route.GetRoute().GetWeightedClusters()
		if len(wc.GetClusters()) != 2 ||
			wc.GetClusters()[0].GetName() != "us/app" || wc.GetClusters()[0].GetWeight().GetValue() != 90 ||
			wc.GetClusters()[1].GetName() != "us/canary" || wc.GetClusters()[1].GetWeight().GetValue() != 10 {
			t.Fatalf("weighted clusters = %v", wc.GetClusters())
		}
	})

	t.Run("redirect", func(t *testing.T) {
		route := buildSingleRuleRoute(t, protocol.Rule{
			Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Prefix: "/"}},
			Redirect: &protocol.Redirect{Scheme: "https", Code: 301},
		})
		red := route.GetRedirect()
		if red.GetSchemeRedirect() != "https" || red.GetResponseCode() != routev3.RedirectAction_MOVED_PERMANENTLY {
			t.Fatalf("redirect = %v", red)
		}
	})

	t.Run("redirect unsupported code", func(t *testing.T) {
		cs := singleRuleCS(protocol.Rule{
			Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Prefix: "/"}},
			Redirect: &protocol.Redirect{Scheme: "https", Code: 418},
		})
		_, errs := buildCS(t, cs)
		if len(errs) != 1 || errs[0].Stage != StageBuild || !strings.Contains(errs[0].Message, "418") ||
			errs[0].Source.Path != "spec.rules[0].redirect.code" {
			t.Fatalf("want unsupported code build error, got:\n%s", formatErrs(errs))
		}
	})

	t.Run("direct response", func(t *testing.T) {
		route := buildSingleRuleRoute(t, protocol.Rule{
			Match:          protocol.RuleMatch{Path: &protocol.PathMatch{Prefix: "/"}},
			DirectResponse: &protocol.DirectResponse{Status: 404, Body: "not found"},
		})
		dr := route.GetDirectResponse()
		if dr.GetStatus() != 404 || dr.GetBody().GetInlineString() != "not found" {
			t.Fatalf("direct response = %v", dr)
		}
	})
}

// TestRuleRewrite 覆盖 rewrite 翻译：pathPrefix / path / regex + host。
func TestRuleRewrite(t *testing.T) {
	t.Run("pathPrefix", func(t *testing.T) {
		route := buildSingleRuleRoute(t, protocol.Rule{
			Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Prefix: "/v1/"}},
			Rewrite:  &protocol.Rewrite{PathPrefix: "/"},
			Backends: []protocol.BackendRef{{Upstream: "app"}},
		})
		if route.GetRoute().GetPrefixRewrite() != "/" {
			t.Fatalf("prefix_rewrite = %q", route.GetRoute().GetPrefixRewrite())
		}
	})

	t.Run("path whole replace", func(t *testing.T) {
		route := buildSingleRuleRoute(t, protocol.Rule{
			Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Prefix: "/"}},
			Rewrite:  &protocol.Rewrite{Path: "/fixed"},
			Backends: []protocol.BackendRef{{Upstream: "app"}},
		})
		rr := route.GetRoute().GetRegexRewrite()
		if rr.GetPattern().GetRegex() != "^/.*$" || rr.GetSubstitution() != "/fixed" {
			t.Fatalf("regex_rewrite = %v", rr)
		}
	})

	t.Run("regex and host", func(t *testing.T) {
		route := buildSingleRuleRoute(t, protocol.Rule{
			Match: protocol.RuleMatch{Path: &protocol.PathMatch{Regex: "^/v1/(.*)$"}},
			Rewrite: &protocol.Rewrite{
				Regex: &protocol.RegexSubstitution{Pattern: "^/v1/(.*)$", Substitution: "/\\1"},
				Host:  "user.internal",
			},
			Backends: []protocol.BackendRef{{Upstream: "app"}},
		})
		rr := route.GetRoute().GetRegexRewrite()
		if rr.GetPattern().GetRegex() != "^/v1/(.*)$" || rr.GetSubstitution() != "/\\1" {
			t.Fatalf("regex_rewrite = %v", rr)
		}
		if route.GetRoute().GetHostRewriteLiteral() != "user.internal" {
			t.Fatalf("host_rewrite = %q", route.GetRoute().GetHostRewriteLiteral())
		}
	})
}

// TestRuleTimeoutRetry 覆盖 timeout（默认值/0s 关闭）与 retry_policy（on 枚举映射）。
func TestRuleTimeoutRetry(t *testing.T) {
	t.Run("default timeout 15s", func(t *testing.T) {
		route := buildSingleRuleRoute(t, protocol.Rule{
			Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Prefix: "/"}},
			Backends: []protocol.BackendRef{{Upstream: "app"}},
		})
		if route.GetRoute().GetTimeout().GetSeconds() != 15 {
			t.Fatalf("timeout = %v, want 15s（F2 默认值）", route.GetRoute().GetTimeout())
		}
	})

	t.Run("timeout 0s disables", func(t *testing.T) {
		route := buildSingleRuleRoute(t, protocol.Rule{
			Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Prefix: "/"}},
			Timeout:  &protocol.Duration{Duration: 0},
			Backends: []protocol.BackendRef{{Upstream: "app"}},
		})
		if route.GetRoute().GetTimeout().GetSeconds() != 0 {
			t.Fatalf("timeout = %v, want 0（关闭路由级超时）", route.GetRoute().GetTimeout())
		}
	})

	t.Run("retry policy", func(t *testing.T) {
		route := buildSingleRuleRoute(t, protocol.Rule{
			Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Prefix: "/"}},
			Backends: []protocol.BackendRef{{Upstream: "app"}},
			Retry: &protocol.Retry{
				Attempts:      2,
				PerTryTimeout: &protocol.Duration{Duration: 2 * time.Second},
				On: []protocol.RetryOn{
					protocol.RetryOn5xx, protocol.RetryOnGatewayError, protocol.RetryOnConnectFailure,
					protocol.RetryOnReset, protocol.RetryOnRetriable4xx,
				},
			},
		})
		rp := route.GetRoute().GetRetryPolicy()
		if rp.GetNumRetries().GetValue() != 2 {
			t.Fatalf("num_retries = %d", rp.GetNumRetries().GetValue())
		}
		if rp.GetPerTryTimeout().GetSeconds() != 2 {
			t.Fatalf("per_try_timeout = %v", rp.GetPerTryTimeout())
		}
		want := "5xx,gateway-error,connect-failure,reset,retriable-4xx"
		if rp.GetRetryOn() != want {
			t.Fatalf("retry_on = %q, want %q", rp.GetRetryOn(), want)
		}
	})
}

// TestRuleHashPolicy 覆盖 hashOn → route hash_policy 联动（ringHash/maglev）。
func TestRuleHashPolicy(t *testing.T) {
	up := newUpstream("app")
	up.Spec.LoadBalancer = &protocol.LoadBalancer{
		Policy: protocol.LBPolicyRingHash,
		HashOn: []protocol.HashOn{{Header: "x-user-id"}},
	}
	route := buildSingleRuleRouteWith(t, []*protocol.Upstream{up}, protocol.Rule{
		Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Prefix: "/"}},
		Backends: []protocol.BackendRef{{Upstream: "app"}},
	})
	hp := route.GetRoute().GetHashPolicy()
	if len(hp) != 1 || hp[0].GetHeader().GetHeaderName() != "x-user-id" {
		t.Fatalf("hash_policy = %v", hp)
	}

	// roundRobin 上游不产生 hash_policy。
	route2 := buildSingleRuleRoute(t, protocol.Rule{
		Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Prefix: "/"}},
		Backends: []protocol.BackendRef{{Upstream: "app"}},
	})
	if len(route2.GetRoute().GetHashPolicy()) != 0 {
		t.Fatalf("hash_policy = %v, want none", route2.GetRoute().GetHashPolicy())
	}
}

// TestRulesOrderPreserved 规则保序（协议 P4：顺序即优先级）。
func TestRulesOrderPreserved(t *testing.T) {
	cs := singleRuleCS(protocol.Rule{
		Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Exact: "/first"}},
		Backends: []protocol.BackendRef{{Upstream: "app"}},
	})
	cs.Routes[0].Spec.Rules = append(cs.Routes[0].Spec.Rules, protocol.Rule{
		Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Exact: "/second"}},
		Backends: []protocol.BackendRef{{Upstream: "app"}},
	})
	res, errs := buildCS(t, cs)
	assertNoErrs(t, errs)
	routes := findRouteConfig(t, res, "rc/web").GetVirtualHosts()[0].GetRoutes()
	if len(routes) != 2 || routes[0].GetMatch().GetPath() != "/first" || routes[1].GetMatch().GetPath() != "/second" {
		t.Fatalf("routes order = %v", routes)
	}
}

// singleRuleCS 构造 单 Listener(web) + 单 Route(r1) + 默认上游 的 ConfigSet。
func singleRuleCS(rule protocol.Rule) *protocol.ConfigSet {
	return &protocol.ConfigSet{
		Listeners: []*protocol.Listener{newListener("web", 80, protocol.ProtocolHTTP)},
		Routes: []*protocol.Route{{
			APIVersion: protocol.APIVersionV1Alpha1,
			Kind:       protocol.KindRoute,
			Metadata:   protocol.ObjectMeta{Name: "r1"},
			Spec: protocol.RouteSpec{
				Listeners: []string{"web"},
				Rules:     []protocol.Rule{rule},
			},
			Origin: testOrigin(),
		}},
		Upstreams: []*protocol.Upstream{newUpstream("app"), newUpstream("canary")},
	}
}

// buildSingleRuleRoute 构建单 rule 的 routev3.Route（默认上游集）。
func buildSingleRuleRoute(t *testing.T, rule protocol.Rule) *routev3.Route {
	t.Helper()
	return buildSingleRuleRouteWith(t, nil, rule)
}

// buildSingleRuleRouteWith 同 buildSingleRuleRoute，可自定义上游集。
func buildSingleRuleRouteWith(t *testing.T, upstreams []*protocol.Upstream, rule protocol.Rule) *routev3.Route {
	t.Helper()
	cs := singleRuleCS(rule)
	if upstreams != nil {
		cs.Upstreams = upstreams
	}
	res, errs := buildCS(t, cs)
	assertNoErrs(t, errs)
	routes := findRouteConfig(t, res, "rc/web").GetVirtualHosts()[0].GetRoutes()
	if len(routes) != 1 {
		t.Fatalf("routes = %d, want 1", len(routes))
	}
	return routes[0]
}
