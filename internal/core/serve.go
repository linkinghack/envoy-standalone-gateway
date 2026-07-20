// Package core 是 M-CORE 进程级装配层（dev_design 260720-1 §1）：自身不含
// 业务语义，只负责装配各模块实例并驱动启动序与优雅退出。S2 落地 RunServe
// （xds 骨架）；M-CONF 恢复、M-STATE 探测、M-PROC 托管的插入点见
// dev_design 260720-1 §2.3，就位后按启动序在此追加。
package core

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/linkinghack/envoy-standalone-gateway/internal/compile"
	"github.com/linkinghack/envoy-standalone-gateway/internal/config"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver/xds"
	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// RunServe 是 esgw serve 的装配主流程（Sprint 260720 technical_design §3
// 冻结签名；启动序见 dev_design 260720-1 §2.1 与下发层 §2.2/§7）：
//
//  1. deliver.mode 检查（static → 报「未实现（S7）」，SD2）
//  2. protocol.LoadDir 加载配置目录 → compile.Compile(ModeXDS) 产出 IR
//     （加载/编译错误逐条日志；有 Error 级即返回错误，非法配置不进入下发）
//  3. 构造 xds.Server 并 Apply 首版（FromIR → SetSnapshot）
//  4. 上述全部成功后才 net.Listen + accept xDS 连接——关键不变量：xDS 端口
//     对外可服务之前首版 snapshot 必须已装配（下发层 §2.2）
//  5. 阻塞服务至 ctx 取消 → GracefulStop（dev_design §2.2：esgw 停机不影响
//     数据面，Envoy 按最后有效配置继续服务）
//
// log 为 nil 时用 slog.Default()。项目日志约定：log/slog（S2 引入的首个
// 日志约定，取舍见 Sprint 260720 T3 任务书进展记录）。
func RunServe(ctx context.Context, cfg *config.Config, configDir string, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	if cfg == nil {
		return fmt.Errorf("core: nil config")
	}

	// 步骤 1：模式检查（SD2；esgw compile 的 static 纯导出路径不受影响）。
	if cfg.Deliver.Mode == config.ModeStatic {
		return fmt.Errorf("core: deliver.mode=static: static 运行时下发未实现（S7）；" +
			"纯导出路径请用 esgw compile --mode static")
	}

	// 步骤 2：加载配置目录 → 编译。
	cs, loadErrs := protocol.LoadDir(configDir)
	for _, e := range loadErrs {
		log.Error("config load error", "file", e.Origin.File, "error", e.Message)
	}
	if len(loadErrs) != 0 {
		return fmt.Errorf("core: load config dir %s: %d error(s)", configDir, len(loadErrs))
	}
	out, cerrs := compile.Compile(cs, compile.Options{Mode: compile.ModeXDS})
	errCount := 0
	for _, e := range cerrs {
		if e.Severity == compile.SeverityError {
			errCount++
			log.Error("compile error", "error", e.Error())
		} else {
			log.Warn("compile warning", "warning", e.Error())
		}
	}
	if errCount != 0 {
		return fmt.Errorf("core: compile %s: %d error(s)", configDir, errCount)
	}

	// 步骤 3：下发装配（先 SetSnapshot……）。
	srv := xds.NewServer(cfg.Deliver.XDS.NodeID, log)
	if err := srv.Apply(ctx, out); err != nil {
		return err
	}

	// 步骤 4：……再 Listen + Accept（先装配 snapshot 再 accept 的不变量由
	// 「Apply 先于 Listen 返回」保证；Envoy 提前连接时请求按 SotW 语义挂起，
	// snapshot 就位后即得响应，下发层 §2.6）。
	lis, err := net.Listen("tcp", cfg.Deliver.XDS.Listen)
	if err != nil {
		return fmt.Errorf("core: listen %s: %w", cfg.Deliver.XDS.Listen, err)
	}
	log.Info("esgw serve: xDS listening",
		"listen", lis.Addr().String(),
		"nodeID", cfg.Deliver.XDS.NodeID,
		"version", out.Version)

	// 步骤 5：阻塞服务至 ctx 取消 → GracefulStop。
	return srv.Serve(ctx, lis)
}
