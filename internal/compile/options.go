package compile

import (
	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// Mode 是下发模式（编译层 §3 阶段 F5：形态化按目标模式决定引用还是内联）。
type Mode string

// 下发模式取值。
const (
	ModeXDS    Mode = "xds"    // Listener 引 RDS/SDS、Cluster 引 EDS
	ModeStatic Mode = "static" // 同一批资源内联化，渲染为单文件 YAML
)

// EndpointSource 提供 k8s Service 的端点（M-DISCO 实现；M0 恒 nil）。
// 编译层对 client-go 无依赖（架构 A4），k8s 动态端点经此接口注入。
// EndpointSource 为 nil 时，配置中出现 kubernetesService 即编译错误。
type EndpointSource interface {
	// InitialEndpoints 返回指定 k8s Service 的初始端点快照。
	// 运行期端点变化由 M-DISCO 经 EDS 通道更新，不经过 Compile。
	InitialEndpoints(svc protocol.KubernetesServiceSource) ([]protocol.Endpoint, error)
}

// EnvoyValidateOpts 配置 F7 `envoy --mode validate` 终检；nil = 跳过 F7。
type EnvoyValidateOpts struct {
	BinPath string // envoy 二进制路径；空 = PATH 中的 "envoy"
}

// Options 是 Compile 的入参（编译层 §6 公共 API，technical_design §3 冻结签名）。
type Options struct {
	Mode           Mode               // ModeXDS | ModeStatic
	EndpointSource EndpointSource     // k8s 端点注入接口，可 nil（M0 恒 nil）
	EnvoyValidate  *EnvoyValidateOpts // nil = 跳过 F7
}
