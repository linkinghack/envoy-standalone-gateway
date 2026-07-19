package compile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// TestCertificates 覆盖证书文件存在性、cert/key 配对（openssl 语义，经 crypto/tls）、
// ref 形态未实现错误与 clientCA 校验（编译层 §3 F2）。
func TestCertificates(t *testing.T) {
	t.Run("ref form not implemented", func(t *testing.T) {
		lis := newListener("https", 443, protocol.ProtocolHTTPS)
		lis.Spec.TLS = &protocol.ListenerTLS{
			Certificates: []protocol.Certificate{{Ref: "shop-example-com"}},
		}
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{lis}}
		assertLinkErrs(t, linkErrs(cs), []wantErr{
			{kind: protocol.KindListener, name: "https", path: "spec.tls.certificates[0].ref", msg: "not implemented in M0"},
		})
	})

	t.Run("missing cert files", func(t *testing.T) {
		dir := t.TempDir()
		lis := newTLSListener("https", 443, protocol.ProtocolHTTPS,
			filepath.Join(dir, "nope.crt"), filepath.Join(dir, "nope.key"))
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{lis}}
		_, errs := link(cs, Options{Mode: ModeStatic}, defaultCertVerifier())
		assertLinkErrs(t, errs, []wantErr{
			{kind: protocol.KindListener, name: "https", path: "spec.tls.certificates[0].certFile", msg: "nope.crt"},
		})
	})

	t.Run("cert/key pair mismatch", func(t *testing.T) {
		dir := t.TempDir()
		certA, _ := writeSelfSignedCert(t, dir, "a", "www.example.com")
		_, keyB := writeSelfSignedCert(t, dir, "b", "www.example.com")
		lis := newTLSListener("https", 443, protocol.ProtocolHTTPS, certA, keyB)
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{lis}}
		_, errs := link(cs, Options{Mode: ModeStatic}, defaultCertVerifier())
		assertLinkErrs(t, errs, []wantErr{
			{kind: protocol.KindListener, name: "https", path: "spec.tls.certificates[0].certFile", msg: "does not match"},
		})
	})

	t.Run("missing clientCA", func(t *testing.T) {
		dir := t.TempDir()
		cert, key := writeSelfSignedCert(t, dir, "a", "www.example.com")
		lis := newTLSListener("https", 443, protocol.ProtocolHTTPS, cert, key)
		lis.Spec.TLS.ClientCA = filepath.Join(dir, "ca.crt")
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{lis}}
		_, errs := link(cs, Options{Mode: ModeStatic}, defaultCertVerifier())
		assertLinkErrs(t, errs, []wantErr{
			{kind: protocol.KindListener, name: "https", path: "spec.tls.clientCA", msg: "ca.crt"},
		})
	})

	t.Run("unparseable clientCA", func(t *testing.T) {
		dir := t.TempDir()
		cert, key := writeSelfSignedCert(t, dir, "a", "www.example.com")
		caFile := filepath.Join(dir, "ca.crt")
		if err := os.WriteFile(caFile, []byte("not a pem"), 0o600); err != nil {
			t.Fatal(err)
		}
		lis := newTLSListener("https", 443, protocol.ProtocolHTTPS, cert, key)
		lis.Spec.TLS.ClientCA = caFile
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{lis}}
		_, errs := link(cs, Options{Mode: ModeStatic}, defaultCertVerifier())
		assertLinkErrs(t, errs, []wantErr{
			{kind: protocol.KindListener, name: "https", path: "spec.tls.clientCA", msg: "no PEM certificate found"},
		})
	})

	t.Run("boundary: valid pair and clientCA", func(t *testing.T) {
		dir := t.TempDir()
		cert, key := writeSelfSignedCert(t, dir, "a", "www.example.com")
		caCert, _ := writeSelfSignedCert(t, dir, "ca", "Example CA")
		lis := newTLSListener("https", 443, protocol.ProtocolHTTPS, cert, key)
		lis.Spec.TLS.ClientCA = caCert
		cs := &protocol.ConfigSet{Listeners: []*protocol.Listener{lis}}
		_, errs := link(cs, Options{Mode: ModeStatic}, defaultCertVerifier())
		assertNoErrs(t, errs)
	})
}

// TestCompileSkeleton 覆盖 Compile() 骨架：F2 错误短路（不进 build）；
// F2 通过后返回 build 阶段未实现占位错误。
func TestCompileSkeleton(t *testing.T) {
	t.Run("link errors short-circuit", func(t *testing.T) {
		cs := &protocol.ConfigSet{
			Routes:    []*protocol.Route{newHTTPRoute("r1", []string{"ghost"}, nil, "app")},
			Upstreams: []*protocol.Upstream{newUpstream("app")},
		}
		got, errs := Compile(cs, Options{Mode: ModeStatic})
		if got != nil {
			t.Fatalf("IR = %v, want nil", got)
		}
		if len(errs) != 1 || errs[0].Stage != StageLink {
			t.Fatalf("want exactly one link-stage error, got:\n%s", formatErrs(errs))
		}
	})

	t.Run("F2 pass yields build placeholder", func(t *testing.T) {
		dir := t.TempDir()
		cert, key := writeSelfSignedCert(t, dir, "www", "www.example.com")
		cs := &protocol.ConfigSet{
			Listeners: []*protocol.Listener{
				newTLSListener("https", 443, protocol.ProtocolHTTPS, cert, key),
				newListener("http", 80, protocol.ProtocolHTTP),
			},
			Routes: []*protocol.Route{
				newHTTPRoute("www", []string{"https"}, []string{"www.example.com"}, "app"),
			},
			Upstreams: []*protocol.Upstream{newUpstream("app")},
		}
		got, errs := Compile(cs, Options{Mode: ModeStatic})
		if got != nil {
			t.Fatalf("IR = %v, want nil (F3+ placeholder)", got)
		}
		if len(errs) != 1 || errs[0].Stage != StageBuild {
			t.Fatalf("want exactly one build-stage placeholder error, got:\n%s", formatErrs(errs))
		}
	})
}

// TestErrorModel 校验 CompileError / SourceRef 的可读输出。
func TestErrorModel(t *testing.T) {
	e := CompileError{
		Stage:    StageLink,
		Source:   SourceRef{File: "a.yaml", Kind: protocol.KindRoute, Name: "api", Path: "spec.rules[2].retry.on"},
		Message:  "boom",
		Severity: SeverityError,
	}
	want := "[link] a.yaml Route/api: spec.rules[2].retry.on: boom"
	if e.Error() != want {
		t.Fatalf("Error() = %q, want %q", e.Error(), want)
	}
	// 隐式对象（无文件）与空 Path 的呈现。
	r := SourceRef{Kind: protocol.KindGateway, Name: "default"}
	if got := r.String(); got != "<implicit> Gateway/default" {
		t.Fatalf("String() = %q", got)
	}
}
