package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/linkinghack/envoy-standalone-gateway/internal/certstore"
)

// CertificateService is the HTTP-facing managed certificate contract.
type CertificateService interface {
	Create(context.Context, string, string, string) (certstore.Certificate, error)
	Get(context.Context, string) (certstore.Certificate, error)
	List(context.Context) ([]certstore.Certificate, error)
	Delete(context.Context, string) error
}

// CertificateAPI adapts the managed certificate store to HTTP.
type CertificateAPI struct{ Certificates CertificateService }

// Handlers returns all managed certificate operation adapters.
func (a *CertificateAPI) Handlers() map[string]OperationHandler {
	return map[string]OperationHandler{
		"listCertificates":  a.list,
		"createCertificate": a.create,
		"getCertificate":    a.get,
		"deleteCertificate": a.delete,
	}
}

type createCertificateRequest struct {
	Name           string `json:"name"`
	CertificatePEM string `json:"certificatePem"`
	PrivateKeyPEM  string `json:"privateKeyPem"`
}

func (a *CertificateAPI) list(w http.ResponseWriter, r *http.Request) {
	if !a.configured(w) {
		return
	}
	items, err := a.Certificates.List(r.Context())
	if err != nil {
		writeCertificateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (a *CertificateAPI) create(w http.ResponseWriter, r *http.Request) {
	if !a.configured(w) {
		return
	}
	var request createCertificateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.Name == "" || request.CertificatePEM == "" || request.PrivateKeyPEM == "" {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "name, certificatePem and privateKeyPem are required")
		return
	}
	certificate, err := a.Certificates.Create(r.Context(), request.Name, request.CertificatePEM, request.PrivateKeyPEM)
	if err != nil {
		writeCertificateError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, certificate)
}

func (a *CertificateAPI) get(w http.ResponseWriter, r *http.Request) {
	if !a.configured(w) {
		return
	}
	certificate, err := a.Certificates.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		writeCertificateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, certificate)
}

func (a *CertificateAPI) delete(w http.ResponseWriter, r *http.Request) {
	if !a.configured(w) {
		return
	}
	if err := a.Certificates.Delete(r.Context(), r.PathValue("name")); err != nil {
		writeCertificateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *CertificateAPI) configured(w http.ResponseWriter) bool {
	if a == nil || a.Certificates == nil {
		writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "certificate management is unavailable")
		return false
	}
	return true
}

func writeCertificateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, certstore.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "certificate not found")
	case errors.Is(err, certstore.ErrExists):
		writeError(w, http.StatusConflict, "CONFLICT", "certificate already exists")
	case errors.Is(err, certstore.ErrReferenced):
		writeError(w, http.StatusConflict, "CONFLICT", "certificate is referenced by the draft")
	case errors.Is(err, certstore.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid certificate or private key")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal server error")
	}
}
