package compile

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// marshalAny 把 typed_config 消息打包进 Any，内部字节用确定性 marshal
// （proto.MarshalOptions{Deterministic: true}）。
//
// 不用 anypb.New 的原因：anypb.New 以默认（非确定性）选项序列化内层消息，
// proto map 字段（如 jwt_authn 的 requirement_map）在 wire 字节上顺序随机；
// Any.Value 是不透明字节，IR 外层哈希（ir.MarshalDeterministic）的确定性
// marshal 无法深入重排，导致同一 ConfigSet 两次编译 IR.Version 抖动
// （验收 A6 红线，Sprint 260719 T7 golden 测试实测复现：s2 的 jwt filter
// config 内 requirement_map 两键序翻转）。
func marshalAny(m proto.Message) (*anypb.Any, error) {
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		return nil, err
	}
	return &anypb.Any{
		TypeUrl: "type.googleapis.com/" + string(m.ProtoReflect().Descriptor().FullName()),
		Value:   b,
	}, nil
}
