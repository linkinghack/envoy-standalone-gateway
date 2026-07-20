package deliver

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
)

// SnapshotJSON 把 IR 五类资源序列化为单个 JSON 文档（esgw compile --mode xds
// 的产物形态，M0 裁量，S2 ADS server 消费时可调整）。纯函数：无 IO。
//
// 形态：
//
//	{
//	  "version":   <IR.Version>,
//	  "listeners": {<name>: <protojson>, ...},
//	  "clusters":  {...},
//	  "routes":    {...},
//	  "endpoints": {...},
//	  "secrets":   {...}
//	}
//
// 每类资源是以资源名为键的 map（与 go-control-plane Snapshot 的装配输入
// 一一对应）；protojson 用 UseProtoNames（snake_case）。确定性（SD6）：
// encoding/json 对 map 键字典序输出 + 固定两空格缩进，同一 IR 两次调用
// 字节级相同。Bootstrap 不在 snapshot 中（接入 bootstrap 是独立产物，
// 下发层 §2.7）。
func SnapshotJSON(i *ir.IR) ([]byte, error) {
	if i == nil {
		return nil, fmt.Errorf("snapshot: nil IR")
	}
	doc := map[string]any{
		"version":   i.Version,
		"listeners": map[string]any{},
		"clusters":  map[string]any{},
		"routes":    map[string]any{},
		"endpoints": map[string]any{},
		"secrets":   map[string]any{},
	}
	put := func(section string, name string, m proto.Message) error {
		v, err := protoToMap(m)
		if err != nil {
			return fmt.Errorf("snapshot: %s/%s: %w", section, name, err)
		}
		doc[section].(map[string]any)[name] = v
		return nil
	}
	for name, m := range i.Listeners {
		if err := put("listeners", name, m); err != nil {
			return nil, err
		}
	}
	for name, m := range i.Clusters {
		if err := put("clusters", name, m); err != nil {
			return nil, err
		}
	}
	for name, m := range i.Routes {
		if err := put("routes", name, m); err != nil {
			return nil, err
		}
	}
	for name, m := range i.Endpoints {
		if err := put("endpoints", name, m); err != nil {
			return nil, err
		}
	}
	for name, m := range i.Secrets {
		if err := put("secrets", name, m); err != nil {
			return nil, err
		}
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	return append(b, '\n'), nil
}

// protoToMap 把 proto 消息经 protojson（UseProtoNames）转为 map[string]any。
func protoToMap(m proto.Message) (map[string]any, error) {
	b, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(m)
	if err != nil {
		return nil, err
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}
