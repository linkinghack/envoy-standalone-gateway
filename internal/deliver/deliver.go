package deliver

import (
	"context"
	"errors"
	"time"

	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
)

// Deliverer 是下发层（M-DELIVER）对各模式实现的统一契约
// （下发层设计 §6.1，冻结签名见 Sprint 260720 technical_design §3）。
//
// xds 模式：Apply = FromIR 装配 → Consistent 自检 → SetSnapshot（不等待
// Envoy 连接或 ACK，受理即返回）；static 模式运行时骨架归 S7。
type Deliverer interface {
	// Apply 原子提交新 IR：xds = 换 snapshot 版本；static = 渲染落盘
	// （+托管时热重启）。IR.Version 与当前下发版本相同 → 幂等跳过直接
	// 成功（§6.1 规则 1）。Apply / UpdateEndpoints 单写者串行（规则 3）。
	Apply(ctx context.Context, ir *ir.IR) error
	// UpdateEndpoints EDS 快速通道（下发层 §3.1）。xds 模式在 M-DISCO
	// 落地前返回 ErrEndpointsUnsupported（SD3，防御性契约）。
	UpdateEndpoints(ctx context.Context, updates map[string]*endpointv3.ClusterLoadAssignment) error
	// Status 当前下发状态快照（控制台/排障）；Events 异步事件流（§6.2）。
	Status() Status
	Events() (ch <-chan Event, cancel func())
}

// ErrEndpointsUnsupported 是 UpdateEndpoints 的防御性哨兵错误
// （technical_design SD3）：M-DISCO 未落地前 xds 实现恒返回它。
var ErrEndpointsUnsupported = errors.New("deliver: UpdateEndpoints unsupported (EDS fast channel requires M-DISCO, not landed yet)")

// Phase 是下发状态机的阶段（下发层 §6.2）。
type Phase string

// Phase 全量枚举。
const (
	// PhaseIdle 尚未下发任何版本（Status.Version == ""）。
	PhaseIdle Phase = "idle"
	// PhaseDelivering 换版进行中（Apply 同步路径内）。
	PhaseDelivering Phase = "delivering"
	// PhaseAwaitingConfirm snapshot 已受理，等待生效确认。
	PhaseAwaitingConfirm Phase = "awaiting_confirm"
	// PhaseConfirmed 版本已确认生效。S2 无人驱动到该态——由 S4 的
	// M-STATE VersionConfirmEvent（CONFIRMED）驱动更新（下发层 §6.2）。
	PhaseConfirmed Phase = "confirmed"
	// PhaseNacked Envoy 异步拒绝某类型资源（§2.5）；Envoy 保持旧配置运行。
	PhaseNacked Phase = "nacked"
	// PhaseFailed Apply 同步路径失败（stage=assemble/set_snapshot），
	// 旧配置不变。
	PhaseFailed Phase = "failed"
)

// Status 是当前下发状态快照（下发层 §6.2）。
type Status struct {
	Version   string    // 当前受理的 IR.Version（"" = 尚未下发）
	Phase     Phase     // 见 Phase 枚举
	Detail    string    // NACK error_detail / 失败摘要
	UpdatedAt time.Time // 最近一次状态变化时间
}

// EventKind 是下发事件类别（下发层 §6.2）。
type EventKind string

// EventKind 枚举。S2 实现 Applied / Nacked；HotRestartFailed 与
// SupervisorDegraded 为枚举预留——由 S7 的 M-PROC（static 热重启失败 /
// 托管崩溃退避 degraded，下发层 §5.3/§5.4）产生。
const (
	// EventApplied 一次换版已受理（xds：snapshot 已装载）。
	EventApplied EventKind = "Applied"
	// EventNacked Envoy 拒绝某类型资源；Detail 含 type_url + nonce +
	// error_detail 原文。发布流可消费它提前结束 CONFIRMING 等待。
	EventNacked EventKind = "Nacked"
	// EventHotRestartFailed 预留（S7）：static 热重启新 epoch 未 live，
	// Detail 为新进程 stderr 尾部。
	EventHotRestartFailed EventKind = "HotRestartFailed"
	// EventSupervisorDegraded 预留（S7）：托管 envoy 频繁崩溃进入
	// degraded，Detail 含崩溃计数。
	EventSupervisorDegraded EventKind = "SupervisorDegraded"
)

// Event 是下发层的异步事件（下发层 §6.2）。
type Event struct {
	Kind    EventKind // Applied | Nacked（HotRestartFailed/SupervisorDegraded 预留 S7）
	Version string    // 关联的 IR.Version
	Detail  string    // Nacked: type_url + nonce + envoy error_detail 原文
	At      time.Time
}
