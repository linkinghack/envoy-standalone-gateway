package compile

import (
	"fmt"

	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// Stage 是编译流水线阶段标识（编译层 §3/§4）。
type Stage string

// 编译流水线的全部阶段。
const (
	StageSchema   Stage = "schema"   // F1 结构校验（由 protocol loader 完成）
	StageLink     Stage = "link"     // F2 链接与语义校验
	StageBuild    Stage = "build"    // F3 构建
	StagePatch    Stage = "patch"    // F4 合成（escape hatch）
	StageValidate Stage = "validate" // F6 资源校验
	StageEnvoy    Stage = "envoy"    // F7 envoy --mode validate 终检
)

// Severity 是编译错误的严重级别（编译层 §4）。
type Severity string

// 严重级别取值。
const (
	SeverityError   Severity = "Error"
	SeverityWarning Severity = "Warning" // 如 F7 缺 envoy 二进制
)

// SourceRef 把一条编译错误回指到用户 YAML（定义见 internal/ir，此处别名复用，
// 使 IR.SourceMap 与 CompileError 共用同一类型，编译层 §1/§4）。
type SourceRef = ir.SourceRef

// CompileError 是编译层统一错误模型（编译层 §4）。
// 同阶段收集不中断（collect, don't abort），阶段间失败短路。
type CompileError struct {
	Stage    Stage
	Source   SourceRef
	Message  string // 面向用户；中英文由 API 层处理
	Severity Severity
}

// Error 实现 error 接口。
func (e CompileError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Stage, e.Source, e.Message)
}

// srcRef 从 protocol.Origin + 对象标识 + 字段路径构造 SourceRef。
func srcRef(origin protocol.Origin, kind protocol.Kind, name, path string) SourceRef {
	return SourceRef{File: origin.File, Kind: kind, Name: name, Path: path}
}

// linkError 构造一条 F2（link 阶段）错误。
func linkError(origin protocol.Origin, kind protocol.Kind, name, path, format string, args ...any) CompileError {
	return CompileError{
		Stage:    StageLink,
		Source:   srcRef(origin, kind, name, path),
		Message:  fmt.Sprintf(format, args...),
		Severity: SeverityError,
	}
}

// buildError 构造一条 F3（build 阶段）错误。
func buildError(origin protocol.Origin, kind protocol.Kind, name, path, format string, args ...any) CompileError {
	return CompileError{
		Stage:    StageBuild,
		Source:   srcRef(origin, kind, name, path),
		Message:  fmt.Sprintf(format, args...),
		Severity: SeverityError,
	}
}

// patchError 构造一条 F4（patch 阶段，含 F5 形态化暴露的 patch 后果）错误。
func patchError(origin protocol.Origin, kind protocol.Kind, name, path, format string, args ...any) CompileError {
	return CompileError{
		Stage:    StagePatch,
		Source:   srcRef(origin, kind, name, path),
		Message:  fmt.Sprintf(format, args...),
		Severity: SeverityError,
	}
}

// validateError 构造一条 F6（validate 阶段）错误：带资源标识，并经 SourceMap 回指
// 用户对象；资源无溯源（如 EnvoyResources 合并前的中间态）时退化为裸资源名。
func validateError(out *ir.IR, key ir.ResourceKey, format string, args ...any) CompileError {
	src, ok := out.SourceMap[key]
	if !ok {
		src = ir.SourceRef{Name: key.Name}
	}
	return CompileError{
		Stage:    StageValidate,
		Source:   src,
		Message:  fmt.Sprintf("%s: %s", key, fmt.Sprintf(format, args...)),
		Severity: SeverityError,
	}
}

// hasErrors 报告错误集合中是否有 Error 级条目（Warning 不阻断阶段间流转）。
func hasErrors(errs []CompileError) bool {
	for _, e := range errs {
		if e.Severity == SeverityError {
			return true
		}
	}
	return false
}
