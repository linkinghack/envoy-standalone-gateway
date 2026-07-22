package certstore

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/store"
)

func TestCreateGetListDeleteCertificate(t *testing.T) {
	t.Parallel()
	service, dataDir := newCertificateService(t)
	certificatePEM, privateKeyPEM := testPair(t, "console.example.com")
	created, err := service.Create(context.Background(), "console", certificatePEM, privateKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != "console" || created.Subject == "" || len(created.SANs) != 1 || created.CertificatePEM != certificatePEM {
		t.Fatalf("created = %+v", created)
	}
	keyInfo, err := os.Stat(filepath.Join(dataDir, "certs", "console", "tls.key"))
	if err != nil {
		t.Fatal(err)
	}
	if keyInfo.Mode().Perm() != 0o600 {
		t.Fatalf("private key mode = %o", keyInfo.Mode().Perm())
	}
	got, err := service.Get(context.Background(), "console")
	if err != nil || strings.Contains(got.CertificatePEM, "PRIVATE KEY") {
		t.Fatalf("get = %+v, %v", got, err)
	}
	listed, err := service.List(context.Background())
	if err != nil || len(listed) != 1 || listed[0].Name != "console" {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	if err := service.Delete(context.Background(), "console"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(context.Background(), "console"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted = %v", err)
	}
}

func TestRejectsMismatchedKeyAndReferencedDelete(t *testing.T) {
	t.Parallel()
	service, dataDir := newCertificateService(t)
	certificatePEM, privateKeyPEM := testPair(t, "api.example.com")
	_, otherKey := testPair(t, "other.example.com")
	if _, err := service.Create(context.Background(), "mismatch", certificatePEM, otherKey); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mismatched key = %v", err)
	}
	if _, err := service.Create(context.Background(), "api", certificatePEM, privateKeyPEM); err != nil {
		t.Fatal(err)
	}
	draft := `apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: https}
spec:
  port: 443
  protocol: HTTPS
  tls:
    certificates: [{ref: api}]
`
	if err := os.MkdirAll(filepath.Join(dataDir, "config.d"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "config.d", "listener.yaml"), []byte(draft), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := service.Get(context.Background(), "api")
	if err != nil || len(got.References) != 1 || got.References[0] != "Listener/https" {
		t.Fatalf("references = %+v, %v", got.References, err)
	}
	if err := service.Delete(context.Background(), "api"); !errors.Is(err, ErrReferenced) {
		t.Fatalf("delete referenced = %v", err)
	}
}

func newCertificateService(t *testing.T) (*Service, string) {
	t.Helper()
	dataDir := t.TempDir()
	durable, err := store.Open(filepath.Join(dataDir, "esgw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = durable.Close() })
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	return &Service{DataDir: dataDir, Store: durable, Now: func() time.Time { return now }}, dataDir
}

func testPair(t *testing.T, dnsName string) (string, string) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: dnsName}, DNSNames: []string{dnsName},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(365 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return string(certificatePEM), string(privateKeyPEM)
}
