// Package certstore owns managed certificate files and their SQLite index.
package certstore

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/conf"
	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
	"github.com/linkinghack/envoy-standalone-gateway/internal/store"
)

var (
	// ErrNotFound means the managed certificate does not exist.
	ErrNotFound = errors.New("managed certificate not found")
	// ErrExists means a managed certificate already uses the name.
	ErrExists = errors.New("managed certificate already exists")
	// ErrReferenced prevents deletion of a certificate used by the draft.
	ErrReferenced = errors.New("managed certificate is referenced by the draft")
	// ErrInvalid means the supplied name, certificate or key is invalid.
	ErrInvalid = errors.New("invalid managed certificate")
)

// Certificate is public managed certificate data. Private key material is
// intentionally absent from this type and from every read path.
type Certificate struct {
	Name           string    `json:"name"`
	Subject        string    `json:"subject"`
	SANs           []string  `json:"sans"`
	NotAfter       time.Time `json:"notAfter"`
	UpdatedAt      time.Time `json:"updatedAt"`
	CertificatePEM string    `json:"certificatePem"`
	References     []string  `json:"references"`
}

// Service persists certificate/key pairs under data-dir/certs.
type Service struct {
	DataDir string
	Store   *store.Store
	Now     func() time.Time
}

// Create validates and atomically installs a certificate/key pair.
func (s *Service) Create(ctx context.Context, name, certificatePEM, privateKeyPEM string) (Certificate, error) {
	if err := s.validate(); err != nil {
		return Certificate{}, err
	}
	if err := protocol.ValidateName(name); err != nil {
		return Certificate{}, fmt.Errorf("%w: invalid certificate name: %v", ErrInvalid, err)
	}
	leaf, err := parsePair([]byte(certificatePEM), []byte(privateKeyPEM))
	if err != nil {
		return Certificate{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	root := filepath.Join(s.DataDir, "certs")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Certificate{}, fmt.Errorf("create certificate directory: %w", err)
	}
	final := filepath.Join(root, name)
	if _, err := os.Stat(final); err == nil {
		return Certificate{}, ErrExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return Certificate{}, err
	}
	if _, err := s.Store.GetCertificate(ctx, name); err == nil {
		return Certificate{}, ErrExists
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Certificate{}, err
	}
	stage, err := os.MkdirTemp(root, ".stage-")
	if err != nil {
		return Certificate{}, fmt.Errorf("create certificate staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := writeSynced(filepath.Join(stage, "tls.crt"), []byte(certificatePEM), 0o644); err != nil {
		return Certificate{}, err
	}
	if err := writeSynced(filepath.Join(stage, "tls.key"), []byte(privateKeyPEM), 0o600); err != nil {
		return Certificate{}, err
	}
	if err := os.Rename(stage, final); err != nil {
		return Certificate{}, fmt.Errorf("install certificate: %w", err)
	}
	metadata := metadataFromLeaf(name, leaf, s.now())
	if err := s.Store.UpsertCertificate(ctx, store.Certificate{
		Name: metadata.Name, Subject: metadata.Subject, SANs: metadata.SANs,
		NotAfter: metadata.NotAfter, UpdatedAt: metadata.UpdatedAt,
	}); err != nil {
		_ = os.RemoveAll(final)
		return Certificate{}, fmt.Errorf("index certificate: %w", err)
	}
	metadata.CertificatePEM = certificatePEM
	return metadata, nil
}

// Get returns public certificate data and current draft references.
func (s *Service) Get(ctx context.Context, name string) (Certificate, error) {
	if err := s.validate(); err != nil {
		return Certificate{}, err
	}
	metadata, err := s.Store.GetCertificate(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return Certificate{}, ErrNotFound
	}
	if err != nil {
		return Certificate{}, err
	}
	certificatePEM, err := os.ReadFile(filepath.Join(s.DataDir, "certs", name, "tls.crt"))
	if errors.Is(err, os.ErrNotExist) {
		return Certificate{}, ErrNotFound
	}
	if err != nil {
		return Certificate{}, fmt.Errorf("read managed certificate: %w", err)
	}
	references, err := s.references(name)
	if err != nil {
		return Certificate{}, err
	}
	return Certificate{
		Name: metadata.Name, Subject: metadata.Subject, SANs: metadata.SANs,
		NotAfter: metadata.NotAfter, UpdatedAt: metadata.UpdatedAt,
		CertificatePEM: string(certificatePEM), References: references,
	}, nil
}

// List returns public certificate data in stable name order.
func (s *Service) List(ctx context.Context) ([]Certificate, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	metadata, err := s.Store.ListCertificates(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Certificate, 0, len(metadata))
	for _, item := range metadata {
		certificate, err := s.Get(ctx, item.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, certificate)
	}
	return out, nil
}

// Delete removes an unreferenced managed certificate.
func (s *Service) Delete(ctx context.Context, name string) error {
	if err := s.validate(); err != nil {
		return err
	}
	if _, err := s.Store.GetCertificate(ctx, name); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	references, err := s.references(name)
	if err != nil {
		return err
	}
	if len(references) != 0 {
		return fmt.Errorf("%w: %s", ErrReferenced, strings.Join(references, ", "))
	}
	root := filepath.Join(s.DataDir, "certs")
	final := filepath.Join(root, name)
	trash := filepath.Join(root, fmt.Sprintf(".delete-%s-%d", name, s.now().UnixNano()))
	if err := os.Rename(final, trash); errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("stage certificate deletion: %w", err)
	}
	if err := s.Store.DeleteCertificate(ctx, name); err != nil {
		_ = os.Rename(trash, final)
		return err
	}
	if err := os.RemoveAll(trash); err != nil {
		return fmt.Errorf("remove deleted certificate files: %w", err)
	}
	return nil
}

func (s *Service) references(name string) ([]string, error) {
	draft, loadErrs, err := conf.LoadDraft(s.DataDir)
	if err != nil {
		return nil, fmt.Errorf("load draft certificate references: %w", err)
	}
	if len(loadErrs) != 0 {
		return nil, fmt.Errorf("load draft certificate references: %w", loadErrs[0])
	}
	if draft.Mode != conf.ModeAbstract || draft.Config == nil {
		return nil, nil
	}
	var references []string
	for _, listener := range draft.Config.Listeners {
		if listener.Spec.TLS == nil {
			continue
		}
		for _, certificate := range listener.Spec.TLS.Certificates {
			if certificate.Ref == name {
				references = append(references, "Listener/"+listener.Metadata.Name)
				break
			}
		}
	}
	sort.Strings(references)
	return references, nil
}

func (s *Service) validate() error {
	if s == nil || s.DataDir == "" || s.Store == nil {
		return errors.New("certificate service is not configured")
	}
	return nil
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func parsePair(certificatePEM, privateKeyPEM []byte) (*x509.Certificate, error) {
	pair, err := tls.X509KeyPair(certificatePEM, privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("certificate and private key do not form a valid pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, errors.New("certificate chain is empty")
	}
	block, _ := pem.Decode(certificatePEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("certificate PEM does not start with a CERTIFICATE block")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse leaf certificate: %w", err)
	}
	return leaf, nil
}

func metadataFromLeaf(name string, leaf *x509.Certificate, now time.Time) Certificate {
	sans := append([]string(nil), leaf.DNSNames...)
	for _, address := range leaf.IPAddresses {
		sans = append(sans, address.String())
	}
	sans = append(sans, leaf.EmailAddresses...)
	for _, uri := range leaf.URIs {
		sans = append(sans, uri.String())
	}
	sort.Strings(sans)
	return Certificate{Name: name, Subject: leaf.Subject.String(), SANs: sans, NotAfter: leaf.NotAfter.UTC(), UpdatedAt: now, References: []string{}}
}

func writeSynced(path string, content []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create managed certificate file: %w", err)
	}
	if _, err = file.Write(content); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("write managed certificate file: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close managed certificate file: %w", closeErr)
	}
	return nil
}
