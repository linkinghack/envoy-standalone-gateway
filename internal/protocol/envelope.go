package protocol

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// APIVersionV1Alpha1 是协议 v0 唯一接受的 apiVersion（协议 §6）。
// 多版本共存与版本转换层在后续版本实现（load.go 留有挂载点）。
const APIVersionV1Alpha1 = "esgw/v1alpha1"

// NamePattern 是 metadata.name 的合法形态（协议 §2.1）。
const NamePattern = `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`

var nameRE = regexp.MustCompile(NamePattern)

// Kind 是协议对象类别（协议 §2.1）。
type Kind string

// 协议 v0 的全部对象类别。
const (
	KindGateway        Kind = "Gateway"
	KindListener       Kind = "Listener"
	KindRoute          Kind = "Route"
	KindUpstream       Kind = "Upstream"
	KindPolicy         Kind = "Policy"
	KindEnvoyResources Kind = "EnvoyResources"
)

// Valid 报告 k 是否为协议 v0 已知的对象类别。
func (k Kind) Valid() bool {
	switch k {
	case KindGateway, KindListener, KindRoute, KindUpstream, KindPolicy, KindEnvoyResources:
		return true
	}
	return false
}

// Envelope 是所有协议对象的统一信封（协议 §2.1）。
// Spec 保持原始 JSON，由加载器按 Kind 分发到具体 spec 类型。
type Envelope struct {
	APIVersion string          `json:"apiVersion"`
	Kind       Kind            `json:"kind"`
	Metadata   ObjectMeta      `json:"metadata"`
	Spec       json.RawMessage `json:"spec,omitempty"`
}

// ObjectMeta 是对象元数据。Labels 仅供 UI 分组/筛选，编译不消费（协议 §2.1）。
type ObjectMeta struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
}

// Origin 记录对象来源：文件路径 + 文件内第几个 YAML 文档（0 起）。
type Origin struct {
	File     string
	DocIndex int
}

// String 返回 Origin 的可读形式。
func (o Origin) String() string {
	return fmt.Sprintf("%s doc[%d]", o.File, o.DocIndex)
}

// ValidateName 校验 metadata.name 是否满足 NamePattern。
func ValidateName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("invalid name %q: must match %s", name, NamePattern)
	}
	return nil
}
