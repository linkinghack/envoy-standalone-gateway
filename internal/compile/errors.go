package compile

import (
	"fmt"

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

// SourceRef 把一条编译错误回指到用户 YAML：文件、对象（kind/name）与字段路径
// （如 spec.rules[2].retry.on）。UI 据此在 YAML 编辑器行内标注（编译层 §4，FR-4.2）。
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

// hasErrors 报告错误集合中是否有 Error 级条目（Warning 不阻断阶段间流转）。
func hasErrors(errs []CompileError) bool {
	for _, e := range errs {
		if e.Severity == SeverityError {
			return true
		}
	}
	return false
}
