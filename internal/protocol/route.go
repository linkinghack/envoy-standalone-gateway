package protocol

import "fmt"

// Route 是匹配与转发规则（协议 §3.3）。rules 与 forward（L4 形态，§3.3.5）互斥，
// 互斥校验属编译层（F2）；本包只做结构层解析。
type Route struct {
	APIVersion string     `json:"apiVersion"`
	Kind       Kind       `json:"kind"`
	Metadata   ObjectMeta `json:"metadata"`
	Spec       RouteSpec  `json:"spec"`
	Origin     Origin     `json:"-"`
}

// RouteSpec 是 Route 的 spec。
type RouteSpec struct {
	Listeners  []string           `json:"listeners"`            // 必填，挂接目标 Listener 名
	Hostnames  []string           `json:"hostnames,omitempty"`  // 域名匹配，支持 "*.example.com" 前缀通配；省略 = 兜底 "*"
	Policies   []PolicyAttachment `json:"policies,omitempty"`   // Route 级策略，作用于全部 rules
	Rules      []Rule             `json:"rules,omitempty"`      // 顺序即优先级：自上而下首个 match 命中生效（P4）
	Forward    *Forward           `json:"forward,omitempty"`    // L4 形态（§3.3.5），与 rules 互斥
	EnvoyPatch []EnvoyPatch       `json:"envoyPatch,omitempty"` // escape hatch（协议 §7.1）
}

// Forward 是 L4 路由形态（protocol: TCP/TLS 的 Listener）。
type Forward struct {
	Upstream string   `json:"upstream"`
	SNIHosts []string `json:"sniHosts,omitempty"` // 仅 protocol: TLS 的 listener 可用
}

// Rule 是一条匹配与动作规则。动作三选一：backends / redirect / directResponse。
type Rule struct {
	Match          RuleMatch          `json:"match"`
	Rewrite        *Rewrite           `json:"rewrite,omitempty"`
	Backends       []BackendRef       `json:"backends,omitempty"`
	Redirect       *Redirect          `json:"redirect,omitempty"`
	DirectResponse *DirectResponse    `json:"directResponse,omitempty"`
	Timeout        *Duration          `json:"timeout,omitempty"` // 请求总超时；0s = 不限（默认 15s）
	Retry          *Retry             `json:"retry,omitempty"`
	Policies       []PolicyAttachment `json:"policies,omitempty"` // rule 级策略
}

// RuleMatch 是匹配条件；各字段之间为 AND。
type RuleMatch struct {
	Path        *PathMatch      `json:"path,omitempty"`
	Methods     []string        `json:"methods,omitempty"`
	Headers     []KeyValueMatch `json:"headers,omitempty"`     // 可选，全部条件 AND
	QueryParams []KeyValueMatch `json:"queryParams,omitempty"` // 可选，同 headers 形态
}

// PathMatch 是路径匹配，exact | prefix | regex 三选一。
type PathMatch struct {
	Exact  string `json:"exact,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	Regex  string `json:"regex,omitempty"`
}

// KeyValueMatch 是 header / queryParam 匹配，exact | regex | present 三选一。
type KeyValueMatch struct {
	Name    string `json:"name"`
	Exact   string `json:"exact,omitempty"`
	Regex   string `json:"regex,omitempty"`
	Present *bool  `json:"present,omitempty"`
}

// Rewrite 是可选改写（协议 §3.3）。
type Rewrite struct {
	PathPrefix string             `json:"pathPrefix,omitempty"` // 将命中的 prefix 替换为此值
	Path       string             `json:"path,omitempty"`       // 或整路径替换
	Regex      *RegexSubstitution `json:"regex,omitempty"`
	Host       string             `json:"host,omitempty"` // 改写发往上游的 Host
}

// RegexSubstitution 是正则整路径替换。
type RegexSubstitution struct {
	Pattern      string `json:"pattern"`
	Substitution string `json:"substitution"`
}

// BackendRef 引用一个 Upstream，可带权重做灰度分流。
type BackendRef struct {
	Upstream string `json:"upstream"`         // 引用 Upstream.name
	Weight   *int32 `json:"weight,omitempty"` // 权重分流（灰度）；单后端可省略
}

// Redirect 是重定向动作。
type Redirect struct {
	Scheme string `json:"scheme,omitempty"`
	Code   int32  `json:"code,omitempty"` // 如 301
}

// DirectResponse 是直接响应动作。
type DirectResponse struct {
	Status int32  `json:"status"`
	Body   string `json:"body,omitempty"`
}

// Retry 是重试策略；默认不重试（显式声明才开启，协议 §3.3 要点 3）。
type Retry struct {
	Attempts      int32     `json:"attempts"`
	PerTryTimeout *Duration `json:"perTryTimeout,omitempty"`
	On            []RetryOn `json:"on,omitempty"` // 枚举映射 Envoy retry_on
}

func (s *RouteSpec) validate() error {
	if len(s.Listeners) == 0 {
		return fmt.Errorf("spec.listeners: at least one listener is required")
	}
	if s.Forward != nil && s.Forward.Upstream == "" {
		return fmt.Errorf("spec.forward.upstream: is required")
	}
	if err := validatePolicyAttachments("spec.policies", s.Policies); err != nil {
		return err
	}
	for i := range s.Rules {
		if err := s.Rules[i].validate(fmt.Sprintf("spec.rules[%d]", i)); err != nil {
			return err
		}
	}
	return validateEnvoyPatches("spec.envoyPatch", s.EnvoyPatch)
}

func (r *Rule) validate(field string) error {
	if err := r.Match.validate(field + ".match"); err != nil {
		return err
	}
	actions := 0
	if len(r.Backends) > 0 {
		actions++
		for i, b := range r.Backends {
			if b.Upstream == "" {
				return fmt.Errorf("%s.backends[%d].upstream: is required", field, i)
			}
		}
	}
	if r.Redirect != nil {
		actions++
	}
	if r.DirectResponse != nil {
		actions++
	}
	if actions != 1 {
		return fmt.Errorf("%s: exactly one of backends | redirect | directResponse is required, got %d", field, actions)
	}
	if r.Rewrite != nil {
		forms := 0
		for _, set := range []bool{r.Rewrite.PathPrefix != "", r.Rewrite.Path != "", r.Rewrite.Regex != nil} {
			if set {
				forms++
			}
		}
		if forms > 1 {
			return fmt.Errorf("%s.rewrite: pathPrefix | path | regex are mutually exclusive", field)
		}
	}
	if r.Retry != nil {
		for i, on := range r.Retry.On {
			if !on.Valid() {
				return fmt.Errorf("%s.retry.on[%d]: invalid value %q (want 5xx | gateway-error | connect-failure | reset | retriable-4xx)", field, i, on)
			}
		}
	}
	return validatePolicyAttachments(field+".policies", r.Policies)
}

func (m *RuleMatch) validate(field string) error {
	if m.Path != nil {
		forms := 0
		for _, set := range []bool{m.Path.Exact != "", m.Path.Prefix != "", m.Path.Regex != ""} {
			if set {
				forms++
			}
		}
		if forms != 1 {
			return fmt.Errorf("%s.path: exactly one of exact | prefix | regex is required, got %d", field, forms)
		}
	}
	for i := range m.Headers {
		if err := m.Headers[i].validate(fmt.Sprintf("%s.headers[%d]", field, i)); err != nil {
			return err
		}
	}
	for i := range m.QueryParams {
		if err := m.QueryParams[i].validate(fmt.Sprintf("%s.queryParams[%d]", field, i)); err != nil {
			return err
		}
	}
	return nil
}

func (m *KeyValueMatch) validate(field string) error {
	if m.Name == "" {
		return fmt.Errorf("%s.name: is required", field)
	}
	forms := 0
	for _, set := range []bool{m.Exact != "", m.Regex != "", m.Present != nil} {
		if set {
			forms++
		}
	}
	if forms != 1 {
		return fmt.Errorf("%s: exactly one of exact | regex | present is required, got %d", field, forms)
	}
	return nil
}
