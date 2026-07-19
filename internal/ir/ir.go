package ir

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"

	bootstrapv3 "github.com/envoyproxy/go-control-plane/envoy/config/bootstrap/v3"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"google.golang.org/protobuf/proto"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// ResourceKind 是 IR 顶层资源的类别（SourceMap 键的组成部分）。
type ResourceKind string

// IR 顶层资源的全部类别。
const (
	ResourceListener  ResourceKind = "listener"  // IR.Listeners
	ResourceCluster   ResourceKind = "cluster"   // IR.Clusters
	ResourceRoute     ResourceKind = "route"     // IR.Routes（RDS 资源）
	ResourceEndpoints ResourceKind = "endpoints" // IR.Endpoints（EDS 资源）
	ResourceSecret    ResourceKind = "secret"    // IR.Secrets（SDS 资源）
	ResourceBootstrap ResourceKind = "bootstrap" // IR.Bootstrap（单例，Name 恒 "bootstrap"）
)

// ResourceKey 唯一标识一份 IR 顶层资源（类别 + 资源 name）。
type ResourceKey struct {
	Kind ResourceKind
	Name string
}

// String 返回 ResourceKey 的可读形式。
func (k ResourceKey) String() string { return string(k.Kind) + "/" + k.Name }

// SourceRef 把一份 Envoy 资源（或一条编译错误）回指到用户 YAML：
// 文件、对象（kind/name）与字段路径（如 spec.rules[2].retry.on）。
// UI 据此在 YAML 编辑器行内标注（编译层 §4，FR-4.2）。
type SourceRef struct {
	File string        // 源文件路径（空 = 隐式对象，如编译期补全的默认 Gateway）
	Kind protocol.Kind // 对象类别
	Name string        // metadata.name
	Path string        // YAML 字段路径；空 = 指整个对象
}

// String 返回 SourceRef 的可读形式。
func (r SourceRef) String() string {
	loc := r.File
	if loc == "" {
		loc = "<implicit>"
	}
	s := fmt.Sprintf("%s %s/%s", loc, r.Kind, r.Name)
	if r.Path != "" {
		s += ": " + r.Path
	}
	return s
}

// IR 是编译产物：一份完整、自洽、已通过校验的 Envoy v3 资源集合（编译层 §1）。
// 不自造中间模型；xDS 路线直接装入 Snapshot，static 路线渲染为单文件 YAML
// （两个输出都是 IR 的纯函数，属 M-DELIVER 职责）。
//
// 动静差异收敛在 IR 内部形态：xDS 模式下 Listener 引 RDS/SDS、STATIC Cluster 引 EDS；
// static 模式下 route_config 内嵌 HCM、证书走文件路径、端点内联（编译层 §3 F5）。
type IR struct {
	Listeners map[string]*listenerv3.Listener              // key = 资源 name（lis/<listener>）
	Clusters  map[string]*clusterv3.Cluster                // key = 资源 name（us/<upstream> 等）
	Routes    map[string]*routev3.RouteConfiguration       // RDS 资源（rc/<listener>）；static 模式内联后仅含 EnvoyResources 引入者
	Endpoints map[string]*endpointv3.ClusterLoadAssignment // EDS 资源；key = cluster name
	Secrets   map[string]*tlsv3.Secret                     // SDS 资源（crt/<listener>/<n> 等）
	Bootstrap *bootstrapv3.Bootstrap                       // 骨架（admin、node 等）

	Version   string                    // 内容哈希：全资源确定性序列化的 SHA-256 前 12 位（编译层 §5）
	SourceMap map[ResourceKey]SourceRef // Envoy 资源 → 协议对象溯源（排障/FR-2.3）
}

// MarshalDeterministic 产出 IR 全资源的确定性字节表示（编译层 §5）：
// 资源类别按固定顺序、类别内按 name 排序，逐资源
// proto.MarshalOptions{Deterministic: true} 序列化后拼接；
// 每段带类别/名/长度前缀，避免拼接歧义。同一 IR → 字节级相同（A6）。
func (i *IR) MarshalDeterministic() ([]byte, error) {
	var buf []byte
	write := func(kind ResourceKind, name string, m proto.Message) error {
		b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", ResourceKey{Kind: kind, Name: name}, err)
		}
		buf = append(buf, []byte(kind)...)
		buf = append(buf, 0)
		buf = append(buf, []byte(name)...)
		buf = append(buf, 0)
		var l [8]byte
		binary.BigEndian.PutUint64(l[:], uint64(len(b)))
		buf = append(buf, l[:]...)
		buf = append(buf, b...)
		return nil
	}
	sections := []struct {
		kind  ResourceKind
		names []string
		get   func(name string) proto.Message
	}{
		{ResourceListener, sortedKeys(i.Listeners), func(n string) proto.Message { return i.Listeners[n] }},
		{ResourceCluster, sortedKeys(i.Clusters), func(n string) proto.Message { return i.Clusters[n] }},
		{ResourceRoute, sortedKeys(i.Routes), func(n string) proto.Message { return i.Routes[n] }},
		{ResourceEndpoints, sortedKeys(i.Endpoints), func(n string) proto.Message { return i.Endpoints[n] }},
		{ResourceSecret, sortedKeys(i.Secrets), func(n string) proto.Message { return i.Secrets[n] }},
	}
	for _, s := range sections {
		for _, n := range s.names {
			if err := write(s.kind, n, s.get(n)); err != nil {
				return nil, err
			}
		}
	}
	if i.Bootstrap != nil {
		if err := write(ResourceBootstrap, "bootstrap", i.Bootstrap); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

// ComputeVersion 计算 IR.Version：全资源确定性序列化的 SHA-256 前 12 位 hex
// （编译层 §5；同时作为 Snapshot version 与 static 文件头注释）。
func (i *IR) ComputeVersion() (string, error) {
	b, err := i.MarshalDeterministic()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12], nil
}

// sortedKeys 返回 map 键的排序副本（map 输出按 name 排序，编译层 §5）。
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
