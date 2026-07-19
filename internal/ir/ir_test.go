package ir

import (
	"strings"
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// testIR 构造一个最小 IR。
func testIR() *IR {
	return &IR{
		Listeners: map[string]*listenerv3.Listener{
			"lis/web": {Name: "lis/web"},
		},
		Routes: map[string]*routev3.RouteConfiguration{
			"rc/web": {Name: "rc/web", VirtualHosts: []*routev3.VirtualHost{
				{Name: "vh/api", Domains: []string{"api.example.com"}},
			}},
		},
		Bootstrap: nil,
		SourceMap: map[ResourceKey]SourceRef{},
	}
}

// TestMarshalDeterministic 校验同一 IR 两次序列化字节一致，
// 且与 map 插入顺序无关（编译层 §5，A6）。
func TestMarshalDeterministic(t *testing.T) {
	a := testIR()
	// 同一批资源、不同插入顺序。
	b := &IR{
		Listeners: map[string]*listenerv3.Listener{},
		Routes:    map[string]*routev3.RouteConfiguration{},
		SourceMap: map[ResourceKey]SourceRef{},
	}
	b.Routes["rc/web"] = a.Routes["rc/web"]
	b.Listeners["lis/web"] = a.Listeners["lis/web"]

	ba, err := a.MarshalDeterministic()
	if err != nil {
		t.Fatal(err)
	}
	bb, err := b.MarshalDeterministic()
	if err != nil {
		t.Fatal(err)
	}
	if string(ba) != string(bb) {
		t.Fatal("deterministic marshal differs for equal IRs")
	}

	// 内容变化 → 字节变化。
	c := testIR()
	c.Listeners["lis/web"].Address = &corev3.Address{
		Address: &corev3.Address_SocketAddress{
			SocketAddress: &corev3.SocketAddress{Address: "0.0.0.0"},
		},
	}
	bc, err := c.MarshalDeterministic()
	if err != nil {
		t.Fatal(err)
	}
	if string(ba) == string(bc) {
		t.Fatal("deterministic marshal identical after mutation")
	}
}

// TestComputeVersion 校验版本哈希形态（SHA-256 前 12 位 hex）与稳定性。
func TestComputeVersion(t *testing.T) {
	i := testIR()
	v1, err := i.ComputeVersion()
	if err != nil {
		t.Fatal(err)
	}
	v2, err := testIR().ComputeVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v1 != v2 {
		t.Fatalf("version unstable: %q vs %q", v1, v2)
	}
	if len(v1) != 12 {
		t.Fatalf("version %q: want 12 hex chars", v1)
	}
	for _, c := range v1 {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("version %q: not lowercase hex", v1)
		}
	}
}

// TestSourceRefString 校验回指的可读输出。
func TestSourceRefString(t *testing.T) {
	r := SourceRef{File: "a.yaml", Kind: protocol.KindRoute, Name: "api", Path: "spec.rules[0]"}
	if got := r.String(); got != "a.yaml Route/api: spec.rules[0]" {
		t.Fatalf("String() = %q", got)
	}
	if got := (SourceRef{Kind: protocol.KindGateway, Name: "default"}).String(); got != "<implicit> Gateway/default" {
		t.Fatalf("String() = %q", got)
	}
}
