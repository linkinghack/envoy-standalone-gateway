// esgw 是唯一二进制入口；子命令随冲刺增加（compile → serve/bootstrap → ...）。
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/linkinghack/envoy-standalone-gateway/internal/proc"
	"github.com/linkinghack/envoy-standalone-gateway/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run 是顶层分发（标准库 flag + 简单分发，不引 cobra，T6 任务书）。
// 返回进程退出码：0 成功（允许 Warning），1 有 Error，2 用法错误。
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "__proc-launcher":
		return proc.RunLauncher(args[1:], stdout, stderr)
	case "version":
		eprintf(stdout, "%s %s\n", version.BinaryName, version.Version)
		return 0
	case "compile":
		return runCompile(args[1:], stdout, stderr)
	case "serve":
		return runServe(args[1:], stderr)
	case "bootstrap":
		return runBootstrap(args[1:], stdout, stderr)
	default:
		eprintf(stderr, "unknown command %q\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(stderr io.Writer) {
	eprintf(stderr, `usage: %s <command> [flags]

Commands:
  compile   compile a config directory into an Envoy config artifact
  serve     run xDS and management API: -c <esgw.yaml> [-f <data-dir/config.d>]
  bootstrap export an Envoy bootstrap for xDS mode: -c <esgw.yaml> [-o <file>]
  version   print version
`, version.BinaryName)
}

// eprintf / eprintln 写用户输出，忽略写错误（CLI 的 stderr/stdout 写失败
// 无可补救，errcheck 合规用集中在此处）。
func eprintf(w io.Writer, format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }
func eprintln(w io.Writer, s string)                  { _, _ = fmt.Fprintln(w, s) }
