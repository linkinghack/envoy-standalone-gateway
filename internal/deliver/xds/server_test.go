package xds

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	resource "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"google.golang.org/genproto/googleapis/rpc/code"
	statusv3 "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver"
)

// 本文件是 ADS server 的单测（technical_design SD6：真实 127.0.0.1:0
// 端口 + 真实 gRPC ADS client，不引 bufconn）。teardown 约定：每个 server
// 由 t.Cleanup 取消 ctx（GracefulStop 等在途流排空）并等 Serve 返回；
// 每个 client conn 由 t.Cleanup 关闭——超时 recv 留下的阻塞 goroutine
// 随 conn 关闭退出，不泄漏。

// testNodeID 与 config 默认值一致（下发层 §2.3）。
const testNodeID = "esgw-node"

// fiveTypes 是 ADS 单流复用的全部五类资源（下发层 §2.1）。
var fiveTypes = []resource.Type{
	resource.ListenerType,
	resource.RouteType,
	resource.ClusterType,
	resource.EndpointType,
	resource.SecretType,
}

// serveOn 在 127.0.0.1:0 上启动 srv 的 ADS 服务，返回监听地址。
func serveOn(t *testing.T, srv *Server) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx, lis) }()
	t.Cleanup(func() {
		cancel()
		if err := <-errCh; err != nil {
			t.Errorf("Serve: %v", err)
		}
	})
	return lis.Addr().String()
}

// dialADS 建立真实 gRPC ADS 双向流。
func dialADS(t *testing.T, addr string) discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesClient {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	stream, err := discoveryv3.NewAggregatedDiscoveryServiceClient(conn).
		StreamAggregatedResources(context.Background())
	if err != nil {
		t.Fatalf("StreamAggregatedResources: %v", err)
	}
	return stream
}

// subscribeAll 在 ADS 流上以 wildcard 订阅五类资源（node.id 随每个请求
// 携带，go-control-plane 只保证首个请求必带）。
func subscribeAll(t *testing.T, stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesClient, nodeID string) {
	t.Helper()
	for _, typ := range fiveTypes {
		err := stream.Send(&discoveryv3.DiscoveryRequest{
			Node:    &corev3.Node{Id: nodeID},
			TypeUrl: typ,
		})
		if err != nil {
			t.Fatalf("send %s: %v", typ, err)
		}
	}
}

// recvTimeout 带超时读一条响应。超时返回（nil, nil）；阻塞的 Recv
// goroutine 由 conn 关闭兜底退出（见文件头 teardown 约定）。
func recvTimeout(t *testing.T, stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesClient, d time.Duration) *discoveryv3.DiscoveryResponse {
	t.Helper()
	type result struct {
		resp *discoveryv3.DiscoveryResponse
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := stream.Recv()
		ch <- result{resp, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("recv: %v", r.err)
		}
		return r.resp
	case <-time.After(d):
		return nil
	}
}

// TestApplyServesAllTypes 是 A2 的单测层：编译 testdata/s1 → Apply →
// 真实 ADS client（node.id=esgw-node）拉全五类型，断言每类响应的
// version_info == IR.Version。
func TestApplyServesAllTypes(t *testing.T) {
	xdsIR := compileS1XDS(t)
	srv := NewServer(testNodeID, nil)
	if err := srv.Apply(context.Background(), xdsIR); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	addr := serveOn(t, srv)

	stream := dialADS(t, addr)
	subscribeAll(t, stream, testNodeID)

	got := make(map[resource.Type]*discoveryv3.DiscoveryResponse, len(fiveTypes))
	deadline := time.Now().Add(10 * time.Second)
	for len(got) < len(fiveTypes) {
		resp := recvTimeout(t, stream, time.Until(deadline))
		if resp == nil {
			t.Fatalf("timeout waiting for responses, got %d/%d types: %v",
				len(got), len(fiveTypes), keysOf(got))
		}
		got[resp.GetTypeUrl()] = resp
	}
	for _, typ := range fiveTypes {
		resp := got[typ]
		if resp.GetVersionInfo() != xdsIR.Version {
			t.Errorf("%s version_info = %q, want IR.Version %q", typ, resp.GetVersionInfo(), xdsIR.Version)
		}
		if len(resp.GetResources()) == 0 {
			t.Errorf("%s: empty resources", typ)
		}
	}
}

// TestApplyIdempotentSkip 是 A6 的单测层：重复 Apply 同一 IR → 成功且
// 客户端收不到新推送（响应计数不前进）；Status 不变、不重发 Applied 事件。
func TestApplyIdempotentSkip(t *testing.T) {
	xdsIR := compileS1XDS(t)
	srv := NewServer(testNodeID, nil)
	if err := srv.Apply(context.Background(), xdsIR); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	events, cancelEvents := srv.Events()
	defer cancelEvents()
	addr := serveOn(t, srv)

	stream := dialADS(t, addr)
	subscribeAll(t, stream, testNodeID)
	for range fiveTypes {
		if resp := recvTimeout(t, stream, 10*time.Second); resp == nil {
			t.Fatal("timeout waiting for initial responses")
		}
	}
	stBefore := srv.Status()

	// 重复 Apply 同一 IR：幂等跳过直接成功。
	if err := srv.Apply(context.Background(), xdsIR); err != nil {
		t.Fatalf("idempotent Apply: %v", err)
	}

	// 客户端收不到新推送（SotW 按 version_info 相等性比对，无变化不推）。
	if resp := recvTimeout(t, stream, 500*time.Millisecond); resp != nil {
		t.Fatalf("unexpected push after idempotent Apply: %s version=%s",
			resp.GetTypeUrl(), resp.GetVersionInfo())
	}
	if st := srv.Status(); st != stBefore {
		t.Errorf("Status changed after idempotent skip: %+v -> %+v", stBefore, st)
	}
	select {
	case ev := <-events:
		t.Errorf("unexpected event after idempotent skip: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestNACK 是 A5 的单测层：客户端回带 ErrorDetail 的 DiscoveryRequest →
// Event{Nacked}（Detail 含 type_url+nonce+原文）、Status.Phase=nacked、
// snapshot 未被替换（后续请求仍收到原 version）、不自动重推。
func TestNACK(t *testing.T) {
	xdsIR := compileS1XDS(t)
	srv := NewServer(testNodeID, nil)
	if err := srv.Apply(context.Background(), xdsIR); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	events, cancelEvents := srv.Events()
	defer cancelEvents()
	addr := serveOn(t, srv)

	stream := dialADS(t, addr)
	err := stream.Send(&discoveryv3.DiscoveryRequest{
		Node:    &corev3.Node{Id: testNodeID},
		TypeUrl: resource.ListenerType,
	})
	if err != nil {
		t.Fatalf("send LDS: %v", err)
	}
	resp := recvTimeout(t, stream, 10*time.Second)
	if resp == nil {
		t.Fatal("timeout waiting for LDS response")
	}

	// NACK：version_info 回显被拒版本、nonce 回显被拒响应、带 ErrorDetail。
	err = stream.Send(&discoveryv3.DiscoveryRequest{
		Node:          &corev3.Node{Id: testNodeID},
		TypeUrl:       resource.ListenerType,
		VersionInfo:   resp.GetVersionInfo(),
		ResponseNonce: resp.GetNonce(),
		ErrorDetail: &statusv3.Status{
			Code:    int32(code.Code_INVALID_ARGUMENT),
			Message: "boom: bad listener",
		},
	})
	if err != nil {
		t.Fatalf("send NACK: %v", err)
	}

	// Event{Nacked}：Detail 含 type_url + nonce + error_detail 原文。
	select {
	case ev := <-events:
		if ev.Kind != deliver.EventNacked {
			t.Fatalf("event kind = %q, want Nacked", ev.Kind)
		}
		if ev.Version != xdsIR.Version {
			t.Errorf("event version = %q, want %q", ev.Version, xdsIR.Version)
		}
		for _, sub := range []string{resource.ListenerType, resp.GetNonce(), "boom: bad listener"} {
			if !strings.Contains(ev.Detail, sub) {
				t.Errorf("event detail %q missing %q", ev.Detail, sub)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for Nacked event")
	}

	// Status.Phase=nacked + Detail。
	st := srv.Status()
	if st.Phase != deliver.PhaseNacked {
		t.Errorf("Status.Phase = %q, want nacked", st.Phase)
	}
	if !strings.Contains(st.Detail, "boom: bad listener") {
		t.Errorf("Status.Detail = %q, want NACK 原文", st.Detail)
	}

	// snapshot 未被替换（不自动重推/回滚）：重新以 version_info="" 请求，
	// 仍收到原 version。
	err = stream.Send(&discoveryv3.DiscoveryRequest{
		Node:          &corev3.Node{Id: testNodeID},
		TypeUrl:       resource.ListenerType,
		ResponseNonce: resp.GetNonce(),
	})
	if err != nil {
		t.Fatalf("send re-request: %v", err)
	}
	again := recvTimeout(t, stream, 10*time.Second)
	if again == nil {
		t.Fatal("timeout waiting for LDS re-response")
	}
	if again.GetVersionInfo() != xdsIR.Version {
		t.Errorf("re-response version = %q, want unchanged %q", again.GetVersionInfo(), xdsIR.Version)
	}
}

// TestUnknownNodeID 覆盖 §2.3：未知 node id 的连接不装配 snapshot，
// 请求按 SotW 语义挂起无响应 + warning 日志；已知节点的 Status 不受影响。
func TestUnknownNodeID(t *testing.T) {
	xdsIR := compileS1XDS(t)
	logBuf := &lockedBuffer{}
	log := slog.New(slog.NewTextHandler(logBuf, nil))
	srv := NewServer(testNodeID, log)
	if err := srv.Apply(context.Background(), xdsIR); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	addr := serveOn(t, srv)

	stream := dialADS(t, addr)
	err := stream.Send(&discoveryv3.DiscoveryRequest{
		Node:    &corev3.Node{Id: "stranger"},
		TypeUrl: resource.ListenerType,
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if resp := recvTimeout(t, stream, 500*time.Millisecond); resp != nil {
		t.Fatalf("unknown node got a response: %s version=%s", resp.GetTypeUrl(), resp.GetVersionInfo())
	}
	if !strings.Contains(logBuf.String(), "unknown node id") {
		t.Errorf("missing warning log for unknown node id:\n%s", logBuf.String())
	}
	if st := srv.Status(); st.Version != xdsIR.Version || st.Phase != deliver.PhaseAwaitingConfirm {
		t.Errorf("Status disturbed by unknown node: %+v", st)
	}
}

// TestApplyFailures 覆盖 Apply 失败路径（§6.3）：装配失败 → error 带
// stage=assemble、Status.Phase=failed、旧状态不变（此后成功 Apply 可恢复）；
// 以及 ErrEndpointsUnsupported 哨兵（SD3）与 Applied 事件。
func TestApplyFailures(t *testing.T) {
	srv := NewServer(testNodeID, nil)
	events, cancelEvents := srv.Events()
	defer cancelEvents()

	err := srv.Apply(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "stage=assemble") {
		t.Fatalf("Apply(nil) err = %v, want stage=assemble", err)
	}
	if st := srv.Status(); st.Phase != deliver.PhaseFailed || st.Version != "" {
		t.Errorf("Status after failed Apply = %+v, want Phase=failed Version=\"\"", st)
	}

	xdsIR := compileS1XDS(t)
	if err := srv.Apply(context.Background(), xdsIR); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	select {
	case ev := <-events:
		if ev.Kind != deliver.EventApplied || ev.Version != xdsIR.Version {
			t.Errorf("event = %+v, want Applied %q", ev, xdsIR.Version)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Applied event")
	}
	if st := srv.Status(); st.Phase != deliver.PhaseAwaitingConfirm || st.Version != xdsIR.Version {
		t.Errorf("Status = %+v, want awaiting_confirm %q", st, xdsIR.Version)
	}

	if err := srv.UpdateEndpoints(context.Background(), nil); !errors.Is(err, deliver.ErrEndpointsUnsupported) {
		t.Errorf("UpdateEndpoints err = %v, want ErrEndpointsUnsupported", err)
	}
}

// keysOf 返回已收到响应的 type_url 列表（断言信息用）。
func keysOf(m map[resource.Type]*discoveryv3.DiscoveryResponse) []resource.Type {
	keys := make([]resource.Type, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// lockedBuffer 是并发安全的日志 sink：server goroutine（callbacks）与测试
// goroutine 会并发写/读，裸 bytes.Buffer 在 -race 下报 DATA RACE。
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
