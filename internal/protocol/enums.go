package protocol

import "github.com/invopop/jsonschema"

// 协议 §4：枚举为 camelCase（Listener protocol 等按协议原文大写）字符串，编译期强校验。
// 每个枚举类型实现 Valid() 用于加载期校验，JSONSchema() 用于 schema 生成。

func enumSchema[T ~string](values ...T) *jsonschema.Schema {
	list := make([]any, len(values))
	for i, v := range values {
		list[i] = string(v)
	}
	return &jsonschema.Schema{Type: "string", Enum: list}
}

// AccessLogFormat 是 accessLog.format 的枚举（协议 §3.1）。
type AccessLogFormat string

// AccessLogFormat 的合法取值。
const (
	AccessLogFormatJSON AccessLogFormat = "json"
	AccessLogFormatText AccessLogFormat = "text"
)

// Valid 报告取值是否合法。
func (f AccessLogFormat) Valid() bool {
	return f == AccessLogFormatJSON || f == AccessLogFormatText
}

// JSONSchema 实现自定义 schema 钩子。
func (f AccessLogFormat) JSONSchema() *jsonschema.Schema {
	return enumSchema(AccessLogFormatJSON, AccessLogFormatText)
}

// ListenerProtocol 是 Listener.spec.protocol 的枚举（协议 §3.2）。
type ListenerProtocol string

// ListenerProtocol 的合法取值。
const (
	ProtocolHTTP  ListenerProtocol = "HTTP"
	ProtocolHTTPS ListenerProtocol = "HTTPS"
	ProtocolTCP   ListenerProtocol = "TCP"
	ProtocolTLS   ListenerProtocol = "TLS"
	ProtocolUDP   ListenerProtocol = "UDP"
)

// Valid 报告取值是否合法。
func (p ListenerProtocol) Valid() bool {
	switch p {
	case ProtocolHTTP, ProtocolHTTPS, ProtocolTCP, ProtocolTLS, ProtocolUDP:
		return true
	}
	return false
}

// JSONSchema 实现自定义 schema 钩子。
func (p ListenerProtocol) JSONSchema() *jsonschema.Schema {
	return enumSchema(ProtocolHTTP, ProtocolHTTPS, ProtocolTCP, ProtocolTLS, ProtocolUDP)
}

// TLSVersion 是 tls.minVersion 的枚举（协议 §3.2，默认 1.2）。
type TLSVersion string

// TLSVersion 的合法取值。
const (
	TLSVersion12 TLSVersion = "1.2"
	TLSVersion13 TLSVersion = "1.3"
)

// Valid 报告取值是否合法。
func (v TLSVersion) Valid() bool {
	return v == TLSVersion12 || v == TLSVersion13
}

// JSONSchema 实现自定义 schema 钩子。
func (v TLSVersion) JSONSchema() *jsonschema.Schema {
	return enumSchema(TLSVersion12, TLSVersion13)
}

// RetryOn 是 rule.retry.on 的枚举（协议 §3.3 要点 3）。
type RetryOn string

// RetryOn 的合法取值。
const (
	RetryOn5xx            RetryOn = "5xx"
	RetryOnGatewayError   RetryOn = "gateway-error"
	RetryOnConnectFailure RetryOn = "connect-failure"
	RetryOnReset          RetryOn = "reset"
	RetryOnRetriable4xx   RetryOn = "retriable-4xx"
)

// Valid 报告取值是否合法。
func (r RetryOn) Valid() bool {
	switch r {
	case RetryOn5xx, RetryOnGatewayError, RetryOnConnectFailure, RetryOnReset, RetryOnRetriable4xx:
		return true
	}
	return false
}

// JSONSchema 实现自定义 schema 钩子。
func (r RetryOn) JSONSchema() *jsonschema.Schema {
	return enumSchema(RetryOn5xx, RetryOnGatewayError, RetryOnConnectFailure, RetryOnReset, RetryOnRetriable4xx)
}

// LBPolicy 是 loadBalancer.policy 的枚举（协议 §3.4，默认 roundRobin）。
type LBPolicy string

// LBPolicy 的合法取值。
const (
	LBPolicyRoundRobin   LBPolicy = "roundRobin"
	LBPolicyLeastRequest LBPolicy = "leastRequest"
	LBPolicyRandom       LBPolicy = "random"
	LBPolicyRingHash     LBPolicy = "ringHash"
	LBPolicyMaglev       LBPolicy = "maglev"
)

// Valid 报告取值是否合法。
func (p LBPolicy) Valid() bool {
	switch p {
	case LBPolicyRoundRobin, LBPolicyLeastRequest, LBPolicyRandom, LBPolicyRingHash, LBPolicyMaglev:
		return true
	}
	return false
}

// JSONSchema 实现自定义 schema 钩子。
func (p LBPolicy) JSONSchema() *jsonschema.Schema {
	return enumSchema(LBPolicyRoundRobin, LBPolicyLeastRequest, LBPolicyRandom, LBPolicyRingHash, LBPolicyMaglev)
}

// DNSResolution 是 dns.resolution 的枚举（协议 §3.4，默认 logical）。
type DNSResolution string

// DNSResolution 的合法取值。
const (
	DNSResolutionLogical DNSResolution = "logical"
	DNSResolutionStrict  DNSResolution = "strict"
)

// Valid 报告取值是否合法。
func (r DNSResolution) Valid() bool {
	return r == DNSResolutionLogical || r == DNSResolutionStrict
}

// JSONSchema 实现自定义 schema 钩子。
func (r DNSResolution) JSONSchema() *jsonschema.Schema {
	return enumSchema(DNSResolutionLogical, DNSResolutionStrict)
}

// RateLimitUnit 是 rateLimit.unit 的枚举（协议 §3.5）。
type RateLimitUnit string

// RateLimitUnit 的合法取值。
const (
	RateLimitUnitSecond RateLimitUnit = "second"
	RateLimitUnitMinute RateLimitUnit = "minute"
	RateLimitUnitHour   RateLimitUnit = "hour"
)

// Valid 报告取值是否合法。
func (u RateLimitUnit) Valid() bool {
	return u == RateLimitUnitSecond || u == RateLimitUnitMinute || u == RateLimitUnitHour
}

// JSONSchema 实现自定义 schema 钩子。
func (u RateLimitUnit) JSONSchema() *jsonschema.Schema {
	return enumSchema(RateLimitUnitSecond, RateLimitUnitMinute, RateLimitUnitHour)
}

// RateLimitKeyClientIP 是 rateLimit.key 的默认限流维度（协议 §3.5）。
// 另一合法形态为 "header:<name>"，故 key 不是封闭枚举，校验见 rateLimit 的 validate。
const RateLimitKeyClientIP = "clientIP"

// RateLimitKeyHeaderPrefix 是 "header:<name>" 形态的前缀。
const RateLimitKeyHeaderPrefix = "header:"
