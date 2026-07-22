package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// User is a local management account. PasswordHash is never exposed by M-API.
type User struct {
	ID                int64
	Username          string
	PasswordHash      string
	CreatedAt         time.Time
	PasswordUpdatedAt time.Time
}

// Session is a server-side management session. TokenHash contains the SHA-256
// digest of the browser token, never the bearer token itself.
type Session struct {
	TokenHash         string
	UserID            int64
	Username          string
	CreatedAt         time.Time
	LastActiveAt      time.Time
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
	IP                string
	UserAgent         string
}

// UserCount reports the number of local users.
func (s *Store) UserCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

// CreateUser inserts a local account.
func (s *Store) CreateUser(ctx context.Context, user User) (User, error) {
	if user.Username == "" || user.PasswordHash == "" {
		return User{}, errors.New("username and password hash are required")
	}
	now := time.Now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	if user.PasswordUpdatedAt.IsZero() {
		user.PasswordUpdatedAt = user.CreatedAt
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO users
		(username,password_hash,created_at,password_updated_at) VALUES(?,?,?,?)`,
		user.Username, user.PasswordHash, formatTime(user.CreatedAt), formatTime(user.PasswordUpdatedAt))
	if err != nil {
		return User{}, err
	}
	user.ID, err = result.LastInsertId()
	return user, err
}

// UserByUsername loads a local account by its exact username.
func (s *Store) UserByUsername(ctx context.Context, username string) (User, error) {
	var user User
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,username,password_hash,created_at,password_updated_at
		FROM users WHERE username=?`, username).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &created, &updated)
	if err != nil {
		return User{}, err
	}
	if user.CreatedAt, err = parseStoreTime(created); err != nil {
		return User{}, fmt.Errorf("parse user created_at: %w", err)
	}
	user.PasswordUpdatedAt, err = parseStoreTimeDefault(updated, user.CreatedAt)
	if err != nil {
		return User{}, fmt.Errorf("parse password_updated_at: %w", err)
	}
	return user, nil
}

// CreateSession inserts a session whose key is already a token hash.
func (s *Store) CreateSession(ctx context.Context, session Session) error {
	if session.TokenHash == "" || session.UserID <= 0 {
		return errors.New("session token hash and user id are required")
	}
	return withSessionDefaults(&session, func() error {
		_, err := s.db.ExecContext(ctx, `INSERT INTO sessions
			(token,user_id,created_at,last_active_at,expires_at,absolute_expires_at,ip,user_agent)
			VALUES(?,?,?,?,?,?,?,?)`, session.TokenHash, session.UserID, formatTime(session.CreatedAt),
			formatTime(session.LastActiveAt), formatTime(session.ExpiresAt), formatTime(session.AbsoluteExpiresAt),
			session.IP, session.UserAgent)
		return err
	})
}

// SessionByTokenHash loads a session and its current username.
func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	var session Session
	var created, active, expires, absolute string
	err := s.db.QueryRowContext(ctx, `SELECT s.token,s.user_id,u.username,s.created_at,
		s.last_active_at,s.expires_at,s.absolute_expires_at,s.ip,s.user_agent
		FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token=?`, tokenHash).Scan(
		&session.TokenHash, &session.UserID, &session.Username, &created, &active, &expires,
		&absolute, &session.IP, &session.UserAgent)
	if err != nil {
		return Session{}, err
	}
	if session.CreatedAt, err = parseStoreTime(created); err != nil {
		return Session{}, err
	}
	if session.LastActiveAt, err = parseStoreTimeDefault(active, session.CreatedAt); err != nil {
		return Session{}, err
	}
	if session.ExpiresAt, err = parseStoreTime(expires); err != nil {
		return Session{}, err
	}
	if session.AbsoluteExpiresAt, err = parseStoreTimeDefault(absolute, session.ExpiresAt); err != nil {
		return Session{}, err
	}
	return session, nil
}

// RotateSession atomically replaces a token hash and updates activity/expiry.
func (s *Store) RotateSession(ctx context.Context, oldHash, newHash string, activeAt, expiresAt time.Time) error {
	if oldHash == "" || newHash == "" {
		return errors.New("old and new token hashes are required")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET token=?,last_active_at=?,expires_at=? WHERE token=?`,
		newHash, formatTime(activeAt), formatTime(expiresAt), oldHash)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// TouchSession updates activity without rotating the token.
func (s *Store) TouchSession(ctx context.Context, tokenHash string, activeAt time.Time) error {
	result, err := s.db.ExecContext(ctx, "UPDATE sessions SET last_active_at=? WHERE token=?", formatTime(activeAt), tokenHash)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteSession revokes one session. Missing sessions are idempotently ignored.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token=?", tokenHash)
	return err
}

// UpdatePasswordAndRevokeSessions changes a password and revokes every session
// except keepTokenHash. An empty keepTokenHash revokes them all.
func (s *Store) UpdatePasswordAndRevokeSessions(ctx context.Context, userID int64, passwordHash, keepTokenHash string, updatedAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, "UPDATE users SET password_hash=?,password_updated_at=? WHERE id=?", passwordHash, formatTime(updatedAt), userID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return sql.ErrNoRows
	}
	if keepTokenHash == "" {
		_, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=?", userID)
	} else {
		_, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id=? AND token<>?", userID, keepTokenHash)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteExpiredSessions removes idle or absolutely expired sessions.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	stamp := formatTime(now)
	result, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at<=? OR absolute_expires_at<=?", stamp, stamp)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func withSessionDefaults(session *Session, fn func() error) error {
	if session.CreatedAt.IsZero() || session.ExpiresAt.IsZero() || session.AbsoluteExpiresAt.IsZero() {
		return errors.New("session timestamps are required")
	}
	if session.LastActiveAt.IsZero() {
		session.LastActiveAt = session.CreatedAt
	}
	return fn()
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func parseStoreTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }

func parseStoreTimeDefault(value string, fallback time.Time) (time.Time, error) {
	if value == "" {
		return fallback, nil
	}
	return parseStoreTime(value)
}
