package core

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
