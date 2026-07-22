package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	"github.com/linkinghack/envoy-standalone-gateway/internal/auth"
	"github.com/linkinghack/envoy-standalone-gateway/internal/compile"
	"github.com/linkinghack/envoy-standalone-gateway/internal/conf"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver"
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/store"
)

const validDraft = `apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: web}
spec: {port: 8080, protocol: HTTP}
---
apiVersion: esgw/v1alpha1
kind: Upstream
metadata: {name: backend}
spec:
  endpoints: [{address: 127.0.0.1, port: 9000}]
---
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: main}
spec:
  listeners: [web]
  hostnames: [example.com]
  rules:
    - match: {path: {prefix: /}}
      backends: [{upstream: backend}]
`

func TestConfigDraftObjectsValidateAndCompileHTTP(t *testing.T) {
	t.Parallel()
	server, _, _, _ := newConfigTestServer(t)
	cookie := bootstrapCookie(t, server)

	response := apiRequest(t, server, http.MethodPut, "/api/v1/config/draft", map[string]any{
		"sourceType": "protocol", "files": []map[string]string{{"path": "config.d/gateway.yaml", "content": validDraft}},
	}, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("replace draft = %d %s", response.Code, response.Body.String())
	}
	var mutation map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &mutation); err != nil {
		t.Fatal(err)
	}
	hash := mutation["draftResourceVersion"].(string)

	response = apiRequest(t, server, http.MethodGet, "/api/v1/config/draft", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), hash) || !strings.Contains(response.Body.String(), "gateway.yaml") {
		t.Fatalf("get draft = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodGet, "/api/v1/config/objects?kind=Listener", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":1`) {
		t.Fatalf("list objects = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodGet, "/api/v1/config/objects/Route/main", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"main"`) {
		t.Fatalf("get object = %d %s", response.Code, response.Body.String())
	}

	policy := map[string]any{
		"apiVersion": "esgw/v1alpha1", "kind": "Policy", "metadata": map[string]string{"name": "cors"},
		"spec": map[string]any{"cors": map[string]any{"allowOrigins": []string{"*"}}},
	}
	response = apiRequest(t, server, http.MethodPut, "/api/v1/config/objects/Policy/cors?expectedResourceVersion="+hash, policy, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("put object = %d %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &mutation); err != nil {
		t.Fatal(err)
	}
	newHash := mutation["draftResourceVersion"].(string)
	response = apiRequest(t, server, http.MethodDelete, "/api/v1/config/objects/Policy/cors?expectedResourceVersion=stale", nil, cookie)
	assertAPIError(t, response, http.StatusConflict, "CONFLICT")
	response = apiRequest(t, server, http.MethodDelete, "/api/v1/config/objects/Policy/cors?expectedResourceVersion="+newHash, nil, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("delete object = %d %s", response.Code, response.Body.String())
	}

	response = apiRequest(t, server, http.MethodPost, "/api/v1/config/validate?mode=xds", map[string]bool{"envoyValidate": false}, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("validate = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodGet, "/api/v1/config/compiled?mode=xds", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"listeners"`) || !strings.Contains(response.Body.String(), `"version"`) {
		t.Fatalf("compiled = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodGet, "/api/v1/config/schemas", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "application/schema+json") {
		t.Fatalf("schemas = %d %s", response.Code, response.Header().Get("Content-Type"))
	}
}

func TestConfigPublishVersionDiffAndRollbackHTTP(t *testing.T) {
	t.Parallel()
	server, durable, publisher, _ := newConfigTestServer(t)
	cookie := bootstrapCookie(t, server)
	response := apiRequest(t, server, http.MethodPut, "/api/v1/config/draft", map[string]any{
		"sourceType": "protocol", "files": []map[string]string{{"path": "config.d/gateway.yaml", "content": validDraft}},
	}, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("replace draft = %d %s", response.Code, response.Body.String())
	}

	response = apiRequest(t, server, http.MethodPost, "/api/v1/config/publish", map[string]string{"message": "first"}, cookie)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"state":"awaiting_confirm"`) {
		t.Fatalf("publish = %d %s", response.Code, response.Body.String())
	}
	confirmActivePublish(t, durable, publisher)
	response = apiRequest(t, server, http.MethodGet, "/api/v1/config/status", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"effective"`) {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodPost, "/api/v1/config/publish", map[string]string{"message": "duplicate"}, cookie)
	assertAPIError(t, response, http.StatusConflict, "NO_CHANGES")

	updated := strings.Replace(validDraft, "port: 8080", "port: 8081", 1)
	response = apiRequest(t, server, http.MethodPut, "/api/v1/config/draft", map[string]any{
		"sourceType": "protocol", "files": []map[string]string{{"path": "config.d/gateway.yaml", "content": updated}},
	}, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("update draft = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodGet, "/api/v1/config/draft/diff?against=current&format=summary", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"op":"changed"`) {
		t.Fatalf("draft diff = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodPost, "/api/v1/config/publish", map[string]string{"message": "second"}, cookie)
	if response.Code != http.StatusAccepted {
		t.Fatalf("second publish = %d %s", response.Code, response.Body.String())
	}
	confirmActivePublish(t, durable, publisher)

	response = apiRequest(t, server, http.MethodGet, "/api/v1/config/versions?limit=10", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"total":2`) {
		t.Fatalf("versions = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodGet, "/api/v1/config/versions/2/diff?against=1", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"changed"`) {
		t.Fatalf("version diff = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodGet, "/api/v1/config/versions/1/config", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "port: 8080") {
		t.Fatalf("version source = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodGet, "/api/v1/config/versions/1/compiled?mode=static&format=static-yaml", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "config_version") {
		t.Fatalf("version compiled = %d %s", response.Code, response.Body.String())
	}

	response = apiRequest(t, server, http.MethodPost, "/api/v1/config/versions/1/rollback", map[string]bool{"publish": false, "force": false}, cookie)
	assertAPIError(t, response, http.StatusConflict, "CONFLICT")
	response = apiRequest(t, server, http.MethodPost, "/api/v1/config/versions/1/rollback", map[string]bool{"publish": false, "force": true}, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("rollback = %d %s", response.Code, response.Body.String())
	}
	response = apiRequest(t, server, http.MethodGet, "/api/v1/config/draft", nil, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "port: 8080") {
		t.Fatalf("rolled back draft = %d %s", response.Code, response.Body.String())
	}
}

func newConfigTestServer(t *testing.T) (*Server, *store.Store, *conf.Publisher, *fakeAPIDeliverer) {
	t.Helper()
	dataDir := t.TempDir()
	durable, err := store.Open(filepath.Join(dataDir, "esgw.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = durable.Close() })
	deliverer := &fakeAPIDeliverer{}
	publisher := &conf.Publisher{DataDir: dataDir, Store: durable, Deliver: deliverer, Mode: compile.ModeXDS}
	configAPI := &ConfigAPI{DataDir: dataDir, Store: durable, Publisher: publisher, Mode: compile.ModeXDS}
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	authService, err := auth.New(durable, auth.Config{
		StartedAt: now, Now: func() time.Time { return now },
		PasswordParams: auth.PasswordParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 16},
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(Config{Auth: authService, Handlers: configAPI.Handlers(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return server, durable, publisher, deliverer
}

func apiRequest(t *testing.T, handler http.Handler, method, target string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, handler, method, target, body, cookie, method != http.MethodGet)
}

func confirmActivePublish(t *testing.T, durable *store.Store, publisher *conf.Publisher) {
	t.Helper()
	runs, err := durable.ActivePublishRuns(context.Background())
	if err != nil || len(runs) != 1 {
		t.Fatalf("active runs = %+v, %v", runs, err)
	}
	version, err := durable.GetVersion(context.Background(), runs[0].VersionSeq)
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Confirm(context.Background(), runs[0].ID, version.IRVersion); err != nil {
		t.Fatal(err)
	}
}

type fakeAPIDeliverer struct {
	mu     sync.Mutex
	status deliver.Status
}

func (f *fakeAPIDeliverer) Apply(_ context.Context, output *ir.IR) error {
	f.mu.Lock()
	f.status = deliver.Status{Version: output.Version, Phase: deliver.PhaseAwaitingConfirm, UpdatedAt: time.Now().UTC()}
	f.mu.Unlock()
	return nil
}

func (*fakeAPIDeliverer) UpdateEndpoints(context.Context, map[string]*endpointv3.ClusterLoadAssignment) error {
	return deliver.ErrEndpointsUnsupported
}

func (f *fakeAPIDeliverer) Status() deliver.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (*fakeAPIDeliverer) Events() (<-chan deliver.Event, func()) {
	ch := make(chan deliver.Event)
	return ch, func() { close(ch) }
}
