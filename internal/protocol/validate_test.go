package protocol

import (
	"strings"
	"testing"
)

// 逐对象结构层校验：每对象至少一个合法样例与若干非法样例。
// 非法样例必须报错且错误信息可读（带字段路径）。

func TestListenerValidate(t *testing.T) {
	expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: https}
spec:
  port: 443
  protocol: HTTPS
  tls:
    certificates:
      - {certFile: /a.crt, keyFile: /a.key}
      - {ref: managed-cert}
    minVersion: "1.3"
    alpn: [h2]
  http: {http2: true, http3: false}
  policies: [p1]
  envoyPatch:
    - {target: listener, op: merge, value: {stat_prefix: web}}
`)

	bad := []struct{ name, doc, substr string }{
		{"bad protocol", `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: x}
spec: {port: 80, protocol: HTTP2}
`, `invalid value "HTTP2"`},
		{"bad port", `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: x}
spec: {port: 70000, protocol: HTTP}
`, `invalid port`},
		{"missing port", `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: x}
spec: {protocol: HTTP}
`, `invalid port`},
		{"bad minVersion", `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: x}
spec: {port: 443, protocol: HTTPS, tls: {minVersion: "1.1", certificates: [{ref: c}]}}
`, `minVersion`},
		{"cert both ref and files", `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: x}
spec: {port: 443, protocol: HTTPS, tls: {certificates: [{certFile: a, keyFile: b, ref: c}]}}
`, `oneOf`},
		{"cert keyFile without certFile", `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: x}
spec: {port: 443, protocol: HTTPS, tls: {certificates: [{keyFile: b}]}}
`, `certFile and keyFile must be set together`},
		{"bad patch op", `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: x}
spec: {port: 80, protocol: HTTP, envoyPatch: [{target: listener, op: replace, value: {}}]}
`, `invalid op "replace"`},
		{"patch missing value", `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: x}
spec: {port: 80, protocol: HTTP, envoyPatch: [{target: listener, op: merge}]}
`, `value is required`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) { expectLoadErr(t, tc.doc, tc.substr) })
	}
}

func TestRouteValidate(t *testing.T) {
	expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: r}
spec:
  listeners: [http]
  hostnames: ["*.example.com"]
  rules:
    - name: legacy-regex
      match:
        path: {regex: "^/v[0-9]/"}
        methods: [GET]
        headers: [{name: x-tenant, exact: acme}]
        queryParams: [{name: debug, present: true}]
      rewrite: {regex: {pattern: "^/v1/(.*)$", substitution: "/\\1"}}
      directResponse: {status: 404, body: not found}
    - match: {path: {prefix: /}}
      backends: [{upstream: u1, weight: 1}]
      timeout: 0s
`)

	bad := []struct{ name, doc, substr string }{
		{"no listeners", `
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: r}
spec:
  rules:
    - match: {path: {prefix: /}}
      backends: [{upstream: u}]
`, `at least one listener`},
		{"path match none", `
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: r}
spec:
  listeners: [http]
  rules:
    - match: {path: {}}
      backends: [{upstream: u}]
`, `exactly one of exact | prefix | regex`},
		{"path match two", `
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: r}
spec:
  listeners: [http]
  rules:
    - match: {path: {exact: /a, prefix: /a}}
      backends: [{upstream: u}]
`, `exactly one of exact | prefix | regex`},
		{"header match two forms", `
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: r}
spec:
  listeners: [http]
  rules:
    - match: {path: {prefix: /}, headers: [{name: h, exact: a, present: true}]}
      backends: [{upstream: u}]
`, `exactly one of exact | regex | present`},
		{"rule no action", `
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: r}
spec:
  listeners: [http]
  rules:
    - match: {path: {prefix: /}}
`, `exactly one of backends | redirect | directResponse`},
		{"rule two actions", `
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: r}
spec:
  listeners: [http]
  rules:
    - match: {path: {prefix: /}}
      backends: [{upstream: u}]
      redirect: {scheme: https, code: 301}
`, `exactly one of backends | redirect | directResponse`},
		{"bad retry on", `
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: r}
spec:
  listeners: [http]
  rules:
    - match: {path: {prefix: /}}
      backends: [{upstream: u}]
      retry: {attempts: 2, on: [5xx, bogus]}
`, `invalid value "bogus"`},
		{"rewrite two forms", `
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: r}
spec:
  listeners: [http]
  rules:
    - match: {path: {prefix: /}}
      rewrite: {pathPrefix: /, path: /fixed}
      backends: [{upstream: u}]
`, `mutually exclusive`},
		{"duplicate rule name", `
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: r}
spec:
  listeners: [http]
  rules:
    - {name: login, match: {path: {prefix: /a}}, backends: [{upstream: u}]}
    - {name: login, match: {path: {prefix: /b}}, backends: [{upstream: u}]}
`, `duplicate rule name "login"`},
		{"invalid rule name", `
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: r}
spec:
  listeners: [http]
  rules:
    - {name: "Login_Bad", match: {path: {prefix: /a}}, backends: [{upstream: u}]}
`, `invalid name`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) { expectLoadErr(t, tc.doc, tc.substr) })
	}
}

func TestUpstreamValidate(t *testing.T) {
	expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: u}
spec:
  endpoints: [{address: 10.0.0.1, port: 8080, weight: 2}]
  loadBalancer: {policy: ringHash, hashOn: [{header: x-user-id}]}
  tls: {enabled: true, sni: svc.internal, insecureSkipVerify: false}
  connection: {connectTimeout: 3s, http2: true, maxConnections: 2147483647, maxPendingRequests: 1}
`)
	expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: u}
spec:
  kubernetesService: {namespace: default, name: svc, port: 80}
`)
	expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: u}
spec:
  dns: {hostname: svc.internal, port: 80, resolution: strict}
  healthCheck: {tcp: {}, interval: 5s}
`)

	bad := []struct{ name, doc, substr string }{
		{"no endpoint source", `
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: u}
spec: {}
`, `exactly one endpoint source`},
		{"two endpoint sources", `
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: u}
spec:
  endpoints: [{address: 10.0.0.1, port: 80}]
  dns: {hostname: h, port: 80}
`, `exactly one endpoint source`},
		{"bad lb policy", `
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: u}
spec:
  endpoints: [{address: 10.0.0.1, port: 80}]
  loadBalancer: {policy: weighted}
`, `invalid value "weighted"`},
		{"bad resolution", `
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: u}
spec:
  dns: {hostname: h, port: 80, resolution: cached}
`, `invalid value "cached"`},
		{"healthCheck both probes", `
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: u}
spec:
  endpoints: [{address: 10.0.0.1, port: 80}]
  healthCheck: {http: {path: /h}, tcp: {}}
`, `exactly one of http | tcp`},
		{"bad endpoint port", `
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: u}
spec:
  endpoints: [{address: 10.0.0.1, port: 0}]
`, `invalid port`},
		{"zero max connections", `
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: u}
spec:
  endpoints: [{address: 10.0.0.1, port: 80}]
  connection: {maxConnections: 0}
`, `spec.connection.maxConnections: must be between 1 and 2147483647`},
		{"negative max pending requests", `
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: u}
spec:
  endpoints: [{address: 10.0.0.1, port: 80}]
  connection: {maxPendingRequests: -1}
`, `spec.connection.maxPendingRequests: must be between 1 and 2147483647`},
		{"max connections integer overflow", `
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: u}
spec:
  endpoints: [{address: 10.0.0.1, port: 80}]
  connection: {maxConnections: 2147483648}
`, `cannot unmarshal number 2147483648`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) { expectLoadErr(t, tc.doc, tc.substr) })
	}
}

func TestPolicyValidate(t *testing.T) {
	expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  rateLimit: {requests: 100, unit: minute, burst: 200, key: "header:x-tenant"}
`)
	expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  headerModifier:
    request: {set: {x-a: "1"}, add: {x-b: "2"}, remove: [x-c]}
    response: {set: {x-frame-options: DENY}}
`)
	expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  extAuth: {disabled: true}
`)
	expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  extAuth: {grpc: {address: "[::1]:9000"}, failOpen: true}
`)
	expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  extAuth: {http: {address: "https://auth.example.com:8443", pathPrefix: /check, caFile: /etc/ssl/auth-ca.pem}}
`)
	expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  ipAccess: {allow: [10.0.0.0/8], deny: [192.168.0.1/32]}
`)
	expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  basicAuth: {users: /etc/esgw/htpasswd}
`)

	bad := []struct{ name, doc, substr string }{
		{"no type key", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec: {}
`, `exactly one policy type key`},
		{"two type keys", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  cors: {allowOrigins: ["*"]}
  rateLimit: {requests: 1, unit: second}
`, `exactly one policy type key`},
		{"bad rateLimit unit", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  rateLimit: {requests: 1, unit: day}
`, `invalid value "day"`},
		{"bad rateLimit burst", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  rateLimit: {requests: 1, unit: second, burst: 0}
`, `rateLimit.burst: must be > 0`},
		{"bad rateLimit key", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  rateLimit: {requests: 1, unit: second, key: remoteAddr}
`, `invalid value "remoteAddr"`},
		{"empty rateLimit header key", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  rateLimit: {requests: 1, unit: second, key: "header:"}
`, `header:<valid HTTP field name>`},
		{"invalid rateLimit header name", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  rateLimit: {requests: 1, unit: second, key: "header:x tenant"}
`, `header:<valid HTTP field name>`},
		{"bad rateLimit maxKeys", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  rateLimit: {requests: 1, unit: second, maxKeys: 0}
`, `maxKeys: must be between 1 and 100000`},
		{"jwks uri and file", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  jwt: {issuer: i, jwks: {uri: u, file: f}}
`, `exactly one of uri | file`},
		{"extAuth no backend", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  extAuth: {failOpen: true}
`, `one of grpc | http is required`},
		{"extAuth both backends", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  extAuth: {grpc: {address: a}, http: {address: b}}
`, `mutually exclusive`},
		{"extAuth disabled with backend", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  extAuth: {disabled: true, grpc: {address: "127.0.0.1:9000"}}
`, `disabled cannot be combined`},
		{"extAuth grpc missing port", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  extAuth: {grpc: {address: auth.internal}}
`, `invalid host:port`},
		{"extAuth http bad scheme", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  extAuth: {http: {address: "grpc://auth.internal:9000"}}
`, `invalid HTTP(S) URL`},
		{"extAuth http embedded path", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  extAuth: {http: {address: "http://auth.internal:9000/check"}}
`, `path, query and fragment are not allowed`},
		{"extAuth http pathPrefix", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  extAuth: {http: {address: "http://auth.internal:9000", pathPrefix: check}}
`, `must start with /`},
		{"extAuth https missing trust", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  extAuth: {http: {address: "https://auth.internal:9443"}}
`, `caFile is required`},
		{"extAuth http with TLS fields", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  extAuth: {http: {address: "http://auth.internal:9000", insecureSkipVerify: true}}
`, `only allowed for HTTPS`},
		{"ipAccess bad CIDR", `
apiVersion: esgw/v1alpha1
kind: Policy
metadata: {name: p}
spec:
  ipAccess: {allow: [10.0.0.0/33]}
`, `invalid CIDR`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) { expectLoadErr(t, tc.doc, tc.substr) })
	}
}

func TestPolicyAttachmentUnion(t *testing.T) {
	cs := expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Gateway
metadata: {name: default}
spec:
  policies:
    - ref-policy
    - cors: {allowOrigins: ["*"]}
`)
	if len(cs.Gateway.Spec.Policies) != 2 {
		t.Fatalf("policies: %+v", cs.Gateway.Spec.Policies)
	}
	if cs.Gateway.Spec.Policies[0].Ref != "ref-policy" || cs.Gateway.Spec.Policies[0].Inline != nil {
		t.Fatalf("attachment[0]: %+v", cs.Gateway.Spec.Policies[0])
	}
	if cs.Gateway.Spec.Policies[1].Inline == nil || cs.Gateway.Spec.Policies[1].Inline.CORS == nil {
		t.Fatalf("attachment[1]: %+v", cs.Gateway.Spec.Policies[1])
	}

	// 内联对象中的未知字段必须报错（strict decode 穿透 union）。
	expectLoadErr(t, `
apiVersion: esgw/v1alpha1
kind: Gateway
metadata: {name: default}
spec:
  policies:
    - cors: {allowOrigins: ["*"], bogus: 1}
`, `unknown field "bogus"`)

	// 内联对象同样过结构校验（零类型键）。
	expectLoadErr(t, `
apiVersion: esgw/v1alpha1
kind: Gateway
metadata: {name: default}
spec:
  policies:
    - {}
`, `exactly one policy type key`)

	// 非法形态（数字）。
	expectLoadErr(t, `
apiVersion: esgw/v1alpha1
kind: Gateway
metadata: {name: default}
spec:
  policies: [42]
`, `policy attachment`)

	// 引用名非法。
	expectLoadErr(t, `
apiVersion: esgw/v1alpha1
kind: Gateway
metadata: {name: default}
spec:
  policies: [Bad_Name]
`, `invalid name`)

	// accessLog format 坏枚举。
	expectLoadErr(t, `
apiVersion: esgw/v1alpha1
kind: Gateway
metadata: {name: default}
spec:
  accessLog: {enabled: true, format: yaml}
`, `invalid value "yaml"`)
}

func TestEnvoyResourcesValidate(t *testing.T) {
	cs := expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: EnvoyResources
metadata: {name: custom-lua}
spec:
  resources:
    - "@type": type.googleapis.com/envoy.config.cluster.v3.Cluster
      name: legacy-cluster
      connect_timeout: 3s
      type: STRICT_DNS
  allowOverride: true
`)
	if len(cs.EnvoyResources) != 1 || !cs.EnvoyResources[0].Spec.AllowOverride {
		t.Fatalf("envoyResources: %+v", cs.EnvoyResources[0].Spec)
	}
	if !strings.Contains(string(cs.EnvoyResources[0].Spec.Resources[0]), "legacy-cluster") {
		t.Fatalf("resource[0]: %s", cs.EnvoyResources[0].Spec.Resources[0])
	}

	expectLoadErr(t, `
apiVersion: esgw/v1alpha1
kind: EnvoyResources
metadata: {name: x}
spec:
  resources:
    - name: no-at-type
`, `"@type"`)
}

func TestUnknownFieldInNestedSpec(t *testing.T) {
	expectLoadErr(t, `
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: u}
spec:
  endpoints: [{address: 10.0.0.1, port: 80}]
  loadBalancer: {policy: roundRobin, typo: 1}
`, `unknown field "typo"`)
}
