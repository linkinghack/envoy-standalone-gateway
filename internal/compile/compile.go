package compile

import (
	"fmt"

	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// Compile 是编译层公共入口（编译层 §6，technical_design §3 冻结签名）。
//
// 流水线：F1 结构校验（已由 protocol loader 完成）→ F2 链接与语义校验 →
// F3 构建 → F4 合成（escape hatch）→ F5 形态化 → F6 资源校验（→ F7 envoy 终检，
// 由发布流调用，不在库内同步路径）。同阶段收集不中断、阶段间失败短路。
//
// Compile 保持纯函数：不 import envoycheck、除 F2/F3 注入的证书文件检查与
// SAN 提取外无 IO（SD5）；F4/F5 不再触碰文件系统。
//
// 注意：Compile 会调用 protocol.ApplyDefaults 原地填充 cs 的默认值（F2 职责）。
func Compile(cs *protocol.ConfigSet, opts Options) (*ir.IR, []CompileError) {
	lk, errs := link(cs, opts, defaultCertVerifier())
	if hasErrors(errs) {
		return nil, errs // 阶段间短路：链接失败不进构建
	}
	res, berrs := build(cs, lk, nil)
	errs = append(errs, berrs...)
	if hasErrors(errs) {
		return nil, errs
	}
	syn, perrs := synthesize(cs, res)
	errs = append(errs, perrs...)
	if hasErrors(errs) {
		return nil, errs
	}
	out, merrs := materialize(syn, opts.Mode)
	errs = append(errs, merrs...)
	if hasErrors(errs) {
		return nil, errs
	}
	errs = append(errs, validateIR(out)...)
	if hasErrors(errs) {
		return nil, errs
	}
	v, err := out.ComputeVersion()
	if err != nil {
		return nil, append(errs, CompileError{
			Stage:    StageValidate,
			Severity: SeverityError,
			Message:  fmt.Sprintf("compute IR version: %v", err),
		})
	}
	out.Version = v
	return out, errs
}
