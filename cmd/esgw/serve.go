package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/linkinghack/envoy-standalone-gateway/internal/config"
	"github.com/linkinghack/envoy-standalone-gateway/internal/core"
)

// 本文件实现 `esgw serve`（S2 M-CORE 骨架，technical_design SD2）：
//
//	esgw serve -c <esgw.yaml> -f <config-dir>
//
// 两个 flag 均必填：-c 是 esgw 进程自身配置（暂无默认值，演进方向见
// dev_design 260720-1 §3），-f 是网关配置目录（S2 过渡形态的配置真源，
// S3 起由 M-CONF/M-STORE 接管）。
//
// 接线：config.LoadFile → core.RunServe；信号处理 SIGINT/SIGTERM → 取消
// serve 上下文 → xDS gRPC server GracefulStop（dev_design 260720-1 §2.2）。
// 结构化日志用 log/slog 写 stderr（项目日志约定自本命令引入）。

// runServe 执行 serve 子命令，返回进程退出码（0 优雅退出，1 运行错误，
// 2 用法错误）。
func runServe(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := fs.String("c", "", "esgw.yaml config file (required)")
	dir := fs.String("f", "", "gateway config directory (required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		eprintf(stderr, "error: unexpected positional arguments: %v\n", fs.Args())
		return 2
	}
	if *cfgPath == "" || *dir == "" {
		eprintln(stderr, "error: -c <esgw.yaml> and -f <config-dir> are both required")
		return 2
	}

	cfg, err := config.LoadFile(*cfgPath)
	if err != nil {
		eprintf(stderr, "error: load esgw.yaml: %v\n", err)
		return 1
	}

	log := slog.New(slog.NewTextHandler(stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := core.RunServe(ctx, cfg, *dir, log); err != nil {
		eprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
