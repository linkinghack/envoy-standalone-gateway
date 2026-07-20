package xds

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	cache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	gcplog "github.com/envoyproxy/go-control-plane/pkg/log"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/grpc"

	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver"
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
)

// eventBufSize 是单个事件订阅者的缓冲容量。事件流是观察通道（权威状态
// 是 Status），慢消费者直接丢弃新事件并计数，不阻塞 Apply（取舍见
// Sprint 260720 T3 任务书进展记录）。
const eventBufSize = 16

// Server 是 xds 模式的 deliver.Deliverer 实现：ADS gRPC server +
// SotW SnapshotCache 生命周期 + callbacks ACK/NACK 跟踪
// （下发层 §2.2~§2.6、§6.1~§6.3）。
//
// 并发模型：mu 同时是 Apply/UpdateEndpoints 的单写者锁（§6.1 规则 3）与
// status/subs 的保护锁；callbacks 在流 goroutine 上被同步调用，只短暂持锁
// 更新状态，不调用 Apply，无锁序问题。
type Server struct {
	nodeID string
	log    *slog.Logger
	cache  cache.SnapshotCache

	mu     sync.Mutex
	status deliver.Status
	subs   map[int]chan deliver.Event
	subSeq int
	// dropped 是因订阅者消费过慢而丢弃的事件总数（慢消费者丢弃并计数）。
	dropped uint64
	// acked 按 type_url 跟踪最近一次 ACK 的 version_info（§2.5 按
	// (node, type_url) 跟踪；v1 单节点假设下 key 只含 type_url）——
	// 仅在 ACK 版本前进时记日志，避免常态重连刷屏。
	acked map[string]string
}

// NewServer 构造 xds Server：SnapshotCache 以 ads=true、node 哈希取
// node.Id（cache.IDHash）构造（§2.1）。log 为 nil 时用 slog.Default()。
func NewServer(nodeID string, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		nodeID: nodeID,
		log:    log,
		cache:  cache.NewSnapshotCache(true, cache.IDHash{}, gcpLogger{log}),
		status: deliver.Status{Phase: deliver.PhaseIdle},
		subs:   make(map[int]chan deliver.Event),
		acked:  make(map[string]string),
	}
}

// 编译期断言：Server 实现 deliver.Deliverer。
var _ deliver.Deliverer = (*Server)(nil)

// Apply 原子提交新 IR（§6.1）：幂等跳过 → FromIR 装配 → SetSnapshot →
// Status{awaiting_confirm} → Event{Applied}。同步路径失败 → Status.Phase=
// failed + error（错误信息带 stage=assemble|set_snapshot，§6.3）。
//
// 幂等跳过（规则 1）：IR.Version 与当前受理版本相同 → 直接成功，Status
// 不变、不重发 Applied 事件（取舍：事件语义是「发生了一次换版受理」，
// 幂等跳过无换版动作，重发会让消费方误判新发布；跳过本身由 Apply 的
// 同步 nil 返回表达）。
func (s *Server) Apply(ctx context.Context, i *ir.IR) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if i != nil && s.status.Version != "" && i.Version == s.status.Version {
		s.log.Debug("xds: apply idempotent skip", "version", i.Version)
		return nil
	}

	snap, err := FromIR(i)
	if err != nil {
		return s.failLocked(fmt.Errorf("stage=assemble: %w", err))
	}
	if err := s.cache.SetSnapshot(ctx, s.nodeID, snap); err != nil {
		return s.failLocked(fmt.Errorf("stage=set_snapshot: %w", err))
	}

	s.status = deliver.Status{
		Version:   i.Version,
		Phase:     deliver.PhaseAwaitingConfirm,
		UpdatedAt: time.Now(),
	}
	s.log.Info("xds: snapshot applied",
		"version", i.Version, "nodeID", s.nodeID,
		"listeners", len(i.Listeners), "clusters", len(i.Clusters),
		"routes", len(i.Routes), "endpoints", len(i.Endpoints), "secrets", len(i.Secrets))
	s.broadcastLocked(deliver.Event{
		Kind:    deliver.EventApplied,
		Version: i.Version,
		At:      s.status.UpdatedAt,
	})
	return nil
}

// failLocked 把 Apply 同步失败记入 Status（Phase=failed）并返回错误
// （§6.3：旧配置不变——SetSnapshot 未执行或失败时 snapshot 保持原样）。
// 调用方必须持有 s.mu。
func (s *Server) failLocked(err error) error {
	s.status.Phase = deliver.PhaseFailed
	s.status.Detail = err.Error()
	s.status.UpdatedAt = time.Now()
	return fmt.Errorf("xds: apply: %w", err)
}

// UpdateEndpoints EDS 快速通道：M-DISCO 未落地，恒返回
// deliver.ErrEndpointsUnsupported（technical_design SD3，防御性契约）。
func (s *Server) UpdateEndpoints(context.Context, map[string]*endpointv3.ClusterLoadAssignment) error {
	return deliver.ErrEndpointsUnsupported
}

// Status 返回当前下发状态快照。
func (s *Server) Status() deliver.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Events 订阅异步事件流（§6.2）：每订阅者一条缓冲通道（容量
// eventBufSize），fan-out 非阻塞发送，慢消费者丢弃并计数。cancel 退订并
// 关闭通道（重复调用安全）。
func (s *Server) Events() (ch <-chan deliver.Event, cancel func()) {
	out := make(chan deliver.Event, eventBufSize)
	s.mu.Lock()
	id := s.subSeq
	s.subSeq++
	s.subs[id] = out
	s.mu.Unlock()

	var once sync.Once
	return out, func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subs, id)
			s.mu.Unlock()
			close(out)
		})
	}
}

// broadcastLocked 向全部订阅者 fan-out；调用方必须持有 s.mu。
func (s *Server) broadcastLocked(ev deliver.Event) {
	for id, ch := range s.subs {
		select {
		case ch <- ev:
		default:
			s.dropped++
			s.log.Warn("xds: event subscriber too slow, dropping event",
				"subscriber", id, "kind", string(ev.Kind), "droppedTotal", s.dropped)
		}
	}
}

// Serve 在 lis 上阻塞提供 ADS gRPC 服务，直到 ctx 取消（GracefulStop 等待
// 在途流排空）或 Serve 自身出错。go-control-plane server 以同一 ctx 构造：
// ctx 取消先终止全部 xDS 流，GracefulStop 因此不会挂死在长流上。
//
// 调用前应先 Apply 首版（先 SetSnapshot 再 Accept，§2.2 启动序不变量）。
func (s *Server) Serve(ctx context.Context, lis net.Listener) error {
	callbacks := serverv3.CallbackFuncs{
		StreamRequestFunc: s.onStreamRequest,
	}
	xdsHandler := serverv3.NewServer(ctx, s.cache, callbacks)
	grpcSrv := grpc.NewServer()
	discoveryv3.RegisterAggregatedDiscoveryServiceServer(grpcSrv, xdsHandler)

	errCh := make(chan error, 1)
	go func() { errCh <- grpcSrv.Serve(lis) }()

	select {
	case <-ctx.Done():
		grpcSrv.GracefulStop()
		<-errCh // GracefulStop 返回后 Serve 以 nil 退出
		return nil
	case err := <-errCh:
		return fmt.Errorf("xds: serve %s: %w", lis.Addr(), err)
	}
}

// onStreamRequest 是 callbacks 入口（§2.5 + technical_design SD7），
// ADS 单流上每个 DiscoveryRequest 同步调用一次（go-control-plane 在 nonce
// 判陈之前就回调，NACK 必然到达）。返回 nil——回调返回错误会终结流，
// ACK/NACK 跟踪不是拒绝连接的理由。
func (s *Server) onStreamRequest(_ int64, req *discoveryv3.DiscoveryRequest) error {
	nodeID := req.GetNode().GetId()
	typeURL := req.GetTypeUrl()

	// 未知 node id（§2.3）：不装配 snapshot（SnapshotCache 无其快照，
	// 请求按 SotW 语义挂起无响应），warning 日志提示误接。
	if nodeID != s.nodeID {
		s.log.Warn("xds: request from unknown node id, no snapshot assembled",
			"nodeID", nodeID, "typeURL", typeURL)
		return nil
	}

	if ed := req.GetErrorDetail(); ed != nil {
		// NACK（§2.5）：结构化记录 → Status.Phase=nacked → Event{Nacked}
		// → 日志。不自动重推、不回滚——snapshot 保持，由下一次 Apply 覆盖。
		detail := fmt.Sprintf("type_url=%s nonce=%s error_detail=%s",
			typeURL, req.GetResponseNonce(), ed.GetMessage())
		s.mu.Lock()
		s.status.Phase = deliver.PhaseNacked
		s.status.Detail = detail
		s.status.UpdatedAt = time.Now()
		version := s.status.Version
		s.broadcastLocked(deliver.Event{
			Kind:    deliver.EventNacked,
			Version: version,
			Detail:  detail,
			At:      s.status.UpdatedAt,
		})
		s.mu.Unlock()
		s.log.Error("xds: NACK received",
			"nodeID", nodeID, "typeURL", typeURL,
			"nonce", req.GetResponseNonce(), "version", version,
			"errorDetail", ed.GetMessage())
		return nil
	}

	// ACK（SD7）：version_info == 当前受理版本且无 ErrorDetail。记 Debug
	// 日志（级别取舍：ACK 是每类型每次换版的常态路径，Info 级会在每次
	// 发布产生五倍类型数的常态噪音）；且仅在 ACK 版本前进时记录。
	if vi := req.GetVersionInfo(); vi != "" {
		s.mu.Lock()
		acked := vi == s.status.Version
		advanced := acked && s.acked[typeURL] != vi
		if acked {
			s.acked[typeURL] = vi
		}
		s.mu.Unlock()
		if advanced {
			s.log.Debug("xds: ACK received",
				"nodeID", nodeID, "typeURL", typeURL,
				"nonce", req.GetResponseNonce(), "version", vi)
		}
	}
	return nil
}

// gcpLogger 把 go-control-plane 的 pkg/log.Logger 接口桥接到 log/slog
// （项目日志约定：log/slog，见 internal/core）。
type gcpLogger struct{ log *slog.Logger }

func (g gcpLogger) Debugf(format string, args ...any) { g.log.Debug(fmt.Sprintf(format, args...)) }
func (g gcpLogger) Infof(format string, args ...any)  { g.log.Info(fmt.Sprintf(format, args...)) }
func (g gcpLogger) Warnf(format string, args ...any)  { g.log.Warn(fmt.Sprintf(format, args...)) }
func (g gcpLogger) Errorf(format string, args ...any) { g.log.Error(fmt.Sprintf(format, args...)) }

// 编译期断言：gcpLogger 实现 go-control-plane 的日志接口。
var _ gcplog.Logger = gcpLogger{}
