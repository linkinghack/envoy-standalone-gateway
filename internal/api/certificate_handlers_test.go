package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/certstore"
)

func TestCertificateHTTPNeverReturnsPrivateKey(t *testing.T) {
	t.Parallel()
	certificates := &fakeCertificateService{}
	certificateAPI := &CertificateAPI{Certificates: certificates}
	server := newTestServer(t, nil, nil, certificateAPI.Handlers())
	cookie := bootstrapCookie(t, server)

	privateKey := "-----BEGIN PRIVATE KEY-----\nTOP-SECRET\n-----END PRIVATE KEY-----"
	response := apiRequest(t, server, http.MethodPost, "/api/v1/certs", map[string]string{
		"name": "api", "certificatePem": "PUBLIC CERTIFICATE", "privateKeyPem": privateKey,
	}, cookie)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"name":"api"`) {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "TOP-SECRET") || certificates.privateKey != privateKey {
		t.Fatalf("private key handling response=%s received=%q", response.Body.String(), certificates.privateKey)
	}
	response = apiRequest(t, server, http.MethodGet, "/api/v1/certs", nil, cookie)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "TOP-SECRET") {
		t.Fatalf("list = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodDelete, "/api/v1/certs/api", nil, cookie)
	if response.Code != http.StatusNoContent || !certificates.deleted {
		t.Fatalf("delete = %d %s", response.Code, response.Body.String())
	}
}

type fakeCertificateService struct {
	privateKey string
	deleted    bool
}

func (f *fakeCertificateService) Create(_ context.Context, name, certificatePEM, privateKeyPEM string) (certstore.Certificate, error) {
	f.privateKey = privateKeyPEM
	return certstore.Certificate{Name: name, SANs: []string{}, UpdatedAt: time.Now(), CertificatePEM: certificatePEM, References: []string{}}, nil
}

func (*fakeCertificateService) Get(context.Context, string) (certstore.Certificate, error) {
	return certstore.Certificate{Name: "api", SANs: []string{}, CertificatePEM: "PUBLIC CERTIFICATE", References: []string{}}, nil
}

func (f *fakeCertificateService) List(ctx context.Context) ([]certstore.Certificate, error) {
	certificate, err := f.Get(ctx, "api")
	return []certstore.Certificate{certificate}, err
}

func (f *fakeCertificateService) Delete(context.Context, string) error {
	f.deleted = true
	return nil
}
