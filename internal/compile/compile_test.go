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

// TestCompilePipeline 覆盖 Compile() 全流水线：F2 错误短路（不进 build）；
// F2 通过后 F1~F6 全流水线产出合法 IR（static 模式：route_config 内联、证书回写）。
func TestCompilePipeline(t *testing.T) {
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

	t.Run("full pipeline yields IR", func(t *testing.T) {
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
		out, errs := Compile(cs, Options{Mode: ModeStatic})
		assertNoErrs(t, errs)
		if out == nil {
			t.Fatal("IR = nil")
		}
		if len(out.Version) != 12 {
			t.Fatalf("Version = %q, want 12 hex chars", out.Version)
		}
		if len(out.Listeners) != 2 || out.Clusters["us/app"] == nil {
			t.Fatalf("unexpected IR collections: listeners=%d clusters=%d", len(out.Listeners), len(out.Clusters))
		}
		// static 形态：route_config 内联（IR.Routes 为空）、证书回写（IR.Secrets 为空）、端点内联。
		if len(out.Routes) != 0 || len(out.Secrets) != 0 || len(out.Endpoints) != 0 {
			t.Fatalf("static mode should inline: routes=%d secrets=%d endpoints=%d",
				len(out.Routes), len(out.Secrets), len(out.Endpoints))
		}
		if out.Bootstrap == nil || out.Bootstrap.GetNode().GetId() == "" {
			t.Fatal("Bootstrap skeleton missing")
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
