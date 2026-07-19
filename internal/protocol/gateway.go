package protocol

import "fmt"

// Gateway 是实例级全局设置（协议 §3.1）。v0 只有一个隐式实例：
// 对象可省略（全部取默认值）；显式声明时 metadata.name 必须为 "default"。
type Gateway struct {
	APIVersion string      `json:"apiVersion"`
	Kind       Kind        `json:"kind"`
	Metadata   ObjectMeta  `json:"metadata"`
	Spec       GatewaySpec `json:"spec"`
	Origin     Origin      `json:"-"`
}

// GatewaySpec 是 Gateway 的 spec。
type GatewaySpec struct {
	AccessLog  *AccessLog         `json:"accessLog,omitempty"`
	HTTP       *HTTPDefaults      `json:"http,omitempty"`
	Policies   []PolicyAttachment `json:"policies,omitempty"`   // 全局策略引用，作用于所有 HTTP Listener
	EnvoyPatch []EnvoyPatch       `json:"envoyPatch,omitempty"` // escape hatch（协议 §7.1）
}

// AccessLog 是访问日志配置。
type AccessLog struct {
	Enabled bool            `json:"enabled,omitempty"`
	Format  AccessLogFormat `json:"format,omitempty"` // json | text；自定义格式模板 P1
	Path    string          `json:"path,omitempty"`   // 省略 = stdout
}

// HTTPDefaults 是所有 HTTP 类 Listener 的默认值，Listener 可覆盖（协议 §3.1）。
type HTTPDefaults struct {
	IdleTimeout         *Duration `json:"idleTimeout,omitempty"`         // 默认 60s
	MaxRequestHeadersKB *int32    `json:"maxRequestHeadersKb,omitempty"` // 默认 60
	ServerHeader        *string   `json:"serverHeader,omitempty"`        // 默认 "esgw"；"" 表示透传上游
}

func (s *GatewaySpec) validate() error {
	if s.AccessLog != nil && s.AccessLog.Format != "" && !s.AccessLog.Format.Valid() {
		return fmt.Errorf("spec.accessLog.format: invalid value %q (want json | text)", s.AccessLog.Format)
	}
	if err := validatePolicyAttachments("spec.policies", s.Policies); err != nil {
		return err
	}
	return validateEnvoyPatches("spec.envoyPatch", s.EnvoyPatch)
}
