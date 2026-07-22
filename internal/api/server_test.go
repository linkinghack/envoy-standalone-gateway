package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/auth"
	"github.com/linkinghack/envoy-standalone-gateway/internal/store"
)

func TestAuthCSRFAndSessionHTTP(t *testing.T) {
	t.Parallel()
	server := newTestServer(t, nil, nil, nil)

	response := request(t, server, http.MethodGet, "/api/v1/auth/bootstrap", nil, nil, false)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"required":true`) {
		t.Fatalf("bootstrap state = %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("X-Frame-Options") != "DENY" || response.Header().Get("X-Request-ID") == "" {
		t.Fatalf("security headers = %v", response.Header())
	}

	response = request(t, server, http.MethodPost, "/api/v1/auth/bootstrap", map[string]string{
		"username": "admin", "password": "long-enough-password",
	}, nil, false)
	assertAPIError(t, response, http.StatusForbidden, "FORBIDDEN")

	response = request(t, server, http.MethodPost, "/api/v1/auth/bootstrap", map[string]string{
		"username": "admin", "password": "long-enough-password",
	}, nil, true)
	if response.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %+v", cookies)
	}
	cookie := cookies[0]

	response = request(t, server, http.MethodGet, "/api/v1/auth/session", nil, cookie, false)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"admin"`) {
		t.Fatalf("session = %d %s", response.Code, response.Body.String())
	}
	response = request(t, server, http.MethodGet, "/api/v1/auth/session", nil, nil, false)
	assertAPIError(t, response, http.StatusUnauthorized, "UNAUTHENTICATED")

	response = request(t, server, http.MethodPost, "/api/v1/auth/logout", nil, cookie, false)
	assertAPIError(t, response, http.StatusForbidden, "FORBIDDEN")
	response = request(t, server, http.MethodPost, "/api/v1/auth/logout", nil, cookie, true)
	if response.Code != http.StatusNoContent {
		t.Fatalf("logout = %d %s", response.Code, response.Body.String())
	}
	cleared := response.Result().Cookies()
	if len(cleared) != 1 || cleared[0].MaxAge >= 0 {
		t.Fatalf("clear cookie = %+v", cleared)
	}

	response = request(t, server, http.MethodPost, "/api/v1/auth/bootstrap", map[string]string{
		"username": "other", "password": "long-enough-password",
	}, nil, true)
	assertAPIError(t, response, http.StatusConflict, "CONFLICT")
}

func TestAPIRoutingSPAFallbackAndReady(t *testing.T) {
	t.Parallel()
	assets := fstest.MapFS{
		"index.html":        {Data: []byte("<html>console</html>")},
		"assets/app.abc.js": {Data: []byte("console.log('ok')")},
	}
	ready := false
	server := newTestServer(t, assets, func() bool { return ready }, nil)

	response := request(t, server, http.MethodGet, "/healthz", nil, nil, false)
	if response.Code != http.StatusOK {
		t.Fatalf("health = %d", response.Code)
	}
	response = request(t, server, http.MethodGet, "/readyz", nil, nil, false)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("not ready = %d", response.Code)
	}
	ready = true
	response = request(t, server, http.MethodGet, "/readyz", nil, nil, false)
	if response.Code != http.StatusOK {
		t.Fatalf("ready = %d", response.Code)
	}

	response = request(t, server, http.MethodGet, "/api/v1/not-real", nil, nil, false)
	assertAPIError(t, response, http.StatusNotFound, "NOT_FOUND")
	if strings.Contains(response.Body.String(), "<html>") {
		t.Fatal("unknown API returned SPA")
	}
	response = request(t, server, http.MethodGet, "/dashboard/config", nil, nil, false)
	if response.Code != http.StatusOK || response.Body.String() != "<html>console</html>" || response.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("SPA fallback = %d %q %q", response.Code, response.Body.String(), response.Header().Get("Cache-Control"))
	}
	response = request(t, server, http.MethodGet, "/assets/app.abc.js", nil, nil, false)
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset = %d %q", response.Code, response.Header().Get("Cache-Control"))
	}
	response = request(t, server, http.MethodGet, "/assets/missing.js", nil, nil, false)
	assertAPIError(t, response, http.StatusNotFound, "NOT_FOUND")
}

func TestRecoveryAndUnimplementedInventory(t *testing.T) {
	t.Parallel()
	handlers := map[string]OperationHandler{
		"getConfigDraft": func(http.ResponseWriter, *http.Request) { panic("secret panic") },
	}
	server := newTestServer(t, nil, nil, handlers)
	cookie := bootstrapCookie(t, server)
	response := request(t, server, http.MethodGet, "/api/v1/config/draft", nil, cookie, false)
	assertAPIError(t, response, http.StatusInternalServerError, "INTERNAL")
	if strings.Contains(response.Body.String(), "secret panic") {
		t.Fatal("panic detail leaked")
	}
	if len(server.UnimplementedOperations()) == 0 {
		t.Fatal("expected later S5 operations to remain explicitly unimplemented")
	}
}

func newTestServer(t *testing.T, assets fs.FS, ready func() bool, handlers map[string]OperationHandler) *Server {
	t.Helper()
	durable, err := store.Open(filepath.Join(t.TempDir(), "esgw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = durable.Close() })
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	authService, err := auth.New(durable, auth.Config{
		StartedAt: now, Now: func() time.Time { return now },
		PasswordParams: auth.PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 16},
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(Config{
		Auth: authService, Assets: assets, Ready: ready, Handlers: handlers,
		Logger: slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil)), Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func bootstrapCookie(t *testing.T, server http.Handler) *http.Cookie {
	t.Helper()
	response := request(t, server, http.MethodPost, "/api/v1/auth/bootstrap", map[string]string{
		"username": "admin", "password": "long-enough-password",
	}, nil, true)
	if response.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d %s", response.Code, response.Body.String())
	}
	return response.Result().Cookies()[0]
}

func request(t *testing.T, handler http.Handler, method, target string, body any, cookie *http.Cookie, csrf bool) *httptest.ResponseRecorder {
	t.Helper()
	var content []byte
	if body != nil {
		var err error
		content, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequestWithContext(context.Background(), method, target, bytes.NewReader(content))
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf {
		r.Header.Set("X-ESGW-Request", "1")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d: %s", response.Code, status, response.Body.String())
	}
	var payload ErrorResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v (%s)", err, response.Body.String())
	}
	if payload.Error.Code != code {
		t.Fatalf("error code = %q, want %q", payload.Error.Code, code)
	}
}
