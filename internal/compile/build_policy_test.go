package compile

import (
	"testing"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// TestNormalizePolicies 穷举策略归一化语义（协议 §3.5 规则 3/3a，技术设计 §4 风险项）：
// 同类型就近覆盖整体替换（rule > Route > Listener > Gateway）、引用与内联等价、
// jwt.optional/extAuth.disabled 局部关闭、未实现类型显式标记。
func TestNormalizePolicies(t *testing.T) {
	corsSpec := func(origin string) protocol.PolicySpec {
		return protocol.PolicySpec{CORS: &protocol.CORSPolicy{AllowOrigins: []string{origin}}}
	}
	ref := func(name string) protocol.PolicyAttachment { return protocol.PolicyAttachment{Ref: name} }
	inline := func(s protocol.PolicySpec) protocol.PolicyAttachment { return protocol.PolicyAttachment{Inline: &s} }

	t.Run("four-level closest wins", func(t *testing.T) {
		lk := &linked{policies: map[string]*protocol.Policy{
			"cors-gw": newPolicy("cors-gw", corsSpec("gw")),
			"cors-l":  newPolicy("cors-l", corsSpec("lis")),
			"cors-r":  newPolicy("cors-r", corsSpec("route")),
			"cors-rl": newPolicy("cors-rl", corsSpec("rule")),
		}}
		eff := normalizePolicies(
			lk,
			[]protocol.PolicyAttachment{ref("cors-gw")},
			[]protocol.PolicyAttachment{ref("cors-l")},
			[]protocol.PolicyAttachment{ref("cors-r")},
			[]protocol.PolicyAttachment{ref("cors-rl")},
		)
		if eff.cors == nil || eff.cors.AllowOrigins[0] != "rule" {
			t.Fatalf("cors = %+v, want rule-level wins", eff.cors)
		}
	})

	t.Run("each closer level wins when present", func(t *testing.T) {
		lk := &linked{policies: map[string]*protocol.Policy{
			"cors-gw": newPolicy("cors-gw", corsSpec("gw")),
			"cors-l":  newPolicy("cors-l", corsSpec("lis")),
			"cors-r":  newPolicy("cors-r", corsSpec("route")),
		}}
		cases := []struct {
			name   string
			levels [][]protocol.PolicyAttachment
			want   string
		}{
			{"gateway only", [][]protocol.PolicyAttachment{{ref("cors-gw")}, nil, nil, nil}, "gw"},
			{"listener over gateway", [][]protocol.PolicyAttachment{{ref("cors-gw")}, {ref("cors-l")}, nil, nil}, "lis"},
			{"route over listener", [][]protocol.PolicyAttachment{{ref("cors-gw")}, {ref("cors-l")}, {ref("cors-r")}, nil}, "route"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				eff := normalizePolicies(lk, tc.levels...)
				if eff.cors == nil || eff.cors.AllowOrigins[0] != tc.want {
					t.Fatalf("cors = %+v, want origin %q", eff.cors, tc.want)
				}
			})
		}
	})

	t.Run("inline overrides ref at closer level", func(t *testing.T) {
		lk := &linked{policies: map[string]*protocol.Policy{
			"cors-r": newPolicy("cors-r", corsSpec("route")),
		}}
		eff := normalizePolicies(
			lk,
			nil, nil,
			[]protocol.PolicyAttachment{ref("cors-r")},
			[]protocol.PolicyAttachment{inline(corsSpec("inline"))},
		)
		if eff.cors == nil || eff.cors.AllowOrigins[0] != "inline" {
			t.Fatalf("cors = %+v, want inline wins over closer ref", eff.cors)
		}
	})

	t.Run("ref overrides inline at closer level", func(t *testing.T) {
		lk := &linked{policies: map[string]*protocol.Policy{
			"cors-rl": newPolicy("cors-rl", corsSpec("rule")),
		}}
		eff := normalizePolicies(
			lk,
			nil, nil,
			[]protocol.PolicyAttachment{inline(corsSpec("inline"))},
			[]protocol.PolicyAttachment{ref("cors-rl")},
		)
		if eff.cors == nil || eff.cors.AllowOrigins[0] != "rule" {
			t.Fatalf("cors = %+v, want rule-level ref wins", eff.cors)
		}
	})

	t.Run("whole replacement, no deep merge", func(t *testing.T) {
		lk := &linked{policies: map[string]*protocol.Policy{
			"cors-gw": newPolicy("cors-gw", protocol.PolicySpec{CORS: &protocol.CORSPolicy{
				AllowOrigins: []string{"gw"}, AllowMethods: []string{"GET"},
			}}),
			"cors-r": newPolicy("cors-r", corsSpec("route")),
		}}
		eff := normalizePolicies(
			lk,
			[]protocol.PolicyAttachment{ref("cors-gw")}, nil,
			[]protocol.PolicyAttachment{ref("cors-r")}, nil,
		)
		if len(eff.cors.AllowMethods) != 0 {
			t.Fatalf("AllowMethods = %v, want nil (整体替换不深合并)", eff.cors.AllowMethods)
		}
	})

	t.Run("jwt.optional local off", func(t *testing.T) {
		lk := &linked{policies: map[string]*protocol.Policy{
			"jwt-main": newPolicy("jwt-main", protocol.PolicySpec{JWT: &protocol.JWTPolicy{
				Issuer: "https://auth.example.com",
				JWKS:   &protocol.JWKS{URI: "https://auth.example.com/jwks.json"},
			}}),
			"jwt-off": newPolicy("jwt-off", protocol.PolicySpec{JWT: &protocol.JWTPolicy{Optional: true}}),
		}}
		eff := normalizePolicies(
			lk,
			nil, nil,
			[]protocol.PolicyAttachment{ref("jwt-main")},
			[]protocol.PolicyAttachment{ref("jwt-off")},
		)
		if eff.jwt == nil || !eff.jwt.Optional {
			t.Fatalf("jwt = %+v, want rule-level optional=true（局部关闭强制校验）", eff.jwt)
		}
	})

	t.Run("different types accumulate", func(t *testing.T) {
		lk := &linked{policies: map[string]*protocol.Policy{
			"cors-gw":      newPolicy("cors-gw", corsSpec("gw")),
			"ratelimit-rl": newPolicy("ratelimit-rl", rateLimitSpec()),
		}}
		eff := normalizePolicies(
			lk,
			[]protocol.PolicyAttachment{ref("cors-gw")}, nil, nil,
			[]protocol.PolicyAttachment{ref("ratelimit-rl")},
		)
		if eff.cors == nil || eff.rateLimit == nil {
			t.Fatalf("cors=%+v rateLimit=%+v, want both effective", eff.cors, eff.rateLimit)
		}
	})

	t.Run("dangling ref skipped", func(t *testing.T) {
		lk := &linked{policies: map[string]*protocol.Policy{}}
		eff := normalizePolicies(lk, []protocol.PolicyAttachment{ref("ghost")})
		if eff.cors != nil || eff.rateLimit != nil || eff.jwt != nil || eff.headerModifier != nil {
			t.Fatalf("eff = %+v, want empty", eff)
		}
	})

	t.Run("extAuth normalized", func(t *testing.T) {
		lk := &linked{policies: map[string]*protocol.Policy{
			"ext": newPolicy("ext", protocol.PolicySpec{ExtAuth: &protocol.ExtAuthPolicy{
				GRPC: &protocol.ExtAuthGRPC{Address: "127.0.0.1:9000"},
			}}),
		}}
		eff := normalizePolicies(lk, []protocol.PolicyAttachment{ref("ext")})
		if eff.extAuth == nil || eff.extAuth.GRPC == nil || len(eff.unsupported) != 0 {
			t.Fatalf("extAuth=%+v unsupported=%v", eff.extAuth, eff.unsupported)
		}
	})

	t.Run("unsupported types flagged", func(t *testing.T) {
		lk := &linked{policies: map[string]*protocol.Policy{
			"basic": newPolicy("basic", protocol.PolicySpec{BasicAuth: &protocol.BasicAuthPolicy{Users: "user:hash"}}),
		}}
		eff := normalizePolicies(lk, []protocol.PolicyAttachment{ref("basic")})
		if len(eff.unsupported) != 1 || eff.unsupported[0] != "basicAuth" {
			t.Fatalf("unsupported = %v, want [basicAuth]", eff.unsupported)
		}
	})
}

// TestJWTProviderKey 锁定 C4 决议的去重键：issuer + 规范化 jwks 来源；
// audiences 不参与去重。
func TestJWTProviderKey(t *testing.T) {
	a := &protocol.JWTPolicy{Issuer: "i1", Audiences: []string{"a"}, JWKS: &protocol.JWKS{URI: "https://x/jwks"}}
	b := &protocol.JWTPolicy{Issuer: "i1", Audiences: []string{"b"}, JWKS: &protocol.JWKS{URI: "https://x/jwks"}}
	if jwtProviderKey(a) != jwtProviderKey(b) {
		t.Fatal("same issuer+jwks with different audiences must share provider key")
	}
	c := &protocol.JWTPolicy{Issuer: "i1", JWKS: &protocol.JWKS{URI: "  https://x/jwks  "}}
	if jwtProviderKey(a) != jwtProviderKey(c) {
		t.Fatal("jwks uri 规范化（TrimSpace）后应同 key")
	}
	d := &protocol.JWTPolicy{Issuer: "i2", JWKS: &protocol.JWKS{URI: "https://x/jwks"}}
	if jwtProviderKey(a) == jwtProviderKey(d) {
		t.Fatal("different issuer must not share provider key")
	}
	e := &protocol.JWTPolicy{Issuer: "i1", JWKS: &protocol.JWKS{File: "/etc/jwks.json"}}
	if jwtProviderKey(a) == jwtProviderKey(e) {
		t.Fatal("uri vs file jwks must not share provider key")
	}
	// provider 名确定性：同 key（不同实例）同名，与收集顺序无关。
	if jwtProviderName(jwtProviderKey(a)) != jwtProviderName(jwtProviderKey(b)) {
		t.Fatal("provider name must be deterministic for the same key")
	}
	if jwtProviderName(jwtProviderKey(a)) == jwtProviderName(jwtProviderKey(d)) {
		t.Fatal("different keys must map to different provider names")
	}
}
