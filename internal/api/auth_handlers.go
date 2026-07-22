package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/auth"
)

type credentialsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type passwordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

type sessionResponse struct {
	User struct {
		Name  string   `json:"name"`
		Roles []string `json:"roles"`
	} `json:"user"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (s *Server) builtinHandlers() map[string]OperationHandler {
	return map[string]OperationHandler{
		"getAuthBootstrap":     s.getAuthBootstrap,
		"createAuthBootstrap":  s.createAuthBootstrap,
		"login":                s.login,
		"logout":               s.logout,
		"getAuthSession":       s.getAuthSession,
		"changePassword":       s.changePassword,
		"listAuthMethods":      s.listAuthMethods,
		"getHealth":            s.getHealth,
		"getReady":             s.getReady,
		"getManagementMetrics": s.getManagementMetrics,
	}
}

func (s *Server) getAuthBootstrap(w http.ResponseWriter, r *http.Request) {
	state, err := s.config.Auth.BootstrapState(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"required": state.Required, "expiresAt": state.ExpiresAt})
}

func (s *Server) createAuthBootstrap(w http.ResponseWriter, r *http.Request) {
	var request credentialsRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	session, err := s.config.Auth.Bootstrap(r.Context(), request.Username, request.Password, clientIP(r), r.UserAgent())
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	s.setSessionCookie(w, r, session.Token, session.ExpiresAt)
	writeJSON(w, http.StatusCreated, makeSessionResponse(session))
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var request credentialsRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	session, err := s.config.Auth.Login(r.Context(), request.Username, request.Password, clientIP(r), r.UserAgent())
	if err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	s.setSessionCookie(w, r, session.Token, session.ExpiresAt)
	writeJSON(w, http.StatusOK, makeSessionResponse(session))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(sessionCookieName)
	if cookie != nil {
		if err := s.config.Auth.Logout(r.Context(), cookie.Value); err != nil {
			s.internalError(w, r, err)
			return
		}
	}
	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getAuthSession(w http.ResponseWriter, r *http.Request) {
	session, ok := r.Context().Value(sessionContextKey).(auth.Session)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	writeJSON(w, http.StatusOK, makeSessionResponse(session))
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var request passwordRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	cookie, _ := r.Cookie(sessionCookieName)
	if cookie == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	if err := s.config.Auth.ChangePassword(r.Context(), cookie.Value, request.OldPassword, request.NewPassword); err != nil {
		s.writeAuthError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listAuthMethods(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": []map[string]string{{"type": "local"}}})
}

func (s *Server) getHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) getReady(w http.ResponseWriter, _ *http.Request) {
	ready := s.config.Ready == nil || s.config.Ready()
	status, value := http.StatusOK, "ready"
	if !ready {
		status, value = http.StatusServiceUnavailable, "not_ready"
	}
	writeJSON(w, status, map[string]string{"status": value})
}

func (s *Server) getManagementMetrics(w http.ResponseWriter, r *http.Request) {
	if s.config.Metrics == nil {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = io.WriteString(w, "# esgw management metrics are not configured\n")
		return
	}
	s.config.Metrics.ServeHTTP(w, r)
}

func (s *Server) writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	var limited auth.RateLimitError
	switch {
	case errors.As(err, &limited):
		retry := max(int(limited.RetryAfter.Round(time.Second).Seconds()), 1)
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many authentication attempts")
	case errors.Is(err, auth.ErrUnauthenticated):
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "invalid username or password")
	case errors.Is(err, auth.ErrBootstrapComplete):
		writeError(w, http.StatusConflict, "CONFLICT", "initial bootstrap is already complete")
	case errors.Is(err, auth.ErrBootstrapClosed):
		writeError(w, http.StatusForbidden, "FORBIDDEN", "initial bootstrap window is closed")
	case errors.Is(err, auth.ErrWeakPassword), errors.Is(err, auth.ErrInvalidUsername):
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
	default:
		s.internalError(w, r, err)
	}
}

func makeSessionResponse(session auth.Session) sessionResponse {
	response := sessionResponse{ExpiresAt: session.ExpiresAt}
	response.User.Name, response.User.Roles = session.Username, session.Roles
	return response
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON request body")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "request body must contain one JSON value")
		return false
	}
	return true
}
