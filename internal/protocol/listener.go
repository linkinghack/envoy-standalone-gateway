package protocol

import "fmt"

// Listener 是一个入口 = 地址 + 端口 + 协议 +（可选）TLS（协议 §3.2）。
type Listener struct {
	APIVersion string       `json:"apiVersion"`
	Kind       Kind         `json:"kind"`
	Metadata   ObjectMeta   `json:"metadata"`
	Spec       ListenerSpec `json:"spec"`
	Origin     Origin       `json:"-"`
}

// ListenerSpec 是 Listener 的 spec。
type ListenerSpec struct {
	Address    string             `json:"address,omitempty" jsonschema:"minLength=1"` // 默认 0.0.0.0；支持 IPv6 "::"
	Port       int32              `json:"port" jsonschema:"minimum=1,maximum=65535"`
	Protocol   ListenerProtocol   `json:"protocol"`
	TLS        *ListenerTLS       `json:"tls,omitempty"` // protocol 为 HTTPS 时必填；TLS 表示不终止连接的 SNI passthrough
	HTTP       *ListenerHTTP      `json:"http,omitempty"`
	Policies   []PolicyAttachment `json:"policies,omitempty"`   // Listener 级策略引用
	EnvoyPatch []EnvoyPatch       `json:"envoyPatch,omitempty"` // escape hatch（协议 §7.1）
}

// ListenerTLS 是 HTTPS 的 TLS 终止配置。protocol: TLS 表示 TLS passthrough，
// 只读取 ClientHello SNI 且不持有证书，因此禁止配置本字段。
type ListenerTLS struct {
	Certificates []Certificate `json:"certificates"`         // 多证书 = SNI 多域名（FR-1.2）
	MinVersion   TLSVersion    `json:"minVersion,omitempty"` // 默认 "1.2"
	ALPN         []string      `json:"alpn,omitempty"`       // 默认 [h2, http/1.1]
	ClientCA     string        `json:"clientCA,omitempty"`   // 可选，启用 mTLS 客户端验证（P1）
}

// Certificate 是证书条目，oneOf：{certFile+keyFile} 或 {ref}（协议 §3.2）。
type Certificate struct {
	CertFile string `json:"certFile,omitempty"`
	KeyFile  string `json:"keyFile,omitempty"`
	Ref      string `json:"ref,omitempty"` // 引用管理面证书库中托管的证书
}

// ListenerHTTP 覆盖 Gateway.spec.http 同名字段（协议 §3.2）。
type ListenerHTTP struct {
	HTTP2 *bool `json:"http2,omitempty"` // 默认 true（HTTPS）；HTTP 明文默认 false
	HTTP3 bool  `json:"http3,omitempty"` // P2
}

func (s *ListenerSpec) validate() error {
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("spec.port: invalid port %d (want 1-65535)", s.Port)
	}
	if !s.Protocol.Valid() {
		return fmt.Errorf("spec.protocol: invalid value %q (want HTTP | HTTPS | TCP | TLS | UDP)", s.Protocol)
	}
	if s.TLS != nil {
		if s.TLS.MinVersion != "" && !s.TLS.MinVersion.Valid() {
			return fmt.Errorf("spec.tls.minVersion: invalid value %q (want \"1.2\" | \"1.3\")", s.TLS.MinVersion)
		}
		for i := range s.TLS.Certificates {
			if err := s.TLS.Certificates[i].validate(fmt.Sprintf("spec.tls.certificates[%d]", i)); err != nil {
				return err
			}
		}
	}
	if err := validatePolicyAttachments("spec.policies", s.Policies); err != nil {
		return err
	}
	return validateEnvoyPatches("spec.envoyPatch", s.EnvoyPatch)
}

func (c *Certificate) validate(field string) error {
	hasRef := c.Ref != ""
	hasFiles := c.CertFile != "" || c.KeyFile != ""
	switch {
	case hasRef && hasFiles:
		return fmt.Errorf("%s: certificate is oneOf {certFile+keyFile} or {ref}, got both", field)
	case hasRef:
		return nil
	case c.CertFile == "" || c.KeyFile == "":
		return fmt.Errorf("%s: certFile and keyFile must be set together", field)
	}
	return nil
}
