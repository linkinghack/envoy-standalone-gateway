package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	yamlv3 "gopkg.in/yaml.v3"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// 默认值常量（dev_design 260720-1 §3.1）。
const (
	DefaultDataDir      = "/var/lib/esgw"
	DefaultListen       = "127.0.0.1:18000"
	DefaultNodeID       = "esgw-node" // 下发层 §2.3
	DefaultNodeCluster  = "esgw"
	DefaultAdminAddress = "unix:///var/run/esgw/envoy-admin.sock" // SD5
	DefaultAPIListen    = "127.0.0.1:8080"
	DefaultTopology     = "standalone"

	// ModeXDS 为默认下发模式；ModeStatic 为合法枚举，serve 启动即报未实现（S7，SD2）。
	ModeXDS    = "xds"
	ModeStatic = "static"
)

// DefaultAckTimeout 是 ACK 观察窗口默认值（下发层 §2.5；S2 仅作保留字段，SD7）。
const DefaultAckTimeout = 15 * time.Second

// Config 是 esgw.yaml 的根结构。
type Config struct {
	DataDir string        `json:"dataDir"`
	Deliver DeliverConfig `json:"deliver"`
	API     APIConfig     `json:"api"`
	State   StateConfig   `json:"state"`
}

// APIConfig controls the same-origin management HTTP server.
type APIConfig struct {
	Listen   string `json:"listen"`
	Topology string `json:"topology"`
}

// StateConfig controls bounded read-only Envoy admin polling.
type StateConfig struct {
	ReadyInterval    protocol.Duration `json:"readyInterval"`
	StatsInterval    protocol.Duration `json:"statsInterval"`
	ClustersInterval protocol.Duration `json:"clustersInterval"`
	ConfigInterval   protocol.Duration `json:"configInterval"`
	CertsInterval    protocol.Duration `json:"certsInterval"`
}

// DeliverConfig 对应 deliver.*（下发层 §1.3）。
type DeliverConfig struct {
	Mode string    `json:"mode"` // xds | static
	XDS  XDSConfig `json:"xds"`
}

// XDSConfig 对应 deliver.xds.*。TLS 字段为 P2 预留（下发层 §2.7），
// 文档列出、代码不加——加了就要实现；当前配置 tls 会因未知字段报错。
type XDSConfig struct {
	Listen       string `json:"listen"`
	NodeID       string `json:"nodeID"`
	NodeCluster  string `json:"nodeCluster"`
	AdminAddress string `json:"adminAddress"`
	// AckTimeout 复用 internal/protocol.Duration：与协议对象同一套
	// Go duration 字符串编解码（SD4），避免再造一套时长类型。
	AckTimeout protocol.Duration `json:"ackTimeout"`
}

// LoadFile 加载 esgw.yaml：读文件 → strict decode → 填默认值 → 校验。
// 文件读不到返回错误（serve 要求显式配置）；空文档 = 全默认值，合法。
func LoadFile(path string) (*Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	cfg := &Config{}
	if err := decode(content, cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// decode 解析单文档 YAML 并 strict decode（未知字段报错，SD4）。
// YAML→JSON 转换复用 S1 确立的 yaml.v3 Node→JSON 栈（做法同
// internal/protocol/load.go，理由见 260719 technical_design SD2）。
func decode(content []byte, cfg *Config) error {
	dec := yamlv3.NewDecoder(bytes.NewReader(content))
	var node yamlv3.Node
	err := dec.Decode(&node)
	if errors.Is(err, io.EOF) {
		return nil // 空文件 = 空文档 = 全默认值
	}
	if err != nil {
		return fmt.Errorf("YAML parse error: %w", err)
	}
	jsonDoc, err := yamlNodeToJSON(&node)
	if err != nil {
		return fmt.Errorf("YAML to JSON conversion failed: %w", err)
	}
	// esgw.yaml 为单文档：第二份文档存在即报错。
	var extra yamlv3.Node
	if err := dec.Decode(&extra); err == nil {
		return errors.New("esgw.yaml must be a single YAML document")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("YAML parse error: %w", err)
	}
	if string(jsonDoc) == "null" {
		return nil // 空文档（如 "---"）= 全默认值
	}
	jsonDec := json.NewDecoder(bytes.NewReader(jsonDoc))
	jsonDec.DisallowUnknownFields()
	if err := jsonDec.Decode(cfg); err != nil {
		return fmt.Errorf("strict decode failed: %w", err)
	}
	return nil
}

// applyDefaults 对未设置的字段填默认值（零值 = 未设置；esgw.yaml 无
// 需要区分「显式置空」与「未设置」的键）。
func (c *Config) applyDefaults() {
	if c.DataDir == "" {
		c.DataDir = DefaultDataDir
	}
	if c.Deliver.Mode == "" {
		c.Deliver.Mode = ModeXDS
	}
	x := &c.Deliver.XDS
	if x.Listen == "" {
		x.Listen = DefaultListen
	}
	if x.NodeID == "" {
		x.NodeID = DefaultNodeID
	}
	if x.NodeCluster == "" {
		x.NodeCluster = DefaultNodeCluster
	}
	if x.AdminAddress == "" {
		x.AdminAddress = DefaultAdminAddress
	}
	if x.AckTimeout.Duration == 0 {
		x.AckTimeout = protocol.Duration{Duration: DefaultAckTimeout}
	}
	if c.API.Listen == "" {
		c.API.Listen = DefaultAPIListen
	}
	if c.API.Topology == "" {
		c.API.Topology = DefaultTopology
	}
	if c.State.ReadyInterval.Duration == 0 {
		c.State.ReadyInterval = protocol.Duration{Duration: 10 * time.Second}
	}
	if c.State.StatsInterval.Duration == 0 {
		c.State.StatsInterval = protocol.Duration{Duration: 10 * time.Second}
	}
	if c.State.ClustersInterval.Duration == 0 {
		c.State.ClustersInterval = protocol.Duration{Duration: 15 * time.Second}
	}
	if c.State.ConfigInterval.Duration == 0 {
		c.State.ConfigInterval = protocol.Duration{Duration: time.Minute}
	}
	if c.State.CertsInterval.Duration == 0 {
		c.State.CertsInterval = protocol.Duration{Duration: 5 * time.Minute}
	}
}

// validate 结构层校验（dev_design 260720-1 §3.1 校验规则 1~4）。
func (c *Config) validate() error {
	if c.Deliver.Mode != ModeXDS && c.Deliver.Mode != ModeStatic {
		return fmt.Errorf("deliver.mode %q invalid (want %q | %q)", c.Deliver.Mode, ModeXDS, ModeStatic)
	}
	if err := validateListen(c.Deliver.XDS.Listen); err != nil {
		return fmt.Errorf("deliver.xds.listen: %w", err)
	}
	if err := validateAdminAddress(c.Deliver.XDS.AdminAddress); err != nil {
		return fmt.Errorf("deliver.xds.adminAddress: %w", err)
	}
	if c.Deliver.XDS.AckTimeout.Duration <= 0 {
		return fmt.Errorf("deliver.xds.ackTimeout must be a positive duration, got %q", c.Deliver.XDS.AckTimeout.String())
	}
	if err := validateAPIListen(c.API.Listen); err != nil {
		return fmt.Errorf("api.listen: %w", err)
	}
	switch c.API.Topology {
	case "standalone", "sidecar", "central":
	default:
		return fmt.Errorf("api.topology %q invalid (want standalone | sidecar | central)", c.API.Topology)
	}
	for name, interval := range map[string]time.Duration{
		"readyInterval": c.State.ReadyInterval.Duration, "statsInterval": c.State.StatsInterval.Duration,
		"clustersInterval": c.State.ClustersInterval.Duration, "configInterval": c.State.ConfigInterval.Duration,
		"certsInterval": c.State.CertsInterval.Duration,
	} {
		if interval <= 0 {
			return fmt.Errorf("state.%s must be a positive duration", name)
		}
	}
	return nil
}

func validateAPIListen(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("%q is not a valid host:port address: %v", listen, err)
	}
	if err := validatePort(port); err != nil {
		return err
	}
	if host == "" {
		return errors.New("host is empty; use an explicit address such as 127.0.0.1 or 0.0.0.0")
	}
	if host == "localhost" {
		return nil
	}
	if _, err := netip.ParseAddr(host); err != nil {
		return fmt.Errorf("host %q is not an IP literal", host)
	}
	return nil
}

// validateListen 监听安全硬校验（下发层 §2.7，A7 管理面侧）：listen 必须
// 可解析为 host:port 且 host 为 loopback。tls 本冲刺不存在（P2 预留），
// 故一切非 loopback listen 均拒绝。
func validateListen(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("%q is not a valid host:port address: %v", listen, err)
	}
	if err := validatePort(port); err != nil {
		return err
	}
	switch host {
	case "":
		return fmt.Errorf("%q listens on all interfaces: non-loopback listen requires tls (reserved for P2), use a loopback address such as %q", listen, DefaultListen)
	case "localhost":
		// 取舍：localhost 按 loopback 接受（本机解析语义稳定，拒绝它对
		// 同机部署不友好）；其余主机名无法静态判定，一律拒绝。
		return nil
	default:
		addr, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("host %q is not an IP literal (only loopback IPs and \"localhost\" are accepted)", host)
		}
		if !addr.IsLoopback() {
			return fmt.Errorf("%q is not a loopback address: non-loopback listen requires tls (reserved for P2)", listen)
		}
		return nil
	}
}

// validateAdminAddress 仅接受 unix:///<绝对路径> 或 <host>:<port> 两种形态（SD5）。
func validateAdminAddress(addr string) error {
	if rest, ok := strings.CutPrefix(addr, "unix:///"); ok {
		if rest == "" {
			return fmt.Errorf("%q has an empty socket path", addr)
		}
		return nil
	}
	// 显式拒绝其他 scheme（否则 "tcp://127.0.0.1:9901" 会被 SplitHostPort
	// 拆成 host "tcp://127.0.0.1" 而漏过）。
	if strings.Contains(addr, "://") {
		return fmt.Errorf("%q invalid (want unix:///<path> or host:port)", addr)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%q invalid (want unix:///<path> or host:port)", addr)
	}
	if host == "" {
		return fmt.Errorf("%q has an empty host", addr)
	}
	return validatePort(port)
}

// validatePort 校验端口为 1~65535 的数字。
func validatePort(port string) error {
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("invalid port %q (want 1-65535)", port)
	}
	return nil
}

// yamlNodeToJSON 把一个 YAML 文档节点转换为 JSON（做法同 internal/protocol/load.go）。
func yamlNodeToJSON(doc *yamlv3.Node) ([]byte, error) {
	v, err := yamlNodeToAny(doc)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func yamlNodeToAny(n *yamlv3.Node) (any, error) {
	switch n.Kind {
	case yamlv3.DocumentNode:
		return yamlNodeToAny(n.Content[0])
	case yamlv3.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		for i := 0; i < len(n.Content); i += 2 {
			var key string
			if err := n.Content[i].Decode(&key); err != nil {
				return nil, fmt.Errorf("mapping keys must be strings: %w", err)
			}
			v, err := yamlNodeToAny(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			m[key] = v
		}
		return m, nil
	case yamlv3.SequenceNode:
		s := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := yamlNodeToAny(c)
			if err != nil {
				return nil, err
			}
			s = append(s, v)
		}
		return s, nil
	case yamlv3.ScalarNode:
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		return v, nil
	case yamlv3.AliasNode:
		return nil, errors.New("YAML aliases are not supported")
	default:
		return nil, fmt.Errorf("unsupported YAML node kind %d", n.Kind)
	}
}
