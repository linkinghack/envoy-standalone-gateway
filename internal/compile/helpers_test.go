package compile

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// testOrigin 返回测试对象的统一来源。
func testOrigin() protocol.Origin {
	return protocol.Origin{File: "test.yaml", DocIndex: 0}
}

// newListener 构造一个无 TLS 的 Listener。
func newListener(name string, port int32, proto protocol.ListenerProtocol) *protocol.Listener {
	return &protocol.Listener{
		APIVersion: protocol.APIVersionV1Alpha1,
		Kind:       protocol.KindListener,
		Metadata:   protocol.ObjectMeta{Name: name},
		Spec:       protocol.ListenerSpec{Port: port, Protocol: proto},
		Origin:     testOrigin(),
	}
}

// newTLSListener 构造一个带单证书条目的 HTTPS Listener（证书路径由调用方指定）。
func newTLSListener(name string, port int32, proto protocol.ListenerProtocol, certFile, keyFile string) *protocol.Listener {
	l := newListener(name, port, proto)
	l.Spec.TLS = &protocol.ListenerTLS{
		Certificates: []protocol.Certificate{{CertFile: certFile, KeyFile: keyFile}},
	}
	return l
}

// newUpstream 构造一个静态端点 Upstream。
func newUpstream(name string) *protocol.Upstream {
	return &protocol.Upstream{
		APIVersion: protocol.APIVersionV1Alpha1,
		Kind:       protocol.KindUpstream,
		Metadata:   protocol.ObjectMeta{Name: name},
		Spec: protocol.UpstreamSpec{
			Endpoints: []protocol.Endpoint{{Address: "127.0.0.1", Port: 8080}},
		},
		Origin: testOrigin(),
	}
}

// newHTTPRoute 构造一个 HTTP 形态 Route：单条 catch-all rule 转发到 upstream。
func newHTTPRoute(name string, listeners, hostnames []string, upstream string) *protocol.Route {
	return &protocol.Route{
		APIVersion: protocol.APIVersionV1Alpha1,
		Kind:       protocol.KindRoute,
		Metadata:   protocol.ObjectMeta{Name: name},
		Spec: protocol.RouteSpec{
			Listeners: listeners,
			Hostnames: hostnames,
			Rules: []protocol.Rule{{
				Match:    protocol.RuleMatch{Path: &protocol.PathMatch{Prefix: "/"}},
				Backends: []protocol.BackendRef{{Upstream: upstream}},
			}},
		},
		Origin: testOrigin(),
	}
}

// newForwardRoute 构造一个 L4 forward 形态 Route。
func newForwardRoute(name string, listeners []string, upstream string, sniHosts ...string) *protocol.Route {
	return &protocol.Route{
		APIVersion: protocol.APIVersionV1Alpha1,
		Kind:       protocol.KindRoute,
		Metadata:   protocol.ObjectMeta{Name: name},
		Spec: protocol.RouteSpec{
			Listeners: listeners,
			Forward:   &protocol.Forward{Upstream: upstream, SNIHosts: sniHosts},
		},
		Origin: testOrigin(),
	}
}

// newPolicy 构造一个 Policy 对象。
func newPolicy(name string, spec protocol.PolicySpec) *protocol.Policy {
	return &protocol.Policy{
		APIVersion: protocol.APIVersionV1Alpha1,
		Kind:       protocol.KindPolicy,
		Metadata:   protocol.ObjectMeta{Name: name},
		Spec:       spec,
		Origin:     testOrigin(),
	}
}

// ipAccessSpec 是一个 ipAccess 策略 spec。
func ipAccessSpec() protocol.PolicySpec {
	return protocol.PolicySpec{IPAccess: &protocol.IPAccessPolicy{Allow: []string{"10.0.0.0/8"}}}
}

// rateLimitSpec 是一个 rateLimit 策略 spec。
func rateLimitSpec() protocol.PolicySpec {
	return protocol.PolicySpec{RateLimit: &protocol.RateLimitPolicy{Requests: 100, Unit: protocol.RateLimitUnitMinute}}
}

// okVerifier 是全部通过的假证书检查器（证书规则测试之外的用例使用）。
func okVerifier() certVerifier {
	return certVerifier{
		verifyKeyPair: func(_, _ string) error { return nil },
		verifyCAFile:  func(string) error { return nil },
	}
}

// fakeEndpointSource 是一个非 nil 的假 EndpointSource。
type fakeEndpointSource struct{}

// InitialEndpoints 实现 EndpointSource。
func (fakeEndpointSource) InitialEndpoints(protocol.KubernetesServiceSource) ([]protocol.Endpoint, error) {
	return nil, nil
}

// wantErr 描述一条期望的编译错误。
type wantErr struct {
	kind protocol.Kind // 空 = 不断言
	name string        // 空 = 不断言
	path string        // SourceRef.Path；空 = 不断言
	msg  string        // Message 子串
}

// linkErrs 对 cs 跑 F2（假证书检查器），返回全部错误。
func linkErrs(cs *protocol.ConfigSet) []CompileError {
	_, errs := link(cs, Options{Mode: ModeStatic}, okVerifier())
	return errs
}

// assertLinkErrs 断言 link 产出的错误与 wants 一一对应（Stage 恒为 link）。
func assertLinkErrs(t *testing.T, errs []CompileError, wants []wantErr) {
	t.Helper()
	if len(errs) != len(wants) {
		t.Fatalf("want %d errors, got %d:\n%s", len(wants), len(errs), formatErrs(errs))
	}
	for i, w := range wants {
		e := errs[i]
		if e.Stage != StageLink {
			t.Fatalf("err[%d].Stage = %q, want link", i, e.Stage)
		}
		if e.Severity != SeverityError {
			t.Fatalf("err[%d].Severity = %q, want Error", i, e.Severity)
		}
		if w.kind != "" && e.Source.Kind != w.kind {
			t.Fatalf("err[%d].Source.Kind = %q, want %q", i, e.Source.Kind, w.kind)
		}
		if w.name != "" && e.Source.Name != w.name {
			t.Fatalf("err[%d].Source.Name = %q, want %q", i, e.Source.Name, w.name)
		}
		if w.path != "" && e.Source.Path != w.path {
			t.Fatalf("err[%d].Source.Path = %q, want %q", i, e.Source.Path, w.path)
		}
		if w.msg != "" && !strings.Contains(e.Message, w.msg) {
			t.Fatalf("err[%d].Message = %q, want substring %q", i, e.Message, w.msg)
		}
	}
}

// assertNoErrs 断言 F2 零错误（恰好合法的边界例）。
func assertNoErrs(t *testing.T, errs []CompileError) {
	t.Helper()
	if len(errs) != 0 {
		t.Fatalf("want no errors, got:\n%s", formatErrs(errs))
	}
}

func formatErrs(errs []CompileError) string {
	var b strings.Builder
	for _, e := range errs {
		b.WriteString("  " + e.Error() + "\n")
	}
	return b.String()
}

// writeSelfSignedCert 在 dir 下生成测试自签证书/私钥对（base.crt / base.key），
// 返回文件路径。动态生成避免向仓库提交私钥。
func writeSelfSignedCert(t *testing.T, dir, base string, dnsNames ...string) (certFile, keyFile string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	cn := base
	if len(dnsNames) > 0 {
		cn = dnsNames[0]
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		DNSNames:              dnsNames,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certFile = filepath.Join(dir, base+".crt")
	keyFile = filepath.Join(dir, base+".key")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}
