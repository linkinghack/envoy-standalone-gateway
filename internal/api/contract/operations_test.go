package contract

import (
	"os"
	"path/filepath"
	"testing"

	yamlv3 "gopkg.in/yaml.v3"
)

type specOperation struct {
	OperationID string `yaml:"operationId"`
	Parameters  []struct {
		Ref string `yaml:"$ref"`
	} `yaml:"parameters"`
}

type openAPIDoc struct {
	OpenAPI string              `yaml:"openapi"`
	Paths   map[string]pathItem `yaml:"paths"`
}

type pathItem struct {
	Get    *specOperation `yaml:"get"`
	Post   *specOperation `yaml:"post"`
	Put    *specOperation `yaml:"put"`
	Delete *specOperation `yaml:"delete"`
	Patch  *specOperation `yaml:"patch"`
}

func TestRegistryMatchesOpenAPI(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc openAPIDoc
	if err := yamlv3.Unmarshal(content, &doc); err != nil {
		t.Fatalf("parse OpenAPI YAML: %v", err)
	}
	if doc.OpenAPI != "3.0.3" {
		t.Fatalf("openapi = %q, want 3.0.3", doc.OpenAPI)
	}

	fromSpec := make(map[string]Operation)
	for path, item := range doc.Paths {
		operations := map[string]*specOperation{
			"GET": item.Get, "POST": item.Post, "PUT": item.Put, "DELETE": item.Delete, "PATCH": item.Patch,
		}
		for method, operation := range operations {
			if operation == nil {
				continue
			}
			if operation.OperationID == "" {
				t.Fatalf("%s %s has no operationId", method, path)
			}
			fullPath := "/api/v1" + path
			if path == "/healthz" || path == "/readyz" || path == "/metrics" {
				fullPath = path
			}
			if _, exists := fromSpec[operation.OperationID]; exists {
				t.Fatalf("duplicate operationId %q", operation.OperationID)
			}
			fromSpec[operation.OperationID] = Operation{ID: operation.OperationID, Method: method, Path: fullPath}
			if method != "GET" && !hasCSRFParameter(*operation) {
				t.Errorf("%s %s (%s) has no CsrfHeader", method, path, operation.OperationID)
			}
		}
	}
	if len(fromSpec) != len(Operations) {
		t.Fatalf("spec operations = %d, registry = %d", len(fromSpec), len(Operations))
	}
	seen := make(map[string]bool)
	for _, operation := range Operations {
		if seen[operation.ID] {
			t.Fatalf("duplicate registry operationId %q", operation.ID)
		}
		seen[operation.ID] = true
		from, ok := fromSpec[operation.ID]
		if !ok {
			t.Errorf("registry operation %q missing from spec", operation.ID)
			continue
		}
		if from.Method != operation.Method || from.Path != operation.Path {
			t.Errorf("%s registry = %s %s, spec = %s %s", operation.ID, operation.Method, operation.Path, from.Method, from.Path)
		}
	}
}

func hasCSRFParameter(operation specOperation) bool {
	for _, parameter := range operation.Parameters {
		if parameter.Ref == "#/components/parameters/CsrfHeader" {
			return true
		}
	}
	return false
}
