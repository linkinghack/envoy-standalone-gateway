package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	"github.com/invopop/jsonschema"
)

// Policy 是可插拔策略对象（协议 §3.5），一个对象承载一种策略类型：
// spec 下有且仅有一个类型键。
type Policy struct {
	APIVersion string     `json:"apiVersion"`
	Kind       Kind       `json:"kind"`
	Metadata   ObjectMeta `json:"metadata"`
	Spec       PolicySpec `json:"spec"`
	Origin     Origin     `json:"-"`
}

// PolicySpec 是 Policy 的 spec，也是内联匿名策略的形态（协议 §3.5 规则 2）。
// 有且仅有一个类型键。
type PolicySpec struct {
	HeaderModifier *HeaderModifierPolicy `json:"headerModifier,omitempty"`
	CORS           *CORSPolicy           `json:"cors,omitempty"`
	RateLimit      *RateLimitPolicy      `json:"rateLimit,omitempty"`
	JWT            *JWTPolicy            `json:"jwt,omitempty"`
	ExtAuth        *ExtAuthPolicy        `json:"extAuth,omitempty"`
	IPAccess       *IPAccessPolicy       `json:"ipAccess,omitempty"`
	BasicAuth      *BasicAuthPolicy      `json:"basicAuth,omitempty"`
}

// HeaderModifierPolicy 是请求/响应头增删改（P0）。
type HeaderModifierPolicy struct {
	Request  *HeaderOps `json:"request,omitempty"`
	Response *HeaderOps `json:"response,omitempty"`
}

// HeaderOps 是一组头操作。
type HeaderOps struct {
	Set    map[string]string `json:"set,omitempty"`
	Add    map[string]string `json:"add,omitempty"`
	Remove []string          `json:"remove,omitempty"`
}

// CORSPolicy 是 CORS 策略（P1）。CORS 必须先于鉴权类策略执行（协议 §3.5 规则 4）。
type CORSPolicy struct {
	AllowOrigins     []string  `json:"allowOrigins,omitempty"` // 支持通配
	AllowMethods     []string  `json:"allowMethods,omitempty"`
	AllowHeaders     []string  `json:"allowHeaders,omitempty"`
	AllowCredentials bool      `json:"allowCredentials,omitempty"`
	MaxAge           *Duration `json:"maxAge,omitempty"`
}

// RateLimitPolicy 是本地令牌桶限流（P1，FR-1.3）。分布式限流为后续版本。
type RateLimitPolicy struct {
	Requests int32         `json:"requests"`
	Unit     RateLimitUnit `json:"unit"`
	Burst    *int32        `json:"burst,omitempty"` // 默认 = requests
	Key      string        `json:"key,omitempty"`   // clientIP(默认) | header:<name> —— 限流维度
}

// JWTPolicy 是 JWT 校验（P1）。
type JWTPolicy struct {
	Issuer               string   `json:"issuer,omitempty"`
	Audiences            []string `json:"audiences,omitempty"`
	JWKS                 *JWKS    `json:"jwks,omitempty"`
	ForwardPayloadHeader string   `json:"forwardPayloadHeader,omitempty"`
	Optional             bool     `json:"optional,omitempty"` // 局部关闭：就近覆盖实现免鉴权（协议 §3.5 规则 3a）
}

// JWKS 是 JWT 密钥来源，uri | file 二选一。
type JWKS struct {
	URI  string `json:"uri,omitempty"`
	File string `json:"file,omitempty"`
}

// ExtAuthPolicy 是外部鉴权（P1）。
type ExtAuthPolicy struct {
	GRPC     *ExtAuthGRPC `json:"grpc,omitempty"`
	HTTP     *ExtAuthHTTP `json:"http,omitempty"`
	FailOpen bool         `json:"failOpen,omitempty"`
	Disabled bool         `json:"disabled,omitempty"` // 局部关闭（协议 §3.5 规则 3a），映射 Envoy ExtAuthzPerRoute.disabled
}

// ExtAuthGRPC 是 gRPC 外部鉴权服务。
type ExtAuthGRPC struct {
	Address string `json:"address"`
}

// ExtAuthHTTP 是 HTTP 外部鉴权服务。
type ExtAuthHTTP struct {
	Address    string `json:"address"`
	PathPrefix string `json:"pathPrefix,omitempty"`
}

// IPAccessPolicy 是 IP 黑白名单（P1）。
type IPAccessPolicy struct {
	Allow []string `json:"allow,omitempty"` // CIDR 列表
	Deny  []string `json:"deny,omitempty"`  // CIDR 列表
}

// BasicAuthPolicy 是 Basic 认证（P2）。Users 为 htpasswd 文件路径或内联 htpasswd 内容。
type BasicAuthPolicy struct {
	Users string `json:"users"`
}

// PolicyAttachment 是 policies 列表元素（协议 §3.5 规则 2），union：
// 字符串（引用 Policy 名）或内联匿名对象（形态同 Policy.spec）。
type PolicyAttachment struct {
	Ref    string      `json:"-"`
	Inline *PolicySpec `json:"-"`
}

// UnmarshalJSON 实现 json.Unmarshaler：字符串 → 引用；对象 → 内联 spec（strict decode）。
func (p *PolicyAttachment) UnmarshalJSON(b []byte) error {
	var ref string
	if err := json.Unmarshal(b, &ref); err == nil {
		p.Ref = ref
		p.Inline = nil
		return nil
	}
	var spec PolicySpec
	if err := decodeStrict(bytes.NewReader(b), &spec); err != nil {
		return fmt.Errorf("policy attachment must be a name string or an inline policy spec object: %w", err)
	}
	p.Ref = ""
	p.Inline = &spec
	return nil
}

// MarshalJSON 实现 json.Marshaler。
func (p PolicyAttachment) MarshalJSON() ([]byte, error) {
	if p.Inline != nil {
		return json.Marshal(p.Inline)
	}
	return json.Marshal(p.Ref)
}

// JSONSchema 实现自定义 schema 钩子：名称字符串 或 PolicySpec 对象。
func (PolicyAttachment) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		OneOf: []*jsonschema.Schema{
			{Type: "string", Pattern: NamePattern, Description: "引用 Policy 名"},
			{Ref: "#/$defs/PolicySpec"},
		},
	}
}

func (s *PolicySpec) validate(field string) error {
	types := 0
	for _, set := range []bool{
		s.HeaderModifier != nil, s.CORS != nil, s.RateLimit != nil,
		s.JWT != nil, s.ExtAuth != nil, s.IPAccess != nil, s.BasicAuth != nil,
	} {
		if set {
			types++
		}
	}
	if types != 1 {
		return fmt.Errorf("%s: exactly one policy type key (headerModifier | cors | rateLimit | jwt | extAuth | ipAccess | basicAuth) is required, got %d", field, types)
	}
	if r := s.RateLimit; r != nil {
		if r.Requests <= 0 {
			return fmt.Errorf("%s.rateLimit.requests: must be > 0", field)
		}
		if !r.Unit.Valid() {
			return fmt.Errorf("%s.rateLimit.unit: invalid value %q (want second | minute | hour)", field, r.Unit)
		}
		if r.Key != "" && r.Key != RateLimitKeyClientIP && !strings.HasPrefix(r.Key, RateLimitKeyHeaderPrefix) {
			return fmt.Errorf("%s.rateLimit.key: invalid value %q (want clientIP | header:<name>)", field, r.Key)
		}
	}
	if j := s.JWT; j != nil && j.JWKS != nil {
		if (j.JWKS.URI == "") == (j.JWKS.File == "") {
			return fmt.Errorf("%s.jwt.jwks: exactly one of uri | file is required", field)
		}
	}
	if e := s.ExtAuth; e != nil {
		switch {
		case e.GRPC != nil && e.HTTP != nil:
			return fmt.Errorf("%s.extAuth: grpc and http are mutually exclusive", field)
		case e.GRPC == nil && e.HTTP == nil && !e.Disabled:
			return fmt.Errorf("%s.extAuth: one of grpc | http is required (unless disabled: true)", field)
		}
	}
	if ip := s.IPAccess; ip != nil {
		for i, cidr := range ip.Allow {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				return fmt.Errorf("%s.ipAccess.allow[%d]: invalid CIDR %q", field, i, cidr)
			}
		}
		for i, cidr := range ip.Deny {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				return fmt.Errorf("%s.ipAccess.deny[%d]: invalid CIDR %q", field, i, cidr)
			}
		}
	}
	if b := s.BasicAuth; b != nil && b.Users == "" {
		return fmt.Errorf("%s.basicAuth.users: is required", field)
	}
	return nil
}

func validatePolicyAttachments(field string, policies []PolicyAttachment) error {
	for i := range policies {
		p := &policies[i]
		f := fmt.Sprintf("%s[%d]", field, i)
		switch {
		case p.Inline != nil:
			if err := p.Inline.validate(f); err != nil {
				return err
			}
		case p.Ref != "":
			if err := ValidateName(p.Ref); err != nil {
				return fmt.Errorf("%s: %w", f, err)
			}
		default:
			return fmt.Errorf("%s: empty policy attachment", f)
		}
	}
	return nil
}
