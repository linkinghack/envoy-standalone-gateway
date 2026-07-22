// Package api serves the authenticated management HTTP API and embedded SPA.
package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/api/contract"
	"github.com/linkinghack/envoy-standalone-gateway/internal/auth"
)

const sessionCookieName = "esgw_session"

type contextKey int

const sessionContextKey contextKey = iota

// AuthService is the HTTP-facing auth service contract.
type AuthService interface {
	BootstrapState(context.Context) (auth.BootstrapState, error)
	Bootstrap(context.Context, string, string, string, string) (auth.Session, error)
	Login(context.Context, string, string, string, string) (auth.Session, error)
	Authenticate(context.Context, string) (auth.Session, error)
	Logout(context.Context, string) error
	ChangePassword(context.Context, string, string, string) error
}

// OperationHandler handles one operation after security middleware succeeds.
type OperationHandler func(http.ResponseWriter, *http.Request)

// Config contains HTTP adapters; domain services remain independent of HTTP.
type Config struct {
	Auth       AuthService
	Handlers   map[string]OperationHandler
	Assets     fs.FS
	Ready      func() bool
	Metrics    http.Handler
	Logger     *slog.Logger
	Now        func() time.Time
	CookiePath string
}

// Server is the management HTTP handler.
type Server struct {
	config        Config
	handler       http.Handler
	unimplemented []string
}

// MergeHandlers combines domain adapters and fails fast on duplicate operation IDs.
func MergeHandlers(groups ...map[string]OperationHandler) (map[string]OperationHandler, error) {
	merged := make(map[string]OperationHandler)
	for _, group := range groups {
		for id, handler := range group {
			if _, exists := merged[id]; exists {
				return nil, fmt.Errorf("duplicate API operation handler %q", id)
			}
			merged[id] = handler
		}
	}
	return merged, nil
}

// NewServer registers every OpenAPI operation and returns JSON 501 for domain
// handlers that later S5 tasks have not wired yet.
func NewServer(config Config) (*Server, error) {
	if config.Auth == nil {
		return nil, errors.New("API auth service is required")
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.CookiePath == "" {
		config.CookiePath = "/"
	}
	server := &Server{config: config}
	mux := http.NewServeMux()
	builtin := server.builtinHandlers()
	for _, operation := range contract.Operations {
		handler := builtin[operation.ID]
		if handler == nil {
			handler = config.Handlers[operation.ID]
		}
		if handler == nil {
			server.unimplemented = append(server.unimplemented, operation.ID)
			handler = server.notImplemented(operation.ID)
		}
		mux.Handle(operation.Method+" "+operation.Path, server.secureOperation(operation, handler))
	}
	mux.Handle("/", server.fallback())
	server.handler = server.baseMiddleware(mux)
	return server, nil
}

// ServeHTTP serves management API and SPA requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

// UnimplementedOperations returns a defensive copy for S5 completion checks.
func (s *Server) UnimplementedOperations() []string {
	return append([]string(nil), s.unimplemented...)
}

func (s *Server) secureOperation(operation contract.Operation, next OperationHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-ESGW-Request") != "1" {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "missing or invalid X-ESGW-Request header")
			return
		}
		if operation.Anonymous {
			next(w, r)
			return
		}
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
			return
		}
		session, err := s.config.Auth.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, auth.ErrUnauthenticated) {
				s.clearSessionCookie(w, r)
				writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
				return
			}
			s.internalError(w, r, err)
			return
		}
		if session.Token != "" {
			s.setSessionCookie(w, r, session.Token, session.ExpiresAt)
		}
		next(w, r.WithContext(context.WithValue(r.Context(), sessionContextKey, session)))
	})
}

func (s *Server) baseMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'self'")
		defer func() {
			if recovered := recover(); recovered != nil {
				s.config.Logger.Error("management request panic", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "panic", recovered)
				writeError(w, http.StatusInternalServerError, "INTERNAL", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) fallback() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/api") {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "API endpoint not found")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
			return
		}
		if s.config.Assets == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
			return
		}
		name := "index.html"
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			name = strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		}
		content, err := fs.ReadFile(s.config.Assets, name)
		if err != nil {
			if name != "index.html" {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "asset not found")
				return
			}
			writeError(w, http.StatusNotFound, "NOT_FOUND", "console is not installed")
			return
		}
		if name == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(content))
	})
}

func (s *Server) notImplemented(id string) OperationHandler {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotImplemented, "INTERNAL", fmt.Sprintf("operation %s is not wired", id))
	}
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.config.Logger.Error("management request failed", "request_id", w.Header().Get("X-Request-ID"), "method", r.Method, "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, "INTERNAL", "internal server error")
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: s.config.CookiePath,
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
		Expires: expiresAt, MaxAge: max(int(expiresAt.Sub(s.config.Now()).Seconds()), 1),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Path: s.config.CookiePath, HttpOnly: true,
		Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0),
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func newRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value[:])
}
