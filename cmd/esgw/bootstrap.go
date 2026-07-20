package main

import (
	"flag"
	"io"

	"github.com/linkinghack/envoy-standalone-gateway/internal/config"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver/xds"
)

// 本文件实现 `esgw bootstrap`（S2 T4；下发层 §2.7 接入 bootstrap、§5.7
// 仅下发模式导出）：
//
//	esgw bootstrap -c <esgw.yaml> [--mode xds] [-o <file>]
//
// 从 esgw.yaml 取 deliver.xds.{nodeID,nodeCluster,listen,adminAddress}
// 渲染接入 bootstrap 并导出（输出契约同 compile：-o 缺省 stdout）。
// --mode 当前仅接受 xds：static 无接入 bootstrap 概念（static 产物由
// esgw compile --mode static 导出），传其他值报用法错误 exit 2。

// runBootstrap 执行 bootstrap 子命令，返回进程退出码（0 成功，1 运行错误，
// 2 用法错误）。
func runBootstrap(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("c", "", "esgw.yaml config file (required)")
	mode := fs.String("mode", config.ModeXDS, "bootstrap mode: xds")
	out := fs.String("o", "", "output file (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		eprintf(stderr, "error: unexpected positional arguments: %v\n", fs.Args())
		return 2
	}
	if *cfgPath == "" {
		eprintln(stderr, "error: -c <esgw.yaml> is required")
		return 2
	}
	if *mode != config.ModeXDS {
		eprintf(stderr, "error: invalid --mode %q (want xds; static 无接入 bootstrap 概念，static 产物由 esgw compile 导出)\n", *mode)
		return 2
	}

	cfg, err := config.LoadFile(*cfgPath)
	if err != nil {
		eprintf(stderr, "error: load esgw.yaml: %v\n", err)
		return 1
	}

	artifact, err := xds.RenderBootstrap(xds.BootstrapOpts{
		NodeID:       cfg.Deliver.XDS.NodeID,
		NodeCluster:  cfg.Deliver.XDS.NodeCluster,
		XDSListen:    cfg.Deliver.XDS.Listen,
		AdminAddress: cfg.Deliver.XDS.AdminAddress,
	})
	if err != nil {
		eprintf(stderr, "error: render bootstrap: %v\n", err)
		return 1
	}

	if err := writeArtifact(*out, artifact, stdout); err != nil {
		eprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
