package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Certificate is the searchable metadata index for a managed certificate.
// Certificate and private-key contents remain filesystem-owned.
type Certificate struct {
	Name      string
	Subject   string
	SANs      []string
	NotAfter  time.Time
	UpdatedAt time.Time
}

// UpsertCertificate updates the managed certificate metadata index.
func (s *Store) UpsertCertificate(ctx context.Context, certificate Certificate) error {
	sans, err := json.Marshal(certificate.SANs)
	if err != nil {
		return err
	}
	if certificate.UpdatedAt.IsZero() {
		certificate.UpdatedAt = time.Now().UTC()
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO certificates(name,subject,sans_json,not_after,updated_at) VALUES(?,?,?,?,?)
ON CONFLICT(name) DO UPDATE SET subject=excluded.subject,sans_json=excluded.sans_json,
not_after=excluded.not_after,updated_at=excluded.updated_at`, certificate.Name, certificate.Subject,
		string(sans), formatOptionalTime(certificate.NotAfter), certificate.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

// GetCertificate returns indexed metadata for one managed certificate.
func (s *Store) GetCertificate(ctx context.Context, name string) (Certificate, error) {
	var certificate Certificate
	var sansJSON, notAfter, updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT name,subject,sans_json,COALESCE(not_after,''),updated_at
FROM certificates WHERE name=?`, name).Scan(&certificate.Name, &certificate.Subject, &sansJSON, &notAfter, &updatedAt)
	if err != nil {
		return Certificate{}, err
	}
	if err := json.Unmarshal([]byte(sansJSON), &certificate.SANs); err != nil {
		return Certificate{}, err
	}
	if notAfter != "" {
		certificate.NotAfter, err = time.Parse(time.RFC3339Nano, notAfter)
		if err != nil {
			return Certificate{}, err
		}
	}
	certificate.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	return certificate, err
}

// ListCertificates returns indexed metadata in stable name order.
func (s *Store) ListCertificates(ctx context.Context) ([]Certificate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name,subject,sans_json,COALESCE(not_after,''),updated_at
FROM certificates ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Certificate
	for rows.Next() {
		var certificate Certificate
		var sansJSON, notAfter, updatedAt string
		if err := rows.Scan(&certificate.Name, &certificate.Subject, &sansJSON, &notAfter, &updatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(sansJSON), &certificate.SANs); err != nil {
			return nil, err
		}
		if notAfter != "" {
			certificate.NotAfter, err = time.Parse(time.RFC3339Nano, notAfter)
			if err != nil {
				return nil, err
			}
		}
		certificate.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, certificate)
	}
	return out, rows.Err()
}

// DeleteCertificate removes one metadata record and reports absence via sql.ErrNoRows.
func (s *Store) DeleteCertificate(ctx context.Context, name string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM certificates WHERE name=?", name)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func formatOptionalTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

// IsCertificateNotFound standardizes certificate lookup absence.
func IsCertificateNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }
