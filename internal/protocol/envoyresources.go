package protocol

import (
	"encoding/json"
	"fmt"
)

// EnvoyResources 是顶层原生资源 escape hatch（协议 §7.2）：
// 整段内嵌原生 Envoy 资源，与编译产物合并。
type EnvoyResources struct {
	APIVersion string             `json:"apiVersion"`
	Kind       Kind               `json:"kind"`
	Metadata   ObjectMeta         `json:"metadata"`
	Spec       EnvoyResourcesSpec `json:"spec"`
	Origin     Origin             `json:"-"`
}

// EnvoyResourcesSpec 是 EnvoyResources 的 spec。
type EnvoyResourcesSpec struct {
	Resources     []RawJSON `json:"resources"`               // 每项是带 @type 的任意 JSON 原生 Envoy 资源
	AllowOverride bool      `json:"allowOverride,omitempty"` // 默认 false：与编译产物重名 = 编译错误；true = 替换
}

func (s *EnvoyResourcesSpec) validate() error {
	for i, res := range s.Resources {
		var head struct {
			Type string `json:"@type"`
		}
		if err := json.Unmarshal(res, &head); err != nil || head.Type == "" {
			return fmt.Errorf("spec.resources[%d]: each resource must be a JSON object carrying a non-empty \"@type\"", i)
		}
	}
	return nil
}
