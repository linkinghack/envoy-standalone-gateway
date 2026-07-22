package core

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	resource "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/linkinghack/envoy-standalone-gateway/internal/config"
)

// discardLog 丢弃日志（测试不断言日志内容时用）。
func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testConfig 构造 xds 模式的测试配置（listen 由调用方填；结构校验是
// config.LoadFile 的职责，RunServe 不重复校验，故其余字段可取零值）。
func testConfig(listen string) *config.Config {
	return &config.Config{
		DataDir: config.DefaultDataDir,
		API:     config.APIConfig{Listen: config.DefaultAPIListen, Topology: config.DefaultTopology},
		Deliver: config.DeliverConfig{
			Mode: config.ModeXDS,
			XDS: config.XDSConfig{
				Listen: listen,
				NodeID: config.DefaultNodeID,
			},
		},
	}
}

// TestRunServeStaticMode 覆盖 SD2：deliver.mode=static → 明确报
// 「static 运行时下发未实现（S7）」。
func TestRunServeStaticMode(t *testing.T) {
	cfg := testConfig("127.0.0.1:0")
	cfg.Deliver.Mode = config.ModeStatic
	err := RunServe(context.Background(), cfg, "testdata", discardLog())
	if err == nil || !strings.Contains(err.Error(), "static 运行时下发未实现（S7）") {
		t.Fatalf("RunServe err = %v, want static 未实现（S7）", err)
	}
}

// TestRunServeBadConfigDir 覆盖启动序：配置目录加载/编译失败 → 逐条日志
// 并返回错误（不进入下发）。
func TestRunServeBadConfigDir(t *testing.T) {
	cfg := testConfig("127.0.0.1:0")
	cfg.DataDir = t.TempDir()
	configDir := filepath.Join(cfg.DataDir, "config.d")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "bad.yaml"), []byte("kind: nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := RunServe(context.Background(), cfg, configDir, discardLog())
	if err == nil || !strings.Contains(err.Error(), "load draft") {
		t.Fatalf("RunServe err = %v, want load draft error", err)
	}
}

// TestRunServeSmoke 是「esgw serve 可启动并服务」的测试验证：真实编译
// testdata/s1 → Apply 首版 → 真实端口服务 ADS；client 以 node.id=esgw-node
// 拉到 LDS 且 version_info 为 IR.Version 形态（12 位哈希）；ctx 取消后
// RunServe 优雅返回 nil（GracefulStop）。
func TestRunServeSmoke(t *testing.T) {
	// testdata/s1/input 的证书路径相对仓库根（同 internal/golden 的处理）。
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	t.Chdir(filepath.Dir(filepath.Dir(filepath.Dir(file))))

	// 先占一个空闲端口再释放（listen 地址须为静态可配字符串）。
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	addr := probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}

	apiProbe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("API probe listen: %v", err)
	}
	apiAddr := apiProbe.Addr().String()
	if err := apiProbe.Close(); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	configDir := filepath.Join(dataDir, "config.d")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join("testdata", "s1", "input")
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, readErr := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if writeErr := os.WriteFile(filepath.Join(configDir, entry.Name()), content, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	cfg := testConfig(addr)
	cfg.DataDir = dataDir
	cfg.API.Listen = apiAddr
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunServe(ctx, cfg, configDir, discardLog())
	}()

	// 等服务就绪（拨号重试收敛，不靠固定 sleep）。
	var stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesClient
	var conn *grpc.ClientConn
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err = grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("grpc.NewClient: %v", err)
		}
		stream, err = discoveryv3.NewAggregatedDiscoveryServiceClient(conn).
			StreamAggregatedResources(ctx)
		if err == nil {
			err = stream.Send(&discoveryv3.DiscoveryRequest{
				Node:    &corev3.Node{Id: config.DefaultNodeID},
				TypeUrl: resource.ListenerType,
			})
		}
		if err == nil {
			break
		}
		_ = conn.Close()
		if time.Now().After(deadline) {
			t.Fatalf("server not ready: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	resp, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	if resp.GetTypeUrl() != resource.ListenerType {
		t.Errorf("type_url = %q, want LDS", resp.GetTypeUrl())
	}
	if v := resp.GetVersionInfo(); len(v) != 12 {
		t.Errorf("version_info = %q, want 12 位 IR.Version 哈希", v)
	}
	_ = conn.Close()

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("RunServe: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunServe did not return after ctx cancel")
	}
}
