package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/linkinghack/envoy-standalone-gateway/internal/compile"
	"github.com/linkinghack/envoy-standalone-gateway/internal/compile/envoycheck"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver/static"
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// 本文件实现 `esgw compile`（编译层 §6：M0 CLI 是 Compile 的薄封装）：
//
//	esgw compile -f <dir> --mode static|xds -o <file> [--envoy-validate[=<path>]]
//
// 行为：
//   - LoadDir → Compile → static 渲染（static.Render）或 xds snapshot protojson
//     （deliver.SnapshotJSON）；
//   - --envoy-validate 触发 F7：渲染等价 static 产物后跑 `envoy --mode validate`
//     （xds 模式也渲染 static 再验，编译层 §3 F7 语义）；F7 由 CLI 层编排，
//     Compile 保持纯函数（SD5，Options.EnvoyValidate 字段 M0 不在库内消费）；
//   - 错误逐行打印到 stderr：`error|warning: file:kind/name:path: message`；
//     有 Error 级时 exit 1 且不写产物，仅 Warning 时提示但 exit 0。

// compileFlags 是 compile 子命令的解析结果。
type compileFlags struct {
	dir           string
	mode          string
	out           string
	envoyValidate bool
	envoyPath     string
}

// parseCompileFlags 解析 compile 子命令参数。--envoy-validate 支持可选值
// （--envoy-validate 或 --envoy-validate=<path>），标准库 flag 不支持可选值，
// 故在 flag.Parse 前预扫描剥离（路径只接受 = 形式）。
func parseCompileFlags(args []string, stderr io.Writer) (compileFlags, error) {
	var cf compileFlags
	var rest []string
	for _, a := range args {
		switch {
		case a == "-envoy-validate" || a == "--envoy-validate":
			cf.envoyValidate = true
		case strings.HasPrefix(a, "-envoy-validate="):
			cf.envoyValidate, cf.envoyPath = true, strings.TrimPrefix(a, "-envoy-validate=")
		case strings.HasPrefix(a, "--envoy-validate="):
			cf.envoyValidate, cf.envoyPath = true, strings.TrimPrefix(a, "--envoy-validate=")
		default:
			rest = append(rest, a)
		}
	}
	fs := flag.NewFlagSet("compile", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cf.dir, "f", "", "config directory to load (required)")
	fs.StringVar(&cf.mode, "mode", string(compile.ModeStatic), "output mode: static | xds")
	fs.StringVar(&cf.out, "o", "", "output file (default: stdout)")
	if err := fs.Parse(rest); err != nil {
		return cf, err
	}
	if fs.NArg() != 0 {
		return cf, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if cf.dir == "" {
		return cf, fmt.Errorf("-f <dir> is required")
	}
	if compile.Mode(cf.mode) != compile.ModeStatic && compile.Mode(cf.mode) != compile.ModeXDS {
		return cf, fmt.Errorf("invalid --mode %q (want static | xds)", cf.mode)
	}
	return cf, nil
}

// runCompile 执行 compile 子命令，返回进程退出码。
func runCompile(args []string, stdout, stderr io.Writer) int {
	cf, err := parseCompileFlags(args, stderr)
	if err != nil {
		eprintf(stderr, "error: %v\n", err)
		return 2
	}

	cs, loadErrs := protocol.LoadDir(cf.dir)
	for _, e := range loadErrs {
		eprintf(stderr, "error: %s: %s\n", e.Origin.File, e.Message)
	}
	if len(loadErrs) != 0 {
		return 1
	}

	out, cerrs := compile.Compile(cs, compile.Options{Mode: compile.Mode(cf.mode)})
	for _, e := range cerrs {
		eprintln(stderr, formatCompileError(e))
	}
	if hasErrorLevel(cerrs) {
		return 1
	}

	// 渲染产物。
	var artifact []byte
	switch compile.Mode(cf.mode) {
	case compile.ModeStatic:
		artifact, err = static.Render(out)
	default:
		artifact, err = deliver.SnapshotJSON(out)
	}
	if err != nil {
		eprintf(stderr, "error: render artifact: %v\n", err)
		return 1
	}

	// F7 envoy 终检（CLI 层编排；渲染等价 static 后验）。
	if cf.envoyValidate {
		f7errs := runEnvoyCheck(cf, cs, out, stderr)
		for _, e := range f7errs {
			eprintln(stderr, formatCompileError(e))
		}
		if hasErrorLevel(f7errs) {
			return 1 // 终检失败：非法配置不产出（编译层 §3 F7）
		}
	}

	if err := writeArtifact(cf.out, artifact, stdout); err != nil {
		eprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// runEnvoyCheck 执行 F7：发现 envoy 二进制（flag path → ESGW_ENVOY_PATH →
// PATH），对等价 static 渲染产物跑 --mode validate。返回 CompileError 形态的
// 结果（Stage=envoy）：无二进制 = Warning 降级，校验失败 = Error。
func runEnvoyCheck(cf compileFlags, cs *protocol.ConfigSet, out *ir.IR, stderr io.Writer) []compile.CompileError {
	warn := func(format string, args ...any) []compile.CompileError {
		return []compile.CompileError{{
			Stage:    compile.StageEnvoy,
			Severity: compile.SeverityWarning,
			Message:  fmt.Sprintf(format, args...),
		}}
	}

	// 等价 static 产物：static 模式直接复用；xds 模式重新加载并编译
	// （Compile 会原地填充 cs 默认值，不可复用同一 ConfigSet 二次编译）。
	staticIR := out
	if compile.Mode(cf.mode) != compile.ModeStatic {
		fresh, loadErrs := protocol.LoadDir(cf.dir)
		if len(loadErrs) != 0 {
			return warn("skip envoy validate: reload for static render failed")
		}
		var cerrs []compile.CompileError
		staticIR, cerrs = compile.Compile(fresh, compile.Options{Mode: compile.ModeStatic})
		if hasErrorLevel(cerrs) {
			return warn("skip envoy validate: static-mode compile failed")
		}
	}
	staticYAML, err := static.Render(staticIR)
	if err != nil {
		return warn("skip envoy validate: render static artifact: %v", err)
	}

	bin, err := envoycheck.FindBinary(cf.envoyPath)
	if err != nil {
		return warn("skip envoy --mode validate: %v", err)
	}
	if err := envoycheck.Validate(context.Background(), bin, staticYAML, 0); err != nil {
		return []compile.CompileError{{
			Stage:    compile.StageEnvoy,
			Severity: compile.SeverityError,
			Message:  err.Error(),
		}}
	}
	eprintf(stderr, "envoy --mode validate: OK (%s)\n", bin)
	return nil
}

// writeArtifact 把产物写到 -o 指定的文件；out 为空时写 stdout。
func writeArtifact(out string, artifact []byte, stdout io.Writer) error {
	if out == "" {
		_, err := stdout.Write(artifact)
		return err
	}
	if err := os.WriteFile(out, artifact, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	return nil
}

// formatCompileError 按 `error|warning: file:kind/name:path: message` 格式化
// （T6 任务书错误输出契约；缺失的段省略，无文件来源标记 <implicit>）。
func formatCompileError(e compile.CompileError) string {
	sev := "error"
	if e.Severity == compile.SeverityWarning {
		sev = "warning"
	}
	loc := e.Source.File
	if loc == "" {
		loc = "<implicit>"
	}
	if e.Source.Kind != "" || e.Source.Name != "" {
		loc += ":" + string(e.Source.Kind) + "/" + e.Source.Name
	}
	if e.Source.Path != "" {
		loc += ":" + e.Source.Path
	}
	return fmt.Sprintf("%s: %s: %s", sev, loc, e.Message)
}

// hasErrorLevel 报告错误集合中是否有 Error 级条目。
func hasErrorLevel(errs []compile.CompileError) bool {
	for _, e := range errs {
		if e.Severity == compile.SeverityError {
			return true
		}
	}
	return false
}
