package xds

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tlsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	resource "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/linkinghack/envoy-standalone-gateway/internal/compile"
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// compileS1XDS 用真实编译流水线产出 S1（协议 §8.1）的 xds 模式 IR。
// 证书路径相对仓库根（testdata/certs/，见 testdata/s1/README.md），
// 故先 chdir 到仓库根（与 internal/golden 测试同款处理）。
func compileS1XDS(t *testing.T) *ir.IR {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	t.Chdir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file)))))

	cs, loadErrs := protocol.LoadDir(filepath.Join("testdata", "s1", "input"))
	if len(loadErrs) != 0 {
		t.Fatalf("S1 load errors: %v", loadErrs)
	}
	out, cerrs := compile.Compile(cs, compile.Options{Mode: compile.ModeXDS})
	for _, e := range cerrs {
		if e.Severity == compile.SeverityError {
			t.Fatalf("S1 compile errors: %v", cerrs)
		}
	}
	if out == nil || out.Version == "" {
		t.Fatal("S1 xds: IR incomplete")
	}
	return out
}

// TestFromIR_S1 是正例：真实 IR 装配成功，五类资源名/数量与 golden
// （testdata/s1/want-xds.json）一致，Consistent 通过，全类型 version
// 等于 IR.Version（下发层 §2.2 规则 2）。
func TestFromIR_S1(t *testing.T) {
	xdsIR := compileS1XDS(t)
	snap, err := FromIR(xdsIR)
	if err != nil {
		t.Fatalf("FromIR: %v", err)
	}

	want := map[resource.Type][]string{
		resource.ListenerType: {"lis/http", "lis/https"},
		resource.ClusterType:  {"us/blog-app", "us/www-app"},
		resource.RouteType:    {"rc/http", "rc/https"},
		resource.EndpointType: {"us/blog-app", "us/www-app"},
		resource.SecretType:   {"crt/https/0", "crt/https/1"},
	}
	for typ, names := range want {
		got := sortedKeys(snap.GetResources(typ))
		if strings.Join(got, ",") != strings.Join(names, ",") {
			t.Errorf("%s resources = %v, want %v", typ, got, names)
		}
		if v := snap.GetVersion(typ); v != xdsIR.Version {
			t.Errorf("%s version = %q, want IR.Version %q", typ, v, xdsIR.Version)
		}
	}
	if err := snap.Consistent(); err != nil {
		t.Errorf("Consistent: %v", err)
	}
}

// TestFromIR_InvalidInput 覆盖入参校验：nil IR 与空 Version 报错。
func TestFromIR_InvalidInput(t *testing.T) {
	cases := []struct {
		name    string
		ir      *ir.IR
		wantErr string
	}{
		{"nil IR", nil, "nil IR"},
		{"empty version", &ir.IR{}, "empty IR.Version"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := FromIR(c.ir); err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("FromIR err = %v, want containing %q", err, c.wantErr)
			}
		})
	}
}

// TestFromIR_Inconsistent 覆盖三类引用不闭合反例（RDS/EDS/SDS），断言
// 装配报错且错误信息含可定位信息（引用名/资源名）。
func TestFromIR_Inconsistent(t *testing.T) {
	cases := []struct {
		name    string
		ir      *ir.IR
		wantErr []string // 错误信息须包含的子串
	}{
		{
			// RDS 不闭合：Listener HCM 引用的 RouteConfiguration 不在 IR.Routes。
			name: "rds reference missing",
			ir: &ir.IR{
				Listeners: map[string]*listenerv3.Listener{
					"lis/x": listenerWithRDS("lis/x", "rc/missing"),
				},
				Version: "v1",
			},
			wantErr: []string{"inconsistent", "rc/missing"},
		},
		{
			// EDS 不闭合：EDS 型 Cluster 的 CLA 不在 IR.Endpoints。
			name: "eds reference missing",
			ir: &ir.IR{
				Clusters: map[string]*clusterv3.Cluster{
					"us/x": edsCluster("us/x"),
				},
				Version: "v1",
			},
			wantErr: []string{"inconsistent", "us/x"},
		},
		{
			// SDS 不闭合：Listener transport socket 引用的 Secret 不在
			// IR.Secrets（go-control-plane v0.14 Consistent 不覆盖 SDS，
			// 由 checkSDSClosure 捕获）。
			name: "sds reference missing",
			ir: &ir.IR{
				Listeners: map[string]*listenerv3.Listener{
					"lis/x": listenerWithSDS("lis/x", "crt/x/0"),
				},
				Version: "v1",
			},
			wantErr: []string{"lis/x", "crt/x/0"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := FromIR(c.ir)
			if err == nil {
				t.Fatal("FromIR succeeded, want inconsistency error")
			}
			for _, sub := range c.wantErr {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("err %q missing %q", err.Error(), sub)
				}
			}
		})
	}
}

// TestFromIR_Pure 验证纯函数性：同一 IR 两次调用互不影响，且 FromIR
// 不修改入参 IR。
//
// 结论（按 go-control-plane v0.14 语义核对）：Snapshot 与装配输入共享
// 底层资源 proto 对象（NewSnapshot→IndexResourcesByName 不深拷贝），但
// 每类资源的索引 map 各自新建；GetResources 返回新建副本。因此两次
// FromIR 产出结构独立、内容等价的 Snapshot；调用方（含 cache）按上游
// 约定不得原地修改资源对象。
func TestFromIR_Pure(t *testing.T) {
	xdsIR := compileS1XDS(t)
	version := xdsIR.Version

	snap1, err := FromIR(xdsIR)
	if err != nil {
		t.Fatalf("FromIR #1: %v", err)
	}
	snap2, err := FromIR(xdsIR)
	if err != nil {
		t.Fatalf("FromIR #2: %v", err)
	}

	// 修改第一次结果（GetResources 副本）不污染第二次结果。
	lis1 := snap1.GetResources(resource.ListenerType)
	delete(lis1, "lis/http")
	if got := len(snap2.GetResources(resource.ListenerType)); got != 2 {
		t.Errorf("snap2 listeners = %d, want 2（snap1 的修改污染了 snap2）", got)
	}
	if err := snap2.Consistent(); err != nil {
		t.Errorf("snap2 Consistent: %v", err)
	}

	// 入参 IR 未被修改。
	if xdsIR.Version != version {
		t.Errorf("IR.Version changed: %q -> %q", version, xdsIR.Version)
	}
	for name, n := range map[string]int{
		"Listeners": len(xdsIR.Listeners), "Clusters": len(xdsIR.Clusters),
		"Routes": len(xdsIR.Routes), "Endpoints": len(xdsIR.Endpoints),
		"Secrets": len(xdsIR.Secrets),
	} {
		if n != 2 {
			t.Errorf("IR.%s = %d, want 2（FromIR 修改了入参）", name, n)
		}
	}
}

// listenerWithRDS 构造经 RDS 引用 routeConfigName 的 Listener（HCM 形态）。
func listenerWithRDS(name, routeConfigName string) *listenerv3.Listener {
	hcm := &hcmv3.HttpConnectionManager{
		StatPrefix: name,
		RouteSpecifier: &hcmv3.HttpConnectionManager_Rds{
			Rds: &hcmv3.Rds{RouteConfigName: routeConfigName},
		},
	}
	return &listenerv3.Listener{
		Name: name,
		FilterChains: []*listenerv3.FilterChain{{
			Filters: []*listenerv3.Filter{{
				Name:       "envoy.filters.network.http_connection_manager",
				ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: mustAny(hcm)},
			}},
		}},
	}
}

// listenerWithSDS 构造 transport socket 经 SDS 引用 secretName 的 Listener。
func listenerWithSDS(name, secretName string) *listenerv3.Listener {
	down := &tlsv3.DownstreamTlsContext{
		CommonTlsContext: &tlsv3.CommonTlsContext{
			TlsCertificateSdsSecretConfigs: []*tlsv3.SdsSecretConfig{{Name: secretName}},
		},
	}
	return &listenerv3.Listener{
		Name: name,
		FilterChains: []*listenerv3.FilterChain{{
			TransportSocket: &corev3.TransportSocket{
				Name:       "envoy.transport_sockets.tls",
				ConfigType: &corev3.TransportSocket_TypedConfig{TypedConfig: mustAny(down)},
			},
		}},
	}
}

// edsCluster 构造 EDS 发现型 Cluster（ServiceName 缺省 = cluster 名）。
func edsCluster(name string) *clusterv3.Cluster {
	return &clusterv3.Cluster{
		Name:                 name,
		ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_EDS},
		EdsClusterConfig: &clusterv3.Cluster_EdsClusterConfig{
			EdsConfig: &corev3.ConfigSource{
				ConfigSourceSpecifier: &corev3.ConfigSource_Ads{Ads: &corev3.AggregatedConfigSource{}},
			},
		},
	}
}

// mustAny 打包 typed_config（测试辅助，失败即 panic）。
func mustAny(m proto.Message) *anypb.Any {
	a, err := anypb.New(m)
	if err != nil {
		panic(err)
	}
	return a
}
