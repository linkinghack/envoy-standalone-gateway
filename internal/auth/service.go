// Package auth implements local management authentication independently of HTTP.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/store"
)

const minPasswordLength = 12

var (
	// ErrUnauthenticated deliberately covers both unknown usernames and bad passwords.
	ErrUnauthenticated = errors.New("invalid username or password")
	// ErrBootstrapComplete indicates that a local user already exists.
	ErrBootstrapComplete = errors.New("initial bootstrap is already complete")
	// ErrBootstrapClosed indicates that the initial bootstrap window elapsed.
	ErrBootstrapClosed = errors.New("initial bootstrap window is closed")
	// ErrWeakPassword indicates that a password violates the length policy.
	ErrWeakPassword = errors.New("password must contain at least 12 characters")
	// ErrInvalidUsername indicates an unsafe or empty local username.
	ErrInvalidUsername = errors.New("username must be 1-64 visible characters without whitespace")
)

// Store is the durable subset required by the auth service.
type Store interface {
	UserCount(context.Context) (int, error)
	CreateUser(context.Context, store.User) (store.User, error)
	UserByUsername(context.Context, string) (store.User, error)
	CreateSession(context.Context, store.Session) error
	SessionByTokenHash(context.Context, string) (store.Session, error)
	RotateSession(context.Context, string, string, time.Time, time.Time) error
	TouchSession(context.Context, string, time.Time) error
	DeleteSession(context.Context, string) error
	UpdatePasswordAndRevokeSessions(context.Context, int64, string, string, time.Time) error
}

// Config controls session and password costs. Zero values receive production defaults.
type Config struct {
	StartedAt      time.Time
	BootstrapTTL   time.Duration
	IdleTTL        time.Duration
	AbsoluteTTL    time.Duration
	PasswordParams PasswordParams
	Random         io.Reader
	Now            func() time.Time
}

// Service implements bootstrap, login and revocable server-side sessions.
type Service struct {
	store             Store
	config            Config
	bootstrapDeadline time.Time
	dummyHash         string
	limiter           *loginLimiter
}

// Session is the safe user/session projection returned to API callers.
type Session struct {
	UserID    int64
	Username  string
	Roles     []string
	ExpiresAt time.Time
	Token     string
}

// BootstrapState describes whether unauthenticated setup remains available.
type BootstrapState struct {
	Required  bool
	ExpiresAt *time.Time
}

// New constructs an auth service and one cost-equivalent dummy password hash.
func New(durable Store, config Config) (*Service, error) {
	if durable == nil {
		return nil, errors.New("auth store is required")
	}
	config = config.withDefaults()
	dummyHash, err := hashPassword("esgw-invalid-account-password", config.PasswordParams, config.Random)
	if err != nil {
		return nil, err
	}
	return &Service{
		store: durable, config: config, bootstrapDeadline: config.StartedAt.Add(config.BootstrapTTL),
		dummyHash: dummyHash, limiter: newLoginLimiter(),
	}, nil
}

func (c Config) withDefaults() Config {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.StartedAt.IsZero() {
		c.StartedAt = c.Now().UTC()
	}
	if c.BootstrapTTL <= 0 {
		c.BootstrapTTL = 30 * time.Minute
	}
	if c.IdleTTL <= 0 {
		c.IdleTTL = 24 * time.Hour
	}
	if c.AbsoluteTTL <= 0 {
		c.AbsoluteTTL = 7 * 24 * time.Hour
	}
	if c.PasswordParams.Memory == 0 {
		c.PasswordParams = DefaultPasswordParams
	}
	if c.Random == nil {
		c.Random = rand.Reader
	}
	return c
}

// BootstrapState returns setup availability without revealing account details.
func (s *Service) BootstrapState(ctx context.Context) (BootstrapState, error) {
	count, err := s.store.UserCount(ctx)
	if err != nil {
		return BootstrapState{}, err
	}
	deadline := s.bootstrapDeadline
	return BootstrapState{Required: count == 0 && !s.now().After(deadline), ExpiresAt: &deadline}, nil
}

// Bootstrap creates the only P0 admin and immediately issues a session.
func (s *Service) Bootstrap(ctx context.Context, username, password, ip, userAgent string) (Session, error) {
	if err := validateCredentials(username, password); err != nil {
		return Session{}, err
	}
	count, err := s.store.UserCount(ctx)
	if err != nil {
		return Session{}, err
	}
	if count != 0 {
		return Session{}, ErrBootstrapComplete
	}
	if s.now().After(s.bootstrapDeadline) {
		return Session{}, ErrBootstrapClosed
	}
	passwordHash, err := hashPassword(password, s.config.PasswordParams, s.config.Random)
	if err != nil {
		return Session{}, err
	}
	user, err := s.store.CreateUser(ctx, store.User{Username: username, PasswordHash: passwordHash, CreatedAt: s.now()})
	if err != nil {
		if after, countErr := s.store.UserCount(ctx); countErr == nil && after != 0 {
			return Session{}, ErrBootstrapComplete
		}
		return Session{}, err
	}
	return s.issueSession(ctx, user, ip, userAgent)
}

// Login verifies credentials with equal Argon2 work for absent accounts.
func (s *Service) Login(ctx context.Context, username, password, ip, userAgent string) (Session, error) {
	now := s.now()
	if err := s.limiter.allow(now, ip, username); err != nil {
		return Session{}, err
	}
	user, err := s.store.UserByUsername(ctx, username)
	encoded := s.dummyHash
	if err == nil {
		encoded = user.PasswordHash
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Session{}, err
	}
	ok, verifyErr := VerifyPassword(password, encoded)
	if verifyErr != nil || err != nil || !ok {
		s.limiter.failure(now, username)
		return Session{}, ErrUnauthenticated
	}
	s.limiter.success(username)
	return s.issueSession(ctx, user, ip, userAgent)
}

// Authenticate validates a bearer token and rotates it after half the idle TTL.
func (s *Service) Authenticate(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrUnauthenticated
	}
	now := s.now()
	oldHash := tokenHash(token)
	durable, err := s.store.SessionByTokenHash(ctx, oldHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrUnauthenticated
		}
		return Session{}, err
	}
	if !now.Before(durable.ExpiresAt) || !now.Before(durable.AbsoluteExpiresAt) {
		_ = s.store.DeleteSession(ctx, oldHash)
		return Session{}, ErrUnauthenticated
	}
	result := sessionProjection(durable)
	if durable.ExpiresAt.Sub(now) <= s.config.IdleTTL/2 {
		newToken, err := randomToken(s.config.Random)
		if err != nil {
			return Session{}, err
		}
		newExpiry := minTime(now.Add(s.config.IdleTTL), durable.AbsoluteExpiresAt)
		if err := s.store.RotateSession(ctx, oldHash, tokenHash(newToken), now, newExpiry); err != nil {
			return Session{}, err
		}
		result.Token, result.ExpiresAt = newToken, newExpiry
	} else if now.Sub(durable.LastActiveAt) >= 5*time.Minute {
		if err := s.store.TouchSession(ctx, oldHash, now); err != nil {
			return Session{}, err
		}
	}
	return result, nil
}

// Logout idempotently revokes a browser token.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.store.DeleteSession(ctx, tokenHash(token))
}

// ChangePassword verifies the old password, changes it and revokes all other sessions.
func (s *Service) ChangePassword(ctx context.Context, token, oldPassword, newPassword string) error {
	if len(newPassword) < minPasswordLength {
		return ErrWeakPassword
	}
	hash := tokenHash(token)
	session, err := s.store.SessionByTokenHash(ctx, hash)
	if err != nil || !s.now().Before(session.ExpiresAt) || !s.now().Before(session.AbsoluteExpiresAt) {
		return ErrUnauthenticated
	}
	user, err := s.store.UserByUsername(ctx, session.Username)
	if err != nil {
		return ErrUnauthenticated
	}
	ok, err := VerifyPassword(oldPassword, user.PasswordHash)
	if err != nil || !ok {
		return ErrUnauthenticated
	}
	newHash, err := hashPassword(newPassword, s.config.PasswordParams, s.config.Random)
	if err != nil {
		return err
	}
	return s.store.UpdatePasswordAndRevokeSessions(ctx, user.ID, newHash, hash, s.now())
}

func (s *Service) issueSession(ctx context.Context, user store.User, ip, userAgent string) (Session, error) {
	token, err := randomToken(s.config.Random)
	if err != nil {
		return Session{}, err
	}
	now := s.now()
	abs := now.Add(s.config.AbsoluteTTL)
	durable := store.Session{
		TokenHash: tokenHash(token), UserID: user.ID, Username: user.Username,
		CreatedAt: now, LastActiveAt: now, ExpiresAt: minTime(now.Add(s.config.IdleTTL), abs),
		AbsoluteExpiresAt: abs, IP: ip, UserAgent: summarizeUserAgent(userAgent),
	}
	if err := s.store.CreateSession(ctx, durable); err != nil {
		return Session{}, err
	}
	result := sessionProjection(durable)
	result.Token = token
	return result, nil
}

func (s *Service) now() time.Time { return s.config.Now().UTC() }

func validateCredentials(username, password string) error {
	if len(password) < minPasswordLength {
		return ErrWeakPassword
	}
	if username == "" || len(username) > 64 || strings.IndexFunc(username, func(r rune) bool { return r <= ' ' || r == 0x7f }) >= 0 {
		return ErrInvalidUsername
	}
	return nil
}

func randomToken(source io.Reader) (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(source, raw); err != nil {
		return "", fmt.Errorf("read session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func tokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func sessionProjection(value store.Session) Session {
	return Session{UserID: value.UserID, Username: value.Username, Roles: []string{"admin"}, ExpiresAt: value.ExpiresAt}
}

func summarizeUserAgent(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 256 {
		return value[:256]
	}
	return value
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
