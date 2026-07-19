package protocol

import (
	"strings"
	"testing"
	"time"

	yamlv3 "gopkg.in/yaml.v3"
)

// loadOne 解析单个 YAML 文档，返回 ConfigSet 与加载错误（若有）。
func loadOne(t *testing.T, yamlDoc string) (*ConfigSet, *LoadError) {
	t.Helper()
	var node yamlv3.Node
	if err := yamlv3.Unmarshal([]byte(yamlDoc), &node); err != nil {
		t.Fatalf("YAML parse: %v", err)
	}
	j, err := yamlNodeToJSON(&node)
	if err != nil {
		t.Fatalf("yamlNodeToJSON: %v", err)
	}
	cs := &ConfigSet{}
	lerr := loadDoc(cs, map[Kind]map[string]Origin{}, Origin{File: "test.yaml", DocIndex: 0}, j)
	return cs, lerr
}

// expectLoadErr 断言文档加载失败且错误信息包含 substr。
func expectLoadErr(t *testing.T, yamlDoc, substr string) {
	t.Helper()
	_, lerr := loadOne(t, yamlDoc)
	if lerr == nil {
		t.Fatalf("expected load error containing %q, got nil\n---\n%s", substr, yamlDoc)
	}
	if !strings.Contains(lerr.Message, substr) {
		t.Fatalf("expected error containing %q, got %q", substr, lerr.Message)
	}
}

// expectLoadOK 断言文档加载成功。
func expectLoadOK(t *testing.T, yamlDoc string) *ConfigSet {
	t.Helper()
	cs, lerr := loadOne(t, yamlDoc)
	if lerr != nil {
		t.Fatalf("expected load success, got %v\n---\n%s", lerr, yamlDoc)
	}
	return cs
}

func TestLoadDirS1(t *testing.T) {
	cs, errs := LoadDir("testdata/s1")
	if len(errs) != 0 {
		t.Fatalf("S1 load errors: %v", errs)
	}
	// 协议 §8.1：7 个对象 = 2 Listener + 3 Route + 2 Upstream。
	if len(cs.Listeners) != 2 || len(cs.Routes) != 3 || len(cs.Upstreams) != 2 {
		t.Fatalf("S1 object counts: listeners=%d routes=%d upstreams=%d, want 2/3/2",
			len(cs.Listeners), len(cs.Routes), len(cs.Upstreams))
	}

	https := cs.Listeners[0]
	if https.Metadata.Name != "https" || https.Spec.Port != 443 || https.Spec.Protocol != ProtocolHTTPS {
		t.Fatalf("https listener: %+v", https.Spec)
	}
	if https.Spec.TLS == nil || len(https.Spec.TLS.Certificates) != 2 {
		t.Fatalf("https listener tls certificates: %+v", https.Spec.TLS)
	}
	if https.Spec.TLS.Certificates[0].CertFile != "/etc/esgw/certs/www.crt" ||
		https.Spec.TLS.Certificates[0].KeyFile != "/etc/esgw/certs/www.key" {
		t.Fatalf("cert[0]: %+v", https.Spec.TLS.Certificates[0])
	}
	if https.Origin.File != "testdata/s1/exercise.yaml" || https.Origin.DocIndex != 0 {
		t.Fatalf("https origin: %+v", https.Origin)
	}

	var redirect *Route
	for _, r := range cs.Routes {
		if r.Metadata.Name == "http-redirect" {
			redirect = r
		}
	}
	if redirect == nil || redirect.Spec.Rules[0].Redirect == nil {
		t.Fatalf("http-redirect route: %+v", redirect)
	}
	if redirect.Spec.Rules[0].Redirect.Code != 301 || redirect.Spec.Rules[0].Redirect.Scheme != "https" {
		t.Fatalf("redirect: %+v", redirect.Spec.Rules[0].Redirect)
	}

	www := cs.Routes[0]
	if www.Metadata.Name != "www" || www.Spec.Hostnames[0] != "www.example.com" ||
		www.Spec.Rules[0].Backends[0].Upstream != "www-app" {
		t.Fatalf("www route: %+v", www.Spec)
	}
	if cs.Upstreams[0].Metadata.Name != "www-app" ||
		cs.Upstreams[0].Spec.Endpoints[0].Address != "127.0.0.1" ||
		cs.Upstreams[0].Spec.Endpoints[0].Port != 3000 {
		t.Fatalf("www-app upstream: %+v", cs.Upstreams[0].Spec)
	}
}

func TestLoadDirS2(t *testing.T) {
	cs, errs := LoadDir("testdata/s2")
	if len(errs) != 0 {
		t.Fatalf("S2 load errors: %v", errs)
	}
	// 协议 §8.2：5 个对象 = 1 Route + 3 Policy + 1 Upstream。
	if len(cs.Routes) != 1 || len(cs.Policies) != 3 || len(cs.Upstreams) != 1 {
		t.Fatalf("S2 object counts: routes=%d policies=%d upstreams=%d, want 1/3/1",
			len(cs.Routes), len(cs.Policies), len(cs.Upstreams))
	}

	api := cs.Routes[0]
	if api.Metadata.Name != "api" || len(api.Spec.Rules) != 3 {
		t.Fatalf("api route: %+v", api.Spec)
	}
	if len(api.Spec.Policies) != 2 || api.Spec.Policies[0].Ref != "jwt-main" || api.Spec.Policies[1].Ref != "cors-web" {
		t.Fatalf("api route policies: %+v", api.Spec.Policies)
	}

	login := api.Spec.Rules[0]
	if login.Match.Path == nil || login.Match.Path.Exact != "/auth/login" || len(login.Match.Methods) != 1 {
		t.Fatalf("login rule match: %+v", login.Match)
	}
	if len(login.Policies) != 2 || login.Policies[0].Ref != "jwt-off" {
		t.Fatalf("login rule policies: %+v", login.Policies)
	}
	// 内联匿名策略（union 形态二）：rateLimit。
	inline := login.Policies[1].Inline
	if inline == nil || inline.RateLimit == nil {
		t.Fatalf("login inline policy: %+v", login.Policies[1])
	}
	if inline.RateLimit.Requests != 10 || inline.RateLimit.Unit != RateLimitUnitMinute || inline.RateLimit.Key != "clientIP" {
		t.Fatalf("inline rateLimit: %+v", inline.RateLimit)
	}

	users := api.Spec.Rules[1]
	if users.Rewrite == nil || users.Rewrite.PathPrefix != "/" {
		t.Fatalf("users rewrite: %+v", users.Rewrite)
	}
	if len(users.Backends) != 2 || *users.Backends[0].Weight != 90 || *users.Backends[1].Weight != 10 {
		t.Fatalf("users backends: %+v", users.Backends)
	}
	if users.Timeout == nil || users.Timeout.Duration != 10*time.Second {
		t.Fatalf("users timeout: %+v", users.Timeout)
	}
	if users.Retry == nil || users.Retry.Attempts != 2 || users.Retry.PerTryTimeout.Duration != 2*time.Second ||
		len(users.Retry.On) != 2 || users.Retry.On[0] != RetryOn5xx || users.Retry.On[1] != RetryOnConnectFailure {
		t.Fatalf("users retry: %+v", users.Retry)
	}

	var jwtOff *Policy
	for _, p := range cs.Policies {
		if p.Metadata.Name == "jwt-off" {
			jwtOff = p
		}
	}
	if jwtOff == nil || jwtOff.Spec.JWT == nil || !jwtOff.Spec.JWT.Optional {
		t.Fatalf("jwt-off policy: %+v", jwtOff)
	}
	cors := cs.Policies[2]
	if cors.Metadata.Name != "cors-web" || cors.Spec.CORS == nil ||
		cors.Spec.CORS.MaxAge == nil || cors.Spec.CORS.MaxAge.Duration != 24*time.Hour {
		t.Fatalf("cors-web policy: %+v", cors.Spec)
	}

	order := cs.Upstreams[0]
	if order.Spec.DNS == nil || order.Spec.DNS.Hostname != "order.internal" || order.Spec.DNS.Port != 9090 {
		t.Fatalf("order-svc dns: %+v", order.Spec.DNS)
	}
	if order.Spec.Connection == nil || !order.Spec.Connection.HTTP2 {
		t.Fatalf("order-svc connection: %+v", order.Spec.Connection)
	}
	hc := order.Spec.HealthCheck
	if hc == nil || hc.HTTP == nil || hc.HTTP.Path != "/healthz" ||
		hc.Interval.Duration != 10*time.Second || hc.Timeout.Duration != 2*time.Second ||
		*hc.HealthyThreshold != 2 || *hc.UnhealthyThreshold != 3 {
		t.Fatalf("order-svc healthCheck: %+v", hc)
	}
}

func TestLoadDirFileOrderingAndOrigin(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "02-second.yaml", `
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: second}
spec:
  listeners: [http]
  rules:
    - match: {path: {prefix: /}}
      directResponse: {status: 200, body: ok}
`)
	writeFile(t, dir, "01-first.yaml", `
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: first}
spec:
  listeners: [http]
  forward: {upstream: tcp-svc}
`)
	writeFile(t, dir, "ignored.txt", `not yaml`)
	cs, errs := LoadDir(dir)
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if len(cs.Routes) != 2 || cs.Routes[0].Metadata.Name != "first" || cs.Routes[1].Metadata.Name != "second" {
		t.Fatalf("file ordering: %+v", cs.Routes)
	}
	if cs.Routes[0].Origin.DocIndex != 0 || !strings.HasSuffix(cs.Routes[0].Origin.File, "01-first.yaml") {
		t.Fatalf("origin: %+v", cs.Routes[0].Origin)
	}
	// L4 forward 形态可解析（rules 与 forward 互斥校验归 T3）。
	if cs.Routes[0].Spec.Forward == nil || cs.Routes[0].Spec.Forward.Upstream != "tcp-svc" {
		t.Fatalf("forward: %+v", cs.Routes[0].Spec.Forward)
	}
}

func TestLoadDirDuplicateName(t *testing.T) {
	dir := t.TempDir()
	listener := func(port int) string {
		return `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: http}
spec: {port: ` + itoa(port) + `, protocol: HTTP}
`
	}
	writeFile(t, dir, "01-a.yaml", listener(80))
	writeFile(t, dir, "02-b.yaml", listener(8080))
	cs, errs := LoadDir(dir)
	if len(errs) != 1 {
		t.Fatalf("want 1 duplicate error, got %v", errs)
	}
	// 错误信息带两处 Origin。
	if !strings.Contains(errs[0].Message, "01-a.yaml") || !strings.Contains(errs[0].Message, "duplicate") {
		t.Fatalf("duplicate error should mention first origin: %v", errs[0])
	}
	if !strings.Contains(errs[0].Origin.File, "02-b.yaml") {
		t.Fatalf("error origin should be second location: %v", errs[0].Origin)
	}
	// 首个定义保留。
	if len(cs.Listeners) != 1 || cs.Listeners[0].Spec.Port != 80 {
		t.Fatalf("first definition should be kept: %+v", cs.Listeners)
	}
}

func TestLoadDirErrorCollectionContinues(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "01-bad.yaml", `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: bad}
spec: {port: 80, protocol: HTTP, typoField: 1}
`)
	writeFile(t, dir, "02-good.yaml", `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: good}
spec: {port: 81, protocol: HTTP}
`)
	cs, errs := LoadDir(dir)
	if len(errs) != 1 || !strings.Contains(errs[0].Message, `unknown field "typoField"`) {
		t.Fatalf("want unknown-field error, got %v", errs)
	}
	if len(cs.Listeners) != 1 || cs.Listeners[0].Metadata.Name != "good" {
		t.Fatalf("good doc should still load: %+v", cs.Listeners)
	}
}

func TestLoadDirSkipsEmptyDocs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yaml", `---
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: u}
spec:
  endpoints: [{address: 10.0.0.1, port: 8080}]
---
`)
	cs, errs := LoadDir(dir)
	if len(errs) != 0 || len(cs.Upstreams) != 1 {
		t.Fatalf("cs=%+v errs=%v", cs, errs)
	}
}

func TestEnvelopeValidation(t *testing.T) {
	expectLoadErr(t, `
apiVersion: esgw/v2
kind: Listener
metadata: {name: http}
spec: {port: 80, protocol: HTTP}
`, `unsupported apiVersion`)

	expectLoadErr(t, `
apiVersion: esgw/v1alpha1
kind: Ingress
metadata: {name: x}
spec: {}
`, `unsupported kind`)

	expectLoadErr(t, `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: Bad_Name}
spec: {port: 80, protocol: HTTP}
`, `invalid name`)

	expectLoadErr(t, `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: http}
spec: {port: 80, protocol: HTTP}
extraTopLevel: 1
`, `unknown field "extraTopLevel"`)

	// Gateway 显式声明时 name 必须为 default（协议 §2.2）。
	expectLoadErr(t, `
apiVersion: esgw/v1alpha1
kind: Gateway
metadata: {name: other}
spec: {}
`, `must be "default"`)

	expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Gateway
metadata: {name: default}
spec:
  accessLog: {enabled: true, format: json}
  http: {idleTimeout: 90s, maxRequestHeadersKb: 64, serverHeader: ""}
  policies: [p1]
`)
}

func TestNameRegex(t *testing.T) {
	valid := []string{"a", "ab", "a-b", "0", "api-1", "a" + strings.Repeat("b", 61) + "c"}
	for _, n := range valid {
		if err := ValidateName(n); err != nil {
			t.Fatalf("name %q should be valid: %v", n, err)
		}
	}
	invalid := []string{"", "A", "-ab", "ab-", "a_b", "a.b", "a" + strings.Repeat("b", 62) + "c", "中文"}
	for _, n := range invalid {
		if err := ValidateName(n); err == nil {
			t.Fatalf("name %q should be invalid", n)
		}
	}
}
