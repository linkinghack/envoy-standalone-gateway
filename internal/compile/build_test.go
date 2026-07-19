package compile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/cors/v3"
	jwtv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/jwt_authn/v3"
	localratelimitv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/local_ratelimit/v3"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// loadS1 加载 S1 演练配置（证书路径替换为动态生成的测试自签证书，同 TestS1PassF2）。
func loadS1(t *testing.T) *protocol.ConfigSet {
	t.Helper()
	dir := t.TempDir()
	writeSelfSignedCert(t, dir, "www", "www.example.com")
	writeSelfSignedCert(t, dir, "blog", "blog.example.com")
	raw, err := os.ReadFile("../protocol/testdata/s1/exercise.yaml")
	if err != nil {
		t.Fatalf("read S1 exercise: %v", err)
	}
	yamlDoc := strings.ReplaceAll(string(raw), "/etc/esgw/certs/", dir+"/")
	if err := os.WriteFile(filepath.Join(dir, "exercise.yaml"), []byte(yamlDoc), 0o600); err != nil {
		t.Fatal(err)
	}
	cs, loadErrs := protocol.LoadDir(dir)
	if len(loadErrs) != 0 {
		t.Fatalf("S1 load errors: %v", loadErrs)
	}
	return cs
}

// loadS2 加载 S2 演练配置（补齐 https Listener 与三个同构上游，同 TestS2PassF2）。
func loadS2(t *testing.T) *protocol.ConfigSet {
	t.Helper()
	dir := t.TempDir()
	certFile, keyFile := writeSelfSignedCert(t, dir, "api", "api.example.com")
	raw, err := os.ReadFile("../protocol/testdata/s2/exercise.yaml")
	if err != nil {
		t.Fatalf("read S2 exercise: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "exercise.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	supplement := fmt.Sprintf(`
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: https}
spec:
  port: 443
  protocol: HTTPS
  tls:
    certificates:
      - {certFile: %s, keyFile: %s}
---
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: user-svc}
spec:
  endpoints: [{address: 10.0.0.1, port: 8080}]
---
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: user-svc-canary}
spec:
  endpoints: [{address: 10.0.0.2, port: 8080}]
---
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: auth-svc}
spec:
  endpoints: [{address: 10.0.0.3, port: 8080}]
`, certFile, keyFile)
	if err := os.WriteFile(filepath.Join(dir, "supplement.yaml"), []byte(supplement), 0o600); err != nil {
		t.Fatal(err)
	}
	cs, loadErrs := protocol.LoadDir(dir)
	if len(loadErrs) != 0 {
		t.Fatalf("S2 load errors: %v", loadErrs)
	}
	return cs
}

// TestS1Build 是 S1 主干验收（任务书：S1 全部对象可构建出结构正确的逻辑资源）。
func TestS1Build(t *testing.T) {
	cs := loadS1(t)
	res, errs := buildCS(t, cs)
	assertNoErrs(t, errs)

	// Listener：lis/http（80，单 chain）、lis/https（443，双证书双 chain）。
	if len(res.listeners) != 2 || res.listeners[0].GetName() != "lis/http" || res.listeners[1].GetName() != "lis/https" {
		t.Fatalf("listeners = %v", res.listeners)
	}
	chains := res.listeners[1].GetFilterChains()
	if len(chains) != 2 {
		t.Fatalf("https chains = %d, want 2", len(chains))
	}
	if got := chains[0].GetFilterChainMatch().GetServerNames(); len(got) != 1 || got[0] != "www.example.com" {
		t.Fatalf("chains[0] server_names = %v", got)
	}
	if got := chains[1].GetFilterChainMatch().GetServerNames(); len(got) != 1 || got[0] != "blog.example.com" {
		t.Fatalf("chains[1] server_names = %v", got)
	}

	// RouteConfiguration：rc/http 一个 vhost（redirect），rc/https 两个 vhost（按 Route name 排序）。
	rcHTTP := findRouteConfig(t, res, "rc/http")
	if len(rcHTTP.GetVirtualHosts()) != 1 || rcHTTP.GetVirtualHosts()[0].GetName() != "vh/http-redirect" {
		t.Fatalf("rc/http vhosts = %v", rcHTTP.GetVirtualHosts())
	}
	red := rcHTTP.GetVirtualHosts()[0].GetRoutes()[0].GetRedirect()
	if red.GetSchemeRedirect() != "https" {
		t.Fatalf("redirect = %v", red)
	}
	rcHTTPS := findRouteConfig(t, res, "rc/https")
	vhosts := rcHTTPS.GetVirtualHosts()
	if len(vhosts) != 2 || vhosts[0].GetName() != "vh/blog" || vhosts[1].GetName() != "vh/www" {
		t.Fatalf("rc/https vhosts = %v", vhosts)
	}
	if vhosts[0].GetRoutes()[0].GetRoute().GetCluster() != "us/blog-app" ||
		vhosts[1].GetRoutes()[0].GetRoute().GetCluster() != "us/www-app" {
		t.Fatalf("route clusters = %v / %v", vhosts[0].GetRoutes()[0], vhosts[1].GetRoutes()[0])
	}

	// Cluster：按 name 排序的 STATIC 集群。
	if len(res.clusters) != 2 || res.clusters[0].GetName() != "us/blog-app" || res.clusters[1].GetName() != "us/www-app" {
		t.Fatalf("clusters = %v", res.clusters)
	}
	ep := res.clusters[0].GetLoadAssignment().GetEndpoints()[0].GetLbEndpoints()[0].GetEndpoint()
	if ep.GetAddress().GetSocketAddress().GetPortValue() != 4000 {
		t.Fatalf("blog-app endpoint = %v", ep)
	}
}

// TestS2Build 是 S2 主干验收：路由/重写/灰度/超时重试/JWT/CORS/限流/健康检查/gRPC 上游。
func TestS2Build(t *testing.T) {
	cs := loadS2(t)
	res, errs := buildCS(t, cs)
	assertNoErrs(t, errs)

	// HTTPS listener 单证书；HCM filter 链固定四件套。
	lis := findListener(t, res, "lis/https")
	hcm := hcmOf(t, lis.GetFilterChains()[0])
	names := httpFilterNames(t, hcm)
	want := []string{corsFilterName, jwtAuthnFilterName, localRateLimitFilterName, routerFilterName}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("filter chain = %v, want %v", names, want)
	}

	// jwt_authn：一个聚合 provider + requirement_map（主 requirement + allow-missing）。
	jwt := &jwtv3.JwtAuthentication{}
	for _, f := range hcm.GetHttpFilters() {
		if f.GetName() == jwtAuthnFilterName {
			mustUnmarshal(t, f.GetTypedConfig(), jwt)
		}
	}
	if len(jwt.GetProviders()) != 1 {
		t.Fatalf("jwt providers = %d, want 1", len(jwt.GetProviders()))
	}
	if jwt.GetRequirementMap()[jwtAllowMissingRequirement] == nil {
		t.Fatalf("requirement_map = %v, want allow-missing entry", jwt.GetRequirementMap())
	}

	rc := findRouteConfig(t, res, "rc/https")
	if len(rc.GetVirtualHosts()) != 1 || rc.GetVirtualHosts()[0].GetName() != "vh/api" {
		t.Fatalf("vhosts = %v", rc.GetVirtualHosts())
	}
	vh := rc.GetVirtualHosts()[0]
	if len(vh.GetDomains()) != 1 || vh.GetDomains()[0] != "api.example.com" {
		t.Fatalf("domains = %v", vh.GetDomains())
	}
	rules := vh.GetRoutes()
	if len(rules) != 3 {
		t.Fatalf("rules = %d, want 3（保序）", len(rules))
	}

	// rule[0] 登录：exact path + POST；jwt allow-missing + 内联限流。
	if rules[0].GetMatch().GetPath() != "/auth/login" {
		t.Fatalf("rule[0] match = %v", rules[0].GetMatch())
	}
	perRoute := &jwtv3.PerRouteConfig{}
	mustUnmarshal(t, rules[0].GetTypedPerFilterConfig()[jwtAuthnFilterName], perRoute)
	if perRoute.GetRequirementName() != jwtAllowMissingRequirement {
		t.Fatalf("rule[0] jwt = %q, want allow-missing", perRoute.GetRequirementName())
	}
	lrl := &localratelimitv3.LocalRateLimit{}
	mustUnmarshal(t, rules[0].GetTypedPerFilterConfig()[localRateLimitFilterName], lrl)
	if lrl.GetTokenBucket().GetMaxTokens() != 10 || lrl.GetTokenBucket().GetFillInterval().GetSeconds() != 60 {
		t.Fatalf("rule[0] rate limit = %v, want 10/minute", lrl.GetTokenBucket())
	}
	// rule[0] 同时有 cors（Route 级策略流进所有 rule）。
	if rules[0].GetTypedPerFilterConfig()[corsFilterName] == nil {
		t.Fatal("rule[0] missing cors（Route 级 cors-web 应流进全部 rules）")
	}

	// rule[1] users：前缀重写 + 90/10 灰度 + 10s 超时 + 重试。
	r1 := rules[1].GetRoute()
	if r1.GetPrefixRewrite() != "/" {
		t.Fatalf("rule[1] prefix_rewrite = %q", r1.GetPrefixRewrite())
	}
	wc := r1.GetWeightedClusters()
	if len(wc.GetClusters()) != 2 ||
		wc.GetClusters()[0].GetName() != "us/user-svc" || wc.GetClusters()[0].GetWeight().GetValue() != 90 ||
		wc.GetClusters()[1].GetName() != "us/user-svc-canary" || wc.GetClusters()[1].GetWeight().GetValue() != 10 {
		t.Fatalf("rule[1] weighted clusters = %v", wc.GetClusters())
	}
	if r1.GetTimeout().GetSeconds() != 10 {
		t.Fatalf("rule[1] timeout = %v", r1.GetTimeout())
	}
	rp := r1.GetRetryPolicy()
	if rp.GetNumRetries().GetValue() != 2 || rp.GetRetryOn() != "5xx,connect-failure" {
		t.Fatalf("rule[1] retry = %v", rp)
	}
	// jwt 强制 requirement（provider_and_audiences，audiences=api.example.com）。
	perRoute1 := &jwtv3.PerRouteConfig{}
	mustUnmarshal(t, rules[1].GetTypedPerFilterConfig()[jwtAuthnFilterName], perRoute1)
	req := jwt.GetRequirementMap()[perRoute1.GetRequirementName()]
	if req.GetProviderAndAudiences() == nil {
		t.Fatalf("rule[1] jwt requirement = %v", req)
	}
	cors1 := &corsv3.CorsPolicy{}
	mustUnmarshal(t, rules[1].GetTypedPerFilterConfig()[corsFilterName], cors1)
	if len(cors1.GetAllowOriginStringMatch()) != 1 ||
		cors1.GetAllowOriginStringMatch()[0].GetSafeRegex().GetRegex() != `^https://.*\.example\.com$` {
		t.Fatalf("rule[1] cors = %v", cors1)
	}

	// rule[2] orders：30s 超时，转发 us/order-svc。
	if rules[2].GetRoute().GetCluster() != "us/order-svc" || rules[2].GetRoute().GetTimeout().GetSeconds() != 30 {
		t.Fatalf("rule[2] = %v", rules[2].GetRoute())
	}

	// order-svc：LOGICAL_DNS + http2 + 健康检查。
	order := findCluster(t, res, "us/order-svc")
	if order.GetType().String() != "LOGICAL_DNS" {
		t.Fatalf("order-svc type = %v", order.GetType())
	}
	if order.GetTypedExtensionProtocolOptions()["envoy.extensions.upstreams.http.v3.HttpProtocolOptions"] == nil {
		t.Fatal("order-svc must enable http2")
	}
	if len(order.GetHealthChecks()) != 1 || order.GetHealthChecks()[0].GetHttpHealthCheck().GetPath() != "/healthz" {
		t.Fatalf("order-svc health check = %v", order.GetHealthChecks())
	}

	// 远程 JWKS 抓取集群存在。
	findCluster(t, res, "jwt-jwks/auth.example.com:443")
}

// TestBuildDeterminism 锁定确定性（编译层 §5）：同一 ConfigSet 两次构建，
// protojson 序列化逐字节相同；同时全量资源过 PGV Validate（F6 前置自检）。
func TestBuildDeterminism(t *testing.T) {
	for _, scenario := range []struct {
		name string
		load func(*testing.T) *protocol.ConfigSet
	}{
		{"S1", loadS1},
		{"S2", loadS2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// 同一已链接输入构建两次（证书 SAN 提取结果一致）。
			cs := scenario.load(t)
			lk, lerrs := link(cs, Options{Mode: ModeStatic}, defaultCertVerifier())
			assertNoErrs(t, lerrs)
			r1, e1 := build(cs, lk, nil)
			assertNoErrs(t, e1)
			r2, e2 := build(cs, lk, nil)
			assertNoErrs(t, e2)

			marshalAll := func(res *buildResult) string {
				var b strings.Builder
				write := func(name string, m proto.Message) {
					out, err := protojson.MarshalOptions{}.Marshal(m)
					if err != nil {
						t.Fatalf("marshal %s: %v", name, err)
					}
					b.Write(out)
					b.WriteByte('\n')
				}
				for _, l := range res.listeners {
					write(l.GetName(), l)
				}
				for _, rc := range res.routes {
					write(rc.GetName(), rc)
				}
				for _, c := range res.clusters {
					write(c.GetName(), c)
				}
				return b.String()
			}
			if got1, got2 := marshalAll(r1), marshalAll(r2); got1 != got2 {
				t.Fatal("build is not deterministic: two runs produced different protojson")
			}

			// PGV Validate 自检（F6 的预演；此处失败 = Builder 结构性缺陷）。
			for _, l := range r1.listeners {
				if err := l.Validate(); err != nil {
					t.Fatalf("listener %s PGV: %v", l.GetName(), err)
				}
			}
			for _, rc := range r1.routes {
				if err := rc.Validate(); err != nil {
					t.Fatalf("route config %s PGV: %v", rc.GetName(), err)
				}
			}
			for _, c := range r1.clusters {
				if err := c.Validate(); err != nil {
					t.Fatalf("cluster %s PGV: %v", c.GetName(), err)
				}
			}
		})
	}
}
