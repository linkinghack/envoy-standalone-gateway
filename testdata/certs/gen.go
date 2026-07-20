//go:build ignore

// gen.go 生成 testdata/certs 下的测试证书（仅测试用，见同目录 README.md）。
//
// 用法（仓库根目录）：
//
//	go run testdata/certs/gen.go
//
// 产物：一个自签测试 CA（ca.crt/ca.key）与四张由该 CA 签发的服务器证书：
// www/blog/api（SAN 各为对应 example.com 域名）与 wildcard（SAN *.example.com，
// 通配例）。密钥每次运行随机生成；仓库提交的是运行产物，golden 快照只引用
// 证书路径与 SAN 提取结果，不依赖密钥内容。
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const outDir = "testdata/certs"

func main() {
	caKey, caCert := mustCA()
	writeCert(caCert, nil, "ca")

	servers := []struct{ name, dns string }{
		{"www", "www.example.com"},
		{"blog", "blog.example.com"},
		{"api", "api.example.com"},
		{"wildcard", "*.example.com"},
	}
	for i, s := range servers {
		key, cert := mustServer(caCert, caKey, s.dns, int64(i+2))
		writeCert(cert, key, s.name)
	}
	fmt.Println("testdata/certs: CA + 4 server certificates generated")
}

func mustCA() (*ecdsa.PrivateKey, *x509.Certificate) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(err)
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "esgw-test-ca"},
		NotBefore:             time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2046, 1, 1, 0, 0, 0, 0, time.UTC),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	must(err)
	cert, err := x509.ParseCertificate(der)
	must(err)
	return key, cert
}

func mustServer(ca *x509.Certificate, caKey *ecdsa.PrivateKey, dns string, serial int64) (*ecdsa.PrivateKey, *x509.Certificate) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(err)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: dns},
		DNSNames:     []string{dns},
		NotBefore:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2046, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &key.PublicKey, caKey)
	must(err)
	cert, err := x509.ParseCertificate(der)
	must(err)
	return key, cert
}

func writeCert(cert *x509.Certificate, key *ecdsa.PrivateKey, name string) {
	crtPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	must(os.WriteFile(filepath.Join(outDir, name+".crt"), crtPEM, 0o644))
	if key != nil {
		der, err := x509.MarshalECPrivateKey(key)
		must(err)
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
		must(os.WriteFile(filepath.Join(outDir, name+".key"), keyPEM, 0o600))
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
