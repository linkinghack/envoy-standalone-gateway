package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	yamlv3 "gopkg.in/yaml.v3"

	"github.com/linkinghack/envoy-standalone-gateway/internal/auth"
	"github.com/linkinghack/envoy-standalone-gateway/internal/compile"
	"github.com/linkinghack/envoy-standalone-gateway/internal/compile/envoycheck"
	"github.com/linkinghack/envoy-standalone-gateway/internal/conf"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver/static"
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
	"github.com/linkinghack/envoy-standalone-gateway/internal/store"
)

// ConfigAPI adapts M-CONF/M-STORE to management HTTP operations.
type ConfigAPI struct {
	DataDir   string
	Store     *store.Store
	Publisher *conf.Publisher
	Mode      compile.Mode
	mu        sync.Mutex
}

type draftFileResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type draftResponse struct {
	SourceType      string              `json:"sourceType"`
	ResourceVersion string              `json:"resourceVersion"`
	UpdatedAt       *time.Time          `json:"updatedAt"`
	Files           []draftFileResponse `json:"files"`
}

type replaceDraftRequest struct {
	SourceType              string              `json:"sourceType"`
	ExpectedResourceVersion string              `json:"expectedResourceVersion"`
	Files                   []draftFileResponse `json:"files"`
}

type validationRequest struct {
	EnvoyValidate bool `json:"envoyValidate"`
}

type publishRequest struct {
	Message                 string `json:"message"`
	ExpectedResourceVersion string `json:"expectedResourceVersion"`
}

type rollbackRequest struct {
	Publish bool `json:"publish"`
	Force   bool `json:"force"`
}

type diagnostic struct {
	Stage    string         `json:"stage"`
	Source   map[string]any `json:"source,omitempty"`
	Message  string         `json:"message"`
	Severity string         `json:"severity"`
}

type objectRecord struct {
	Kind   protocol.Kind
	Name   string
	Origin protocol.Origin
	Value  any
}

// Handlers returns all S5 configuration operation adapters.
func (a *ConfigAPI) Handlers() map[string]OperationHandler {
	return map[string]OperationHandler{
		"getConfigDraft":           a.getDraft,
		"replaceConfigDraft":       a.replaceDraft,
		"listConfigObjects":        a.listObjects,
		"getConfigObject":          a.getObject,
		"putConfigObject":          a.putObject,
		"deleteConfigObject":       a.deleteObject,
		"getConfigSchemas":         a.getSchemas,
		"validateConfig":           a.validate,
		"getCompiledConfig":        a.getCompiled,
		"getDraftDiff":             a.getDraftDiff,
		"publishConfig":            a.publish,
		"getConfigStatus":          a.getStatus,
		"listConfigVersions":       a.listVersions,
		"getConfigVersion":         a.getVersion,
		"getConfigVersionSource":   a.getVersionSource,
		"getConfigVersionCompiled": a.getVersionCompiled,
		"getConfigVersionDiff":     a.getVersionDiff,
		"rollbackConfigVersion":    a.rollback,
	}
}

func (a *ConfigAPI) getDraft(w http.ResponseWriter, r *http.Request) {
	response, err := readDraft(a.DataDir)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *ConfigAPI) replaceDraft(w http.ResponseWriter, r *http.Request) {
	var request replaceDraftRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	mode, ok := sourceMode(request.SourceType)
	if !ok {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "sourceType must be protocol or native")
		return
	}
	files := make([]conf.SourceFile, 0, len(request.Files))
	for _, file := range request.Files {
		files = append(files, conf.SourceFile{Path: file.Path, Content: []byte(file.Content)})
	}
	a.mu.Lock()
	hash, err := conf.ReplaceDraft(a.DataDir, mode, files, request.ExpectedResourceVersion)
	a.mu.Unlock()
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"draftResourceVersion": hash})
}

func (a *ConfigAPI) listObjects(w http.ResponseWriter, r *http.Request) {
	draft, loadErrs, err := conf.LoadDraft(a.DataDir)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	if len(loadErrs) != 0 {
		writeValidationError(w, loadDiagnostics(loadErrs))
		return
	}
	records := configObjects(draft.Config)
	if rawKind := r.URL.Query().Get("kind"); rawKind != "" {
		kind := protocol.Kind(rawKind)
		if !kind.Valid() {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid object kind")
			return
		}
		filtered := records[:0]
		for _, record := range records {
			if record.Kind == kind {
				filtered = append(filtered, record)
			}
		}
		records = filtered
	}
	limit, offset, ok := pagination(w, r)
	if !ok {
		return
	}
	total := len(records)
	start := min(offset, total)
	end := min(start+limit, total)
	items := make([]any, 0, end-start)
	for _, record := range records[start:end] {
		items = append(items, record.Value)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "resourceVersion": draft.Hash})
}

func (a *ConfigAPI) getObject(w http.ResponseWriter, r *http.Request) {
	record, _, err := a.findObject(r.PathValue("kind"), r.PathValue("name"))
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/yaml") {
		content, err := objectYAML(record.Value)
		if err != nil {
			writeConfigError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(content)
		return
	}
	writeJSON(w, http.StatusOK, record.Value)
}

func (a *ConfigAPI) putObject(w http.ResponseWriter, r *http.Request) {
	kind, name := protocol.Kind(r.PathValue("kind")), r.PathValue("name")
	if !kind.Valid() || protocol.ValidateName(name) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid object kind or name")
		return
	}
	document, envelope, ok := readObjectDocument(w, r)
	if !ok {
		return
	}
	if envelope.Kind != kind || envelope.Metadata.Name != name {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "path kind/name must match request body")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.checkExpectedHash(w, r) {
		return
	}
	record, draft, err := a.findObject(string(kind), name)
	switch {
	case err == nil:
		err = conf.WriteDraftDocument(a.DataDir, record.Origin, document)
	case errors.Is(err, sql.ErrNoRows):
		files, readErr := currentSourceFiles(a.DataDir, draft)
		if readErr != nil {
			err = readErr
			break
		}
		filename := fmt.Sprintf("config.d/90-api-%s-%s.yaml", strings.ToLower(string(kind)), name)
		files = append(files, conf.SourceFile{Path: filename, Content: append(document, '\n')})
		_, err = conf.ReplaceDraft(a.DataDir, conf.ModeAbstract, files, draft.Hash)
	}
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	hash, err := conf.DraftHash(a.DataDir)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": envelope, "draftResourceVersion": hash})
}

func (a *ConfigAPI) deleteObject(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.checkExpectedHash(w, r) {
		return
	}
	record, _, err := a.findObject(r.PathValue("kind"), r.PathValue("name"))
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	if err := conf.DeleteDraftDocument(a.DataDir, record.Origin); err != nil {
		writeConfigError(w, r, err)
		return
	}
	hash, err := conf.DraftHash(a.DataDir)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"draftResourceVersion": hash})
}

func (a *ConfigAPI) getSchemas(w http.ResponseWriter, r *http.Request) {
	content, err := protocol.Schemas()
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/schema+json; charset=utf-8")
	_, _ = w.Write(append(content, '\n'))
}

func (a *ConfigAPI) validate(w http.ResponseWriter, r *http.Request) {
	var request validationRequest
	if !decodeOptionalJSON(w, r, &request) {
		return
	}
	mode, ok := compileMode(r.URL.Query().Get("mode"), a.Mode)
	if !ok {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "mode must be xds or static")
		return
	}
	output, diagnostics := compileDraft(r.Context(), a.DataDir, mode)
	if request.EnvoyValidate {
		diagnostics = append(diagnostics, validateWithEnvoy(r.Context(), a.DataDir)...)
	}
	ok = output != nil && !hasErrorDiagnostics(diagnostics)
	var version any
	if output != nil {
		version = output.Version
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "mode": mode, "irVersion": version, "results": diagnostics})
}

func (a *ConfigAPI) getCompiled(w http.ResponseWriter, r *http.Request) {
	mode, ok := compileMode(r.URL.Query().Get("mode"), a.Mode)
	if !ok {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "mode must be xds or static")
		return
	}
	output, diagnostics := compileDraft(r.Context(), a.DataDir, mode)
	if output == nil || hasErrorDiagnostics(diagnostics) {
		writeValidationError(w, diagnostics)
		return
	}
	content, err := deliver.SnapshotJSON(output)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	document, err := compiledDocument(output, content)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, document)
}

func (a *ConfigAPI) getDraftDiff(w http.ResponseWriter, r *http.Request) {
	from := int64(0)
	against := r.URL.Query().Get("against")
	if against == "" || against == "current" {
		version, err := a.Store.LatestVersion(r.Context(), "effective")
		if err == nil {
			from = version.Seq
		} else if !errors.Is(err, sql.ErrNoRows) {
			writeConfigError(w, r, err)
			return
		}
	} else {
		var err error
		from, err = strconv.ParseInt(against, 10, 64)
		if err != nil || from <= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "against must be current or a positive version id")
			return
		}
	}
	diff, err := conf.DiffDraftAgainstSnapshot(a.DataDir, from)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" || format == "unified" {
		var patch strings.Builder
		for _, file := range diff.Files {
			if file.Patch == "" {
				continue
			}
			patch.WriteString("# " + file.Path + " (" + file.Status + ")\n")
			patch.WriteString(file.Patch)
		}
		writeJSON(w, http.StatusOK, map[string]any{"base": from, "format": "unified", "diff": patch.String()})
		return
	}
	if format == "summary" {
		changes := make([]map[string]string, 0, len(diff.Files))
		for _, file := range diff.Files {
			if file.Status != "unchanged" {
				changes = append(changes, map[string]string{"op": file.Status, "path": file.Path})
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"base": from, "format": "summary", "changes": changes})
		return
	}
	writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "format must be unified or summary")
}

func (a *ConfigAPI) publish(w http.ResponseWriter, r *http.Request) {
	var request publishRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if len(request.Message) > 500 {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "publish message exceeds 500 characters")
		return
	}
	mode := a.Mode
	if mode == "" {
		mode = compile.ModeXDS
	}
	if output, diagnostics := compileDraft(r.Context(), a.DataDir, mode); output != nil && !hasErrorDiagnostics(diagnostics) {
		if latest, latestErr := a.Store.LatestVersion(r.Context(), "effective"); latestErr == nil && latest.IRVersion == output.Version {
			writeError(w, http.StatusConflict, "NO_CHANGES", "draft compiles to the current effective version")
			return
		} else if latestErr != nil && !errors.Is(latestErr, sql.ErrNoRows) {
			writeConfigError(w, r, latestErr)
			return
		}
	}
	result, err := a.Publisher.PublishWithBase(r.Context(), requestUser(r), request.Message, request.ExpectedResourceVersion)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	version, err := a.Store.GetVersion(r.Context(), result.Seq)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, publishResponse(version, a.Publisher.Deliver.Status(), string(a.Mode)))
}

func (a *ConfigAPI) getStatus(w http.ResponseWriter, r *http.Request) {
	response := map[string]any{"delivery": deliveryResponse(a.Publisher.Deliver.Status(), string(a.Mode))}
	if latest, err := a.Store.LatestVersion(r.Context(), ""); err == nil {
		response["published"] = versionResponse(latest)
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeConfigError(w, r, err)
		return
	}
	if effective, err := a.Store.LatestVersion(r.Context(), "effective"); err == nil {
		response["effective"] = versionResponse(effective)
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeConfigError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *ConfigAPI) listVersions(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := pagination(w, r)
	if !ok {
		return
	}
	versions, total, err := a.Store.ListVersions(r.Context(), limit, offset)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(versions))
	for _, version := range versions {
		items = append(items, versionResponse(version))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (a *ConfigAPI) getVersion(w http.ResponseWriter, r *http.Request) {
	seq, ok := versionPath(w, r)
	if !ok {
		return
	}
	version, err := a.Store.GetVersion(r.Context(), seq)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, versionResponse(version))
}

func (a *ConfigAPI) getVersionSource(w http.ResponseWriter, r *http.Request) {
	seq, ok := versionPath(w, r)
	if !ok {
		return
	}
	if _, err := a.Store.GetVersion(r.Context(), seq); err != nil {
		writeConfigError(w, r, err)
		return
	}
	response, err := readDraft(snapshotDataDir(a.DataDir, seq))
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *ConfigAPI) getVersionCompiled(w http.ResponseWriter, r *http.Request) {
	seq, ok := versionPath(w, r)
	if !ok {
		return
	}
	if _, err := a.Store.GetVersion(r.Context(), seq); err != nil {
		writeConfigError(w, r, err)
		return
	}
	mode, valid := compileMode(r.URL.Query().Get("mode"), a.Mode)
	if !valid {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "mode must be xds or static")
		return
	}
	output, diagnostics := compileDraft(r.Context(), snapshotDataDir(a.DataDir, seq), mode)
	if output == nil || hasErrorDiagnostics(diagnostics) {
		writeValidationError(w, diagnostics)
		return
	}
	var content []byte
	var err error
	if r.URL.Query().Get("format") == "static-yaml" {
		content, err = static.Render(output)
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	} else {
		content, err = deliver.SnapshotJSON(output)
		if err == nil {
			var document map[string]any
			document, err = compiledDocument(output, content)
			if err == nil {
				writeJSON(w, http.StatusOK, document)
				return
			}
		}
	}
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	_, _ = w.Write(content)
}

func compiledDocument(output *ir.IR, snapshot []byte) (map[string]any, error) {
	var document map[string]any
	if err := json.Unmarshal(snapshot, &document); err != nil {
		return nil, err
	}
	keys := make([]ir.ResourceKey, 0, len(output.SourceMap))
	for key := range output.SourceMap {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	sources := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		source := output.SourceMap[key]
		sources = append(sources, map[string]any{
			"resource": key.String(), "file": source.File, "kind": source.Kind,
			"name": source.Name, "path": source.Path,
		})
	}
	document["sourceMap"] = sources
	return document, nil
}

func (a *ConfigAPI) getVersionDiff(w http.ResponseWriter, r *http.Request) {
	to, ok := versionPath(w, r)
	if !ok {
		return
	}
	from, err := strconv.ParseInt(r.URL.Query().Get("against"), 10, 64)
	if err != nil || from <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "against must be a positive version id")
		return
	}
	diff, err := conf.DiffSnapshots(a.DataDir, from, to)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, diff)
}

func (a *ConfigAPI) rollback(w http.ResponseWriter, r *http.Request) {
	seq, ok := versionPath(w, r)
	if !ok {
		return
	}
	var request rollbackRequest
	if !decodeOptionalJSON(w, r, &request) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !request.Publish {
		if err := conf.RollbackSource(a.DataDir, seq, request.Force); err != nil {
			writeConfigError(w, r, err)
			return
		}
		hash, err := conf.DraftHash(a.DataDir)
		if err != nil {
			writeConfigError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"draftResourceVersion": hash})
		return
	}
	result, err := a.Publisher.RollbackPublish(r.Context(), seq, requestUser(r), fmt.Sprintf("rollback to version %d", seq), request.Force)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	version, err := a.Store.GetVersion(r.Context(), result.Seq)
	if err != nil {
		writeConfigError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, publishResponse(version, a.Publisher.Deliver.Status(), string(a.Mode)))
}

func (a *ConfigAPI) findObject(rawKind, name string) (objectRecord, *conf.Draft, error) {
	kind := protocol.Kind(rawKind)
	if !kind.Valid() || protocol.ValidateName(name) != nil {
		return objectRecord{}, nil, errors.New("invalid object kind or name")
	}
	draft, loadErrs, err := conf.LoadDraft(a.DataDir)
	if err != nil {
		return objectRecord{}, nil, err
	}
	if len(loadErrs) != 0 {
		return objectRecord{}, draft, fmt.Errorf("draft load failed: %s", loadErrs[0])
	}
	for _, record := range configObjects(draft.Config) {
		if record.Kind == kind && record.Name == name {
			return record, draft, nil
		}
	}
	return objectRecord{}, draft, sql.ErrNoRows
}

func (a *ConfigAPI) checkExpectedHash(w http.ResponseWriter, r *http.Request) bool {
	expected := r.URL.Query().Get("expectedResourceVersion")
	if expected == "" {
		return true
	}
	current, err := conf.DraftHash(a.DataDir)
	if err != nil {
		writeConfigError(w, r, err)
		return false
	}
	if current != expected {
		writeError(w, http.StatusConflict, "CONFLICT", "draft changed since it was read")
		return false
	}
	return true
}

func readDraft(dataDir string) (draftResponse, error) {
	draft, _, err := conf.LoadDraft(dataDir)
	if err != nil {
		return draftResponse{}, err
	}
	response := draftResponse{SourceType: sourceType(draft.Mode), ResourceVersion: draft.Hash, Files: []draftFileResponse{}}
	var latest time.Time
	for _, file := range draft.Files {
		content, err := os.ReadFile(file)
		if err != nil {
			return draftResponse{}, err
		}
		rel, err := filepath.Rel(dataDir, file)
		if err != nil {
			return draftResponse{}, err
		}
		response.Files = append(response.Files, draftFileResponse{Path: filepath.ToSlash(rel), Content: string(content)})
		if info, err := os.Stat(file); err == nil && info.ModTime().After(latest) {
			latest = info.ModTime().UTC()
		}
	}
	if !latest.IsZero() {
		response.UpdatedAt = &latest
	}
	return response, nil
}

func compileDraft(_ context.Context, dataDir string, mode compile.Mode) (*ir.IR, []diagnostic) {
	draft, loadErrs, err := conf.LoadDraft(dataDir)
	if err != nil {
		return nil, []diagnostic{{Stage: "schema", Message: err.Error(), Severity: "error"}}
	}
	if len(loadErrs) != 0 {
		return nil, loadDiagnostics(loadErrs)
	}
	if draft.Mode == conf.ModeNative {
		output, err := conf.LoadNative(filepath.Join(dataDir, "native.yaml"))
		if err != nil {
			return nil, []diagnostic{{Stage: "validate", Message: err.Error(), Severity: "error"}}
		}
		return output, nil
	}
	output, compileErrs := compile.Compile(draft.Config, compile.Options{
		Mode: mode, ManagedCertificateDir: filepath.Join(dataDir, "certs"),
	})
	return output, compileDiagnostics(compileErrs)
}

func validateWithEnvoy(ctx context.Context, dataDir string) []diagnostic {
	output, diagnostics := compileDraft(ctx, dataDir, compile.ModeStatic)
	if output == nil || hasErrorDiagnostics(diagnostics) {
		return diagnostics
	}
	content, err := static.Render(output)
	if err != nil {
		return []diagnostic{{Stage: "envoy", Message: err.Error(), Severity: "error"}}
	}
	binary, err := envoycheck.FindBinary("")
	if err != nil {
		if errors.Is(err, envoycheck.ErrBinaryNotFound) {
			return []diagnostic{{Stage: "envoy", Message: err.Error(), Severity: "warning"}}
		}
		return []diagnostic{{Stage: "envoy", Message: err.Error(), Severity: "error"}}
	}
	if err := envoycheck.Validate(ctx, binary, content, envoycheck.DefaultTimeout); err != nil {
		return []diagnostic{{Stage: "envoy", Message: err.Error(), Severity: "error"}}
	}
	return nil
}

func configObjects(config *protocol.ConfigSet) []objectRecord {
	if config == nil {
		return nil
	}
	objects := make([]objectRecord, 0, 1+len(config.Listeners)+len(config.Routes)+len(config.Upstreams)+len(config.Policies)+len(config.EnvoyResources))
	if value := config.Gateway; value != nil {
		objects = append(objects, objectRecord{Kind: value.Kind, Name: value.Metadata.Name, Origin: value.Origin, Value: value})
	}
	for _, value := range config.Listeners {
		objects = append(objects, objectRecord{Kind: value.Kind, Name: value.Metadata.Name, Origin: value.Origin, Value: value})
	}
	for _, value := range config.Routes {
		objects = append(objects, objectRecord{Kind: value.Kind, Name: value.Metadata.Name, Origin: value.Origin, Value: value})
	}
	for _, value := range config.Upstreams {
		objects = append(objects, objectRecord{Kind: value.Kind, Name: value.Metadata.Name, Origin: value.Origin, Value: value})
	}
	for _, value := range config.Policies {
		objects = append(objects, objectRecord{Kind: value.Kind, Name: value.Metadata.Name, Origin: value.Origin, Value: value})
	}
	for _, value := range config.EnvoyResources {
		objects = append(objects, objectRecord{Kind: value.Kind, Name: value.Metadata.Name, Origin: value.Origin, Value: value})
	}
	sort.Slice(objects, func(i, j int) bool {
		if objects[i].Kind != objects[j].Kind {
			return objects[i].Kind < objects[j].Kind
		}
		return objects[i].Name < objects[j].Name
	})
	return objects
}

func readObjectDocument(w http.ResponseWriter, r *http.Request) ([]byte, protocol.Envelope, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	content, err := io.ReadAll(r.Body)
	if err != nil || len(content) == 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid or empty object body")
		return nil, protocol.Envelope{}, false
	}
	jsonContent := content
	if strings.Contains(r.Header.Get("Content-Type"), "yaml") {
		var value any
		if err := yamlv3.Unmarshal(content, &value); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid YAML object body")
			return nil, protocol.Envelope{}, false
		}
		jsonContent, err = json.Marshal(value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid YAML object body")
			return nil, protocol.Envelope{}, false
		}
	}
	var envelope protocol.Envelope
	decoder := json.NewDecoder(strings.NewReader(string(jsonContent)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || envelope.APIVersion != protocol.APIVersionV1Alpha1 || !envelope.Kind.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid protocol object envelope")
		return nil, protocol.Envelope{}, false
	}
	return content, envelope, true
}

func currentSourceFiles(dataDir string, draft *conf.Draft) ([]conf.SourceFile, error) {
	files := make([]conf.SourceFile, 0, len(draft.Files))
	for _, file := range draft.Files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(dataDir, file)
		if err != nil {
			return nil, err
		}
		files = append(files, conf.SourceFile{Path: filepath.ToSlash(rel), Content: content})
	}
	return files, nil
}

func objectYAML(value any) ([]byte, error) {
	content, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var plain any
	if err := json.Unmarshal(content, &plain); err != nil {
		return nil, err
	}
	return yamlv3.Marshal(plain)
}

func loadDiagnostics(values []protocol.LoadError) []diagnostic {
	out := make([]diagnostic, 0, len(values))
	for _, value := range values {
		out = append(out, diagnostic{Stage: "schema", Source: map[string]any{"file": value.Origin.File, "docIndex": value.Origin.DocIndex}, Message: value.Message, Severity: "error"})
	}
	return out
}

func compileDiagnostics(values []compile.CompileError) []diagnostic {
	out := make([]diagnostic, 0, len(values))
	for _, value := range values {
		severity := strings.ToLower(string(value.Severity))
		source := map[string]any{"file": value.Source.File, "kind": value.Source.Kind, "name": value.Source.Name, "path": value.Source.Path}
		out = append(out, diagnostic{Stage: string(value.Stage), Source: source, Message: value.Message, Severity: severity})
	}
	return out
}

func hasErrorDiagnostics(values []diagnostic) bool {
	for _, value := range values {
		if value.Severity == "error" {
			return true
		}
	}
	return false
}

func writeValidationError(w http.ResponseWriter, details []diagnostic) {
	writeJSON(w, http.StatusUnprocessableEntity, ErrorResponse{Error: APIError{Code: "VALIDATION_FAILED", Message: "configuration validation failed", Details: details}})
}

func writeConfigError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, os.ErrNotExist):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "configuration resource not found")
	case errors.Is(err, conf.ErrDraftChanged), errors.Is(err, conf.ErrPublishActive), strings.Contains(err.Error(), "force is required"), strings.Contains(strings.ToLower(err.Error()), "conflict"):
		writeError(w, http.StatusConflict, "CONFLICT", err.Error())
	case strings.Contains(err.Error(), "validate"), strings.Contains(err.Error(), "draft load failed"), strings.Contains(err.Error(), "compile"):
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_FAILED", err.Error())
	case strings.Contains(err.Error(), "invalid"), strings.Contains(err.Error(), "unsafe"), strings.Contains(err.Error(), "required"):
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
	default:
		slog.ErrorContext(r.Context(), "configuration API failed", "method", r.Method, "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "INTERNAL", "internal server error")
	}
}

func pagination(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	limit, offset := 100, 0
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 1000 {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "limit must be 1-1000")
			return 0, 0, false
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "offset must be non-negative")
			return 0, 0, false
		}
	}
	return limit, offset, true
}

func versionPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	seq, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || seq <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "version id must be positive")
		return 0, false
	}
	return seq, true
}

func versionResponse(version store.Version) map[string]any {
	return map[string]any{
		"id": version.Seq, "createdAt": version.CreatedAt, "author": version.Author,
		"message": version.Message, "mode": version.Mode, "irVersion": version.IRVersion,
		"state": version.State, "parentId": version.ParentSeq, "rollbackOf": version.RollbackOf,
	}
}

func publishResponse(version store.Version, status deliver.Status, mode string) map[string]any {
	return map[string]any{"version": versionResponse(version), "delivery": deliveryResponse(status, mode)}
}

func deliveryResponse(status deliver.Status, mode string) map[string]any {
	return map[string]any{"mode": mode, "state": status.Phase, "version": status.Version, "detail": status.Detail, "updatedAt": status.UpdatedAt}
}

func requestUser(r *http.Request) string {
	if session, ok := r.Context().Value(sessionContextKey).(auth.Session); ok {
		return session.Username
	}
	return "unknown"
}

func compileMode(raw string, fallback compile.Mode) (compile.Mode, bool) {
	if raw == "" {
		if fallback == "" {
			fallback = compile.ModeXDS
		}
		return fallback, fallback == compile.ModeXDS || fallback == compile.ModeStatic
	}
	mode := compile.Mode(raw)
	return mode, mode == compile.ModeXDS || mode == compile.ModeStatic
}

func sourceMode(value string) (string, bool) {
	switch value {
	case "protocol":
		return conf.ModeAbstract, true
	case "native":
		return conf.ModeNative, true
	default:
		return "", false
	}
}

func sourceType(mode string) string {
	if mode == conf.ModeNative {
		return "native"
	}
	return "protocol"
}

func snapshotDataDir(dataDir string, seq int64) string {
	return filepath.Join(dataDir, "versions", fmt.Sprintf("%06d", seq), "config")
}

func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	return decodeJSON(w, r, target)
}
