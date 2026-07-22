package core

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linkinghack/envoy-standalone-gateway/internal/config"
)

const appDraft = `apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: web}
spec: {port: 8080, protocol: HTTP}
`

func TestAppHTTPConfigurationPublishFlow(t *testing.T) {
	t.Parallel()
	cfg := testConfig("127.0.0.1:18000")
	cfg.DataDir = t.TempDir()
	cfg.Deliver.XDS.AdminAddress = "127.0.0.1:19901"
	app, err := NewApp(cfg, nil, discardLog())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	response := appRequest(t, app.Handler(), http.MethodGet, "/api/v1/config/draft", nil, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated config = %d %s", response.Code, response.Body.String())
	}
	response = appRequest(t, app.Handler(), http.MethodGet, "/configuration", nil, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `id="root"`) {
		t.Fatalf("embedded SPA = %d %s", response.Code, response.Body.String())
	}
	response = appRequest(t, app.Handler(), http.MethodPost, "/api/v1/auth/bootstrap", map[string]string{
		"username": "admin", "password": "long-enough-password",
	}, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("bootstrap = %d %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("bootstrap cookies = %+v", cookies)
	}
	cookie := cookies[0]

	response = appRequest(t, app.Handler(), http.MethodPut, "/api/v1/config/draft", map[string]any{
		"sourceType": "protocol", "files": []map[string]string{{"path": "config.d/gateway.yaml", "content": appDraft}},
	}, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("replace draft = %d %s", response.Code, response.Body.String())
	}
	response = appRequest(t, app.Handler(), http.MethodPost, "/api/v1/config/validate?mode=xds", map[string]bool{
		"envoyValidate": false,
	}, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("validate = %d %s", response.Code, response.Body.String())
	}
	response = appRequest(t, app.Handler(), http.MethodPost, "/api/v1/config/publish", map[string]string{
		"message": "app integration publish",
	}, cookie)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"state":"awaiting_confirm"`) {
		t.Fatalf("publish = %d %s", response.Code, response.Body.String())
	}
	response = appRequest(t, app.Handler(), http.MethodGet, "/api/v1/config/status", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"published"`) || !strings.Contains(response.Body.String(), `"awaiting_confirm"`) {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
}

func TestAppCompositionMatrix(t *testing.T) {
	envoyPath := filepath.Join(t.TempDir(), "envoy")
	if err := os.WriteFile(envoyPath, []byte("#!/bin/sh\necho 'envoy version: build/1.39.0/Clean'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ESGW_ENVOY_PATH", envoyPath)
	for _, tc := range []struct {
		name    string
		mode    string
		managed bool
	}{
		{name: "xds file-only", mode: config.ModeXDS},
		{name: "xds managed", mode: config.ModeXDS, managed: true},
		{name: "static file-only", mode: config.ModeStatic},
		{name: "static managed", mode: config.ModeStatic, managed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig("127.0.0.1:18000")
			cfg.DataDir = t.TempDir()
			cfg.Deliver.Mode = tc.mode
			cfg.Deliver.XDS.NodeCluster = config.DefaultNodeCluster
			cfg.Deliver.XDS.AdminAddress = "unix:///tmp/esgw-matrix-admin.sock"
			cfg.Deliver.Static.OutputPath = filepath.Join(cfg.DataDir, "envoy", "envoy.yaml")
			cfg.Proc.Enabled = tc.managed
			configDir := filepath.Join(cfg.DataDir, "config.d")
			if err := os.MkdirAll(configDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(configDir, "gateway.yaml"), []byte(appDraft), 0o600); err != nil {
				t.Fatal(err)
			}
			app, err := NewApp(cfg, nil, discardLog())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = app.Close() })
			if (app.xds != nil) != (tc.mode == config.ModeXDS) || (app.static != nil) != (tc.mode == config.ModeStatic) {
				t.Fatalf("mode composition xds=%v static=%v", app.xds != nil, app.static != nil)
			}
			if (app.supervisor != nil) != tc.managed {
				t.Fatalf("managed composition supervisor=%v", app.supervisor != nil)
			}
			if tc.managed && tc.mode == config.ModeXDS {
				if _, err := os.Stat(filepath.Join(cfg.DataDir, "envoy", "bootstrap.yaml")); err != nil {
					t.Fatalf("managed xDS bootstrap: %v", err)
				}
			}
			if tc.mode == config.ModeStatic {
				if _, err := os.Stat(cfg.Deliver.Static.OutputPath); err != nil {
					t.Fatalf("static artifact: %v", err)
				}
			}
		})
	}
}

func appRequest(t *testing.T, handler http.Handler, method, target string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var content []byte
	if body != nil {
		var err error
		content, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequestWithContext(context.Background(), method, target, bytes.NewReader(content))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("X-ESGW-Request", "1")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
