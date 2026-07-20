package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

func defaultConfig() *Config {
	return &Config{
		DataDir: DefaultDataDir,
		Deliver: DeliverConfig{
			Mode: ModeXDS,
			XDS: XDSConfig{
				Listen:       DefaultListen,
				NodeID:       DefaultNodeID,
				NodeCluster:  DefaultNodeCluster,
				AdminAddress: DefaultAdminAddress,
				AckTimeout:   protocol.Duration{Duration: DefaultAckTimeout},
			},
		},
	}
}

func TestLoadFile(t *testing.T) {
	tests := []struct {
		name    string
		content string // 写入临时文件的内容
		noFile  bool   // true 表示不写文件（模拟文件不存在）
		want    *Config
		wantErr string // 期望错误信息子串（与 want 互斥）
	}{
		{
			name:    "文件不存在返回错误（serve 要求显式配置）",
			noFile:  true,
			wantErr: "no such file",
		},
		{
			name:    "空文件 = 全默认值",
			content: "",
			want:    defaultConfig(),
		},
		{
			name:    "空文档 = 全默认值",
			content: "---\n",
			want:    defaultConfig(),
		},
		{
			name: "全字段",
			content: `
dataDir: /tmp/esgw-data
deliver:
  mode: xds
  xds:
    listen: 127.0.0.2:19000
    nodeID: edge-1
    nodeCluster: edge
    adminAddress: 127.0.0.1:9901
    ackTimeout: 30s
`,
			want: &Config{
				DataDir: "/tmp/esgw-data",
				Deliver: DeliverConfig{
					Mode: ModeXDS,
					XDS: XDSConfig{
						Listen:       "127.0.0.2:19000",
						NodeID:       "edge-1",
						NodeCluster:  "edge",
						AdminAddress: "127.0.0.1:9901",
						AckTimeout:   protocol.Duration{Duration: 30 * time.Second},
					},
				},
			},
		},
		{
			name: "部分字段缺省项取默认值",
			content: `
deliver:
  xds:
    listen: "[::1]:18000"
`,
			want: func() *Config {
				c := defaultConfig()
				c.Deliver.XDS.Listen = "[::1]:18000"
				return c
			}(),
		},
		{
			name:    "未知顶层字段报错（proc.* 等保留键当前不接受，SD4）",
			content: "proc:\n  enabled: true\n",
			wantErr: `unknown field "proc"`,
		},
		{
			name:    "未知嵌套字段报错（deliver.xds.tls 为 P2 预留）",
			content: "deliver:\n  xds:\n    tls:\n      certFile: /etc/esgw/xds.crt\n",
			wantErr: `unknown field "tls"`,
		},
		{
			name:    "多文档报错（esgw.yaml 为单文档）",
			content: "deliver:\n  mode: xds\n---\ndeliver:\n  mode: xds\n",
			wantErr: "single YAML document",
		},
		{
			name:    "mode 非法枚举",
			content: "deliver:\n  mode: grpc\n",
			wantErr: `deliver.mode "grpc" invalid`,
		},
		{
			name:    "mode: static 为合法枚举（serve 启动才报未实现，SD2）",
			content: "deliver:\n  mode: static\n",
			want: func() *Config {
				c := defaultConfig()
				c.Deliver.Mode = ModeStatic
				return c
			}(),
		},
		{
			name:    "非 loopback listen 拒绝：0.0.0.0",
			content: "deliver:\n  xds:\n    listen: 0.0.0.0:18000\n",
			wantErr: "reserved for P2",
		},
		{
			name:    "非 loopback listen 拒绝：具体外网 IP",
			content: "deliver:\n  xds:\n    listen: 192.168.1.10:18000\n",
			wantErr: "reserved for P2",
		},
		{
			name:    "非 loopback listen 拒绝：空 host 即全部网卡",
			content: "deliver:\n  xds:\n    listen: \":18000\"\n",
			wantErr: "reserved for P2",
		},
		{
			name:    "非 loopback listen 拒绝：非 IP 字面量主机名无法静态判定",
			content: "deliver:\n  xds:\n    listen: gw.internal:18000\n",
			wantErr: "not an IP literal",
		},
		{
			name:    "loopback 边界：127.0.0.0/8 全段接受",
			content: "deliver:\n  xds:\n    listen: 127.1.2.3:18000\n",
			want: func() *Config {
				c := defaultConfig()
				c.Deliver.XDS.Listen = "127.1.2.3:18000"
				return c
			}(),
		},
		{
			name:    "loopback 边界：localhost 按 loopback 接受（T1 取舍）",
			content: "deliver:\n  xds:\n    listen: localhost:18000\n",
			want: func() *Config {
				c := defaultConfig()
				c.Deliver.XDS.Listen = "localhost:18000"
				return c
			}(),
		},
		{
			name:    "listen 端口非法",
			content: "deliver:\n  xds:\n    listen: 127.0.0.1:notaport\n",
			wantErr: "invalid port",
		},
		{
			name:    "listen 缺端口不可解析",
			content: "deliver:\n  xds:\n    listen: 127.0.0.1\n",
			wantErr: "not a valid host:port",
		},
		{
			name:    "adminAddress 形态非法：非 unix:/// 前缀的相对 socket 路径",
			content: "deliver:\n  xds:\n    adminAddress: unix://run/esgw.sock\n",
			wantErr: "want unix:///<path> or host:port",
		},
		{
			name:    "adminAddress 形态非法：unix:/// 空路径",
			content: "deliver:\n  xds:\n    adminAddress: unix:///\n",
			wantErr: "empty socket path",
		},
		{
			name:    "adminAddress 形态非法：其他 scheme",
			content: "deliver:\n  xds:\n    adminAddress: tcp://127.0.0.1:9901\n",
			wantErr: "want unix:///<path> or host:port",
		},
		{
			name:    "adminAddress 形态非法：host:port 端口越界",
			content: "deliver:\n  xds:\n    adminAddress: 127.0.0.1:0\n",
			wantErr: "invalid port",
		},
		{
			name:    "ackTimeout 非法时长字符串",
			content: "deliver:\n  xds:\n    ackTimeout: soon\n",
			wantErr: "invalid duration",
		},
		{
			name:    "ackTimeout 非正值报错",
			content: "deliver:\n  xds:\n    ackTimeout: -5s\n",
			wantErr: "positive duration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "esgw.yaml")
			if !tt.noFile {
				if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
					t.Fatalf("write test file: %v", err)
				}
			}
			got, err := LoadFile(path)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("LoadFile() = %+v, want error containing %q", got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadFile() error = %q, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadFile() unexpected error: %v", err)
			}
			if *got != *tt.want {
				t.Errorf("LoadFile() = %+v, want %+v", *got, *tt.want)
			}
		})
	}
}
