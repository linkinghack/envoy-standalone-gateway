// Package golden 是仓库根 testdata/ 下 golden 快照用例的测试入口
// （编译层 §8.1 测试策略；Sprint 260719 T7）。
//
// 布局：
//
//	testdata/<case>/input/*.yaml     协议 YAML 输入
//	testdata/<case>/want-static.yaml static 模式产物快照（static.Render）
//	testdata/<case>/want-xds.json    xds 模式产物快照（deliver.SnapshotJSON）
//	testdata/bootstrap-xds/esgw.yaml          接入 bootstrap 输入（S2 T4）
//	testdata/bootstrap-xds/want-bootstrap.yaml 接入 bootstrap 产物快照（xds.RenderBootstrap）
//	testdata/errors/<case>/input/*.yaml      错误用例输入
//	testdata/errors/<case>/want-errors.json  错误集合快照
//
// 刷新：go test ./internal/golden -update（make golden-update）。
//
// 确定性（验收 A6 红线）：输入 YAML 中的证书路径是相对仓库根的相对路径
// （testdata/certs/...），测试经 chdir 到仓库根后 LoadDir 相对目录，产物与
// 错误中的文件路径均为相对仓库根的稳定形态，不含机器特定路径。
package golden

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/linkinghack/envoy-standalone-gateway/internal/compile"
	"github.com/linkinghack/envoy-standalone-gateway/internal/config"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver/static"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver/xds"
	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

var update = flag.Bool("update", false, "refresh golden snapshots under testdata/")

// goodCases 是期望编译零错误的 golden 用例（testdata/<case>/）。
var goodCases = []string{
	"s1",                       // 协议 §8.1 多域名 TLS 反代
	"s2",                       // 协议 §8.2 API 网关（含同构补全）
	"l4",                       // P1 TCP/TLS passthrough/UDP
	"extauth",                  // P1 HTTP/gRPC ext_authz + route disable
	"ipaccess",                 // P1 allow/deny RBAC + dynamic-key local rate limit
	"mtls-circuit",             // P1 downstream client CA + upstream circuit breakers
	"patch-merge",              // 协议 §7.1 envoyPatch merge 形态
	"patch-jsonpatch",          // 协议 §7.1 envoyPatch jsonPatch 形态
	"envoy-resources",          // 协议 §7.2 EnvoyResources（allowOverride 默认 false）
	"envoy-resources-override", // 协议 §7.2 allowOverride: true 替换态
}

// repoRoot 由本测试文件位置推导仓库根（internal/golden → 上两级）。
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

// chdirRepoRoot 把进程工作目录切到仓库根：证书等相对路径按仓库根解析，
// 快照内容因此与机器无关（A6）。
func chdirRepoRoot(t *testing.T) {
	t.Helper()
	t.Chdir(repoRoot(t))
}

func TestGolden(t *testing.T) {
	for _, c := range goodCases {
		t.Run(c, func(t *testing.T) {
			chdirRepoRoot(t)
			dir := filepath.Join("testdata", c, "input")

			// static 模式产物。Compile 会原地填充 ConfigSet 默认值，
			// 两种模式各自重新 LoadDir（CLI 同款处理）。
			cs, loadErrs := protocol.LoadDir(dir)
			if len(loadErrs) != 0 {
				t.Fatalf("load errors: %v", loadErrs)
			}
			staticIR, cerrs := compile.Compile(cs, compile.Options{Mode: compile.ModeStatic})
			assertNoCompileErrs(t, cerrs)
			staticYAML, err := static.Render(staticIR)
			if err != nil {
				t.Fatalf("render static: %v", err)
			}
			compareGolden(t, filepath.Join("testdata", c, "want-static.yaml"), staticYAML)

			// xds 模式产物。
			cs, loadErrs = protocol.LoadDir(dir)
			if len(loadErrs) != 0 {
				t.Fatalf("load errors: %v", loadErrs)
			}
			xdsIR, cerrs := compile.Compile(cs, compile.Options{Mode: compile.ModeXDS})
			assertNoCompileErrs(t, cerrs)
			snapJSON, err := deliver.SnapshotJSON(xdsIR)
			if err != nil {
				t.Fatalf("snapshot json: %v", err)
			}
			compareGolden(t, filepath.Join("testdata", c, "want-xds.json"), snapJSON)
		})
	}
}

// errEntry 是 want-errors.json 中一条错误的规范化形态
// （stage/severity + SourceRef 全字段 + message；loader 错误 stage=schema，
// 与 compile.StageSchema 对齐）。
type errEntry struct {
	Stage    string `json:"stage"`
	Severity string `json:"severity"`
	File     string `json:"file,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Name     string `json:"name,omitempty"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
}

func TestGoldenErrors(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "testdata", "errors"))
	if err != nil {
		t.Fatalf("read testdata/errors: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			chdirRepoRoot(t)
			dir := filepath.Join("testdata", "errors", e.Name(), "input")
			cs, loadErrs := protocol.LoadDir(dir)

			var got []errEntry
			for _, le := range loadErrs {
				got = append(got, errEntry{
					Stage:    string(compile.StageSchema),
					Severity: string(compile.SeverityError),
					File:     le.Origin.File,
					Message:  le.Message,
				})
			}
			var cerrs []compile.CompileError
			if len(loadErrs) == 0 {
				_, cerrs = compile.Compile(cs, compile.Options{Mode: compile.ModeStatic})
			}
			for _, ce := range cerrs {
				got = append(got, errEntry{
					Stage:    string(ce.Stage),
					Severity: string(ce.Severity),
					File:     ce.Source.File,
					Kind:     string(ce.Source.Kind),
					Name:     ce.Source.Name,
					Path:     ce.Source.Path,
					Message:  ce.Message,
				})
			}
			if len(got) == 0 {
				t.Fatalf("error case %s compiled cleanly; want at least one error", e.Name())
			}

			sort.SliceStable(got, func(i, j int) bool {
				if got[i].Stage != got[j].Stage {
					return got[i].Stage < got[j].Stage
				}
				return got[i].Message < got[j].Message
			})
			data, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, '\n')
			compareGolden(t, filepath.Join("testdata", "errors", e.Name(), "want-errors.json"), data)
		})
	}
}

// TestGoldenBootstrap 是接入 bootstrap 的 golden 快照（Sprint 260720 T4）：
// testdata/bootstrap-xds/esgw.yaml 经 config.LoadFile → xds.RenderBootstrap
// 渲染，产物快照为 want-bootstrap.yaml。刷新同 -update 约定。
func TestGoldenBootstrap(t *testing.T) {
	chdirRepoRoot(t)
	cfg, err := config.LoadFile(filepath.Join("testdata", "bootstrap-xds", "esgw.yaml"))
	if err != nil {
		t.Fatalf("load esgw.yaml: %v", err)
	}
	b, err := xds.RenderBootstrap(xds.BootstrapOpts{
		NodeID:       cfg.Deliver.XDS.NodeID,
		NodeCluster:  cfg.Deliver.XDS.NodeCluster,
		XDSListen:    cfg.Deliver.XDS.Listen,
		AdminAddress: cfg.Deliver.XDS.AdminAddress,
	})
	if err != nil {
		t.Fatalf("render bootstrap: %v", err)
	}
	compareGolden(t, filepath.Join("testdata", "bootstrap-xds", "want-bootstrap.yaml"), b)
}

// compareGolden 比对或（-update 时）刷新 golden 文件。
func compareGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("update golden %s: %v", path, err)
		}
		t.Logf("updated %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run `make golden-update` to create)", path, err)
	}
	if string(want) != string(got) {
		t.Errorf("golden mismatch: %s (run `make golden-update` to refresh; diff must be reviewed)", path)
	}
}

func assertNoCompileErrs(t *testing.T, errs []compile.CompileError) {
	t.Helper()
	for _, e := range errs {
		if e.Severity == compile.SeverityError {
			t.Fatalf("compile errors:\n%v", errs)
		}
	}
}
