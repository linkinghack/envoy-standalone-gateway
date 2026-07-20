package compile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// TestS1PassF2 是验收用例（A5/T3 验收）：S1 演练配置过 F2 零错误。
// 协议仓库中的 S1 YAML 引用 /etc/esgw/certs/ 下不存在的证书，此处动态生成
// 配对的测试自签证书（t.TempDir()，不提交私钥）并把证书路径替换进去。
func TestS1PassF2(t *testing.T) {
	dir := t.TempDir()
	writeSelfSignedCert(t, dir, "www", "www.example.com")
	writeSelfSignedCert(t, dir, "blog", "blog.example.com")

	raw, err := os.ReadFile("../protocol/testdata/s1/exercise.yaml")
	if err != nil {
		t.Fatalf("read S1 exercise: %v", err)
	}
	yamlDoc := strings.ReplaceAll(string(raw), "/etc/esgw/certs/", dir+"/")
	if err := os.WriteFile(filepath.Join(dir, "exercise.yaml"), []byte(yamlDoc), 0o600); err != nil {
		t.Fatal(err)
	}

	cs, loadErrs := protocol.LoadDir(dir)
	if len(loadErrs) != 0 {
		t.Fatalf("S1 load errors: %v", loadErrs)
	}
	_, errs := link(cs, Options{Mode: ModeStatic}, defaultCertVerifier())
	assertNoErrs(t, errs)

	// T5 验收：F1~F6 全流水线（两模式）产出合法 IR（每次重新加载，避免共享默认填充状态）。
	for _, mode := range []Mode{ModeStatic, ModeXDS} {
		fresh, loadErrs := protocol.LoadDir(dir)
		if len(loadErrs) != 0 {
			t.Fatalf("S1 reload errors: %v", loadErrs)
		}
		out, cerrs := Compile(fresh, Options{Mode: mode})
		assertNoErrs(t, cerrs)
		if out == nil || out.Version == "" {
			t.Fatalf("S1 %s: IR incomplete", mode)
		}
		if verrs := validateIR(out); hasErrors(verrs) {
			t.Fatalf("S1 %s: IR failed re-validation:\n%s", mode, formatErrs(verrs))
		}
	}
}

// TestS2PassF2 是验收用例：S2 演练配置过 F2 零错误。
// 协议仓库中的 S2 YAML 省略了 https Listener 与 user-svc / user-svc-canary /
// auth-svc 三个 Upstream（协议 §8.2 原文标注"同构，略"），此处补齐同构对象。
func TestS2PassF2(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeSelfSignedCert(t, dir, "api", "api.example.com")

	raw, err := os.ReadFile("../protocol/testdata/s2/exercise.yaml")
	if err != nil {
		t.Fatalf("read S2 exercise: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "exercise.yaml"), raw, 0o600); err != nil {
		t.Fatal(err)
	}

	supplement := fmt.Sprintf(`
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: https}
spec:
  port: 443
  protocol: HTTPS
  tls:
    certificates:
      - {certFile: %s, keyFile: %s}
---
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: user-svc}
spec:
  endpoints: [{address: 10.0.0.1, port: 8080}]
---
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: user-svc-canary}
spec:
  endpoints: [{address: 10.0.0.2, port: 8080}]
---
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: auth-svc}
spec:
  endpoints: [{address: 10.0.0.3, port: 8080}]
`, certFile, keyFile)
	if err := os.WriteFile(filepath.Join(dir, "supplement.yaml"), []byte(supplement), 0o600); err != nil {
		t.Fatal(err)
	}

	cs, loadErrs := protocol.LoadDir(dir)
	if len(loadErrs) != 0 {
		t.Fatalf("S2 load errors: %v", loadErrs)
	}
	_, errs := link(cs, Options{Mode: ModeStatic}, defaultCertVerifier())
	assertNoErrs(t, errs)

	// T5 验收：F1~F6 全流水线（两模式）产出合法 IR。
	for _, mode := range []Mode{ModeStatic, ModeXDS} {
		fresh, loadErrs := protocol.LoadDir(dir)
		if len(loadErrs) != 0 {
			t.Fatalf("S2 reload errors: %v", loadErrs)
		}
		out, cerrs := Compile(fresh, Options{Mode: mode})
		assertNoErrs(t, cerrs)
		if out == nil || out.Version == "" {
			t.Fatalf("S2 %s: IR incomplete", mode)
		}
		if verrs := validateIR(out); hasErrors(verrs) {
			t.Fatalf("S2 %s: IR failed re-validation:\n%s", mode, formatErrs(verrs))
		}
	}
}
