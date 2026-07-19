package compile

import (
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// Compile 是编译层公共入口（编译层 §6，technical_design §3 冻结签名）。
//
// 流水线：F1 结构校验（已由 protocol loader 完成）→ F2 链接与语义校验 →
// F3 构建 → F4 合成 → F5 形态化 → F6 资源校验（→ F7 envoy 终检）。
// 同阶段收集不中断、阶段间失败短路。F3+ 当前为占位（Sprint 260719 T4/T5 落地），
// F2 通过后返回一条 build 阶段的未实现错误。
//
// 注意：Compile 会调用 protocol.ApplyDefaults 原地填充 cs 的默认值（F2 职责）。
func Compile(cs *protocol.ConfigSet, opts Options) (*ir.IR, []CompileError) {
	_, errs := link(cs, opts, defaultCertVerifier())
	if hasErrors(errs) {
		return nil, errs // 阶段间短路：链接失败不进构建
	}
	// F3+ 占位：T4 实现 F3 构建，T5 实现 F4/F5/F6 与 IR。
	return nil, append(errs, CompileError{
		Stage:    StageBuild,
		Severity: SeverityError,
		Message:  "build stage (F3+) not yet implemented (Sprint 260719 T4)",
	})
}
