package compile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

func TestManagedCertificateReferenceCompiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	managed := filepath.Join(root, "api")
	if err := os.MkdirAll(managed, 0o700); err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := writeSelfSignedCert(t, managed, "tls", "api.example.com")
	listener := newListener("https", 443, protocol.ProtocolHTTPS)
	listener.Spec.TLS = &protocol.ListenerTLS{Certificates: []protocol.Certificate{{Ref: "api"}}}
	config := &protocol.ConfigSet{Listeners: []*protocol.Listener{listener}}
	output, errs := Compile(config, Options{Mode: ModeStatic, ManagedCertificateDir: root})
	assertNoErrs(t, errs)
	if output == nil {
		t.Fatal("compile returned nil output")
	}
	resolved := listener.Spec.TLS.Certificates[0]
	if resolved.Ref != "api" || resolved.CertFile != certFile || resolved.KeyFile != keyFile {
		t.Fatalf("resolved certificate = %+v", resolved)
	}
}
