package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

func TestSchemaCommand(t *testing.T) {
	code, stdout, stderr := runCLI(t, "schema")
	if code != 0 || stderr != "" {
		t.Fatalf("schema exit=%d stderr=%q", code, stderr)
	}
	want, err := protocol.Schemas()
	if err != nil {
		t.Fatal(err)
	}
	if stdout != string(want)+"\n" {
		t.Fatal("schema stdout differs from protocol.Schemas")
	}

	out := filepath.Join(t.TempDir(), "schema.json")
	code, stdout, stderr = runCLI(t, "schema", "-o", out)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("schema file exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want)+"\n" {
		t.Fatal("schema file differs from protocol.Schemas")
	}
}

func TestConformanceCommand(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		dir := t.TempDir()
		writeConformanceInput(t, dir, `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: web}
spec: {port: 8080, protocol: HTTP}
`)
		code, stdout, stderr := runCLI(t, "conformance", "-f", dir)
		if code != 0 || stderr != "" {
			t.Fatalf("exit=%d stderr=%q report=%s", code, stderr, stdout)
		}
		var report conformanceReport
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatal(err)
		}
		if !report.Valid || len(report.Diagnostics) != 0 {
			t.Fatalf("report=%+v", report)
		}
	})

	t.Run("schema diagnostic", func(t *testing.T) {
		dir := t.TempDir()
		writeConformanceInput(t, dir, `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: web}
spec: {port: 8080, protocol: HTTP, typo: true}
`)
		code, stdout, _ := runCLI(t, "conformance", "-f", dir)
		if code != 1 {
			t.Fatalf("exit=%d report=%s", code, stdout)
		}
		if !strings.Contains(stdout, `"code": "ESGW_SCHEMA_INVALID"`) ||
			!strings.Contains(stdout, `"docIndex": 0`) || !strings.Contains(stdout, `unknown field \"typo\"`) {
			t.Fatalf("report=%s", stdout)
		}
	})

	t.Run("link diagnostic with source ref", func(t *testing.T) {
		dir := t.TempDir()
		writeConformanceInput(t, dir, `
apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: web}
spec: {port: 8080, protocol: HTTP}
---
apiVersion: esgw/v1alpha1
kind: Route
metadata: {name: api}
spec:
  listeners: [web]
  rules:
    - match: {path: {prefix: /}}
      backends: [{upstream: missing}]
`)
		code, stdout, _ := runCLI(t, "conformance", "-f", dir)
		if code != 1 {
			t.Fatalf("exit=%d report=%s", code, stdout)
		}
		for _, want := range []string{
			`"code": "ESGW_LINK_INVALID"`, `"kind": "Route"`, `"name": "api"`,
			`"path": "spec.rules[0].backends[0].upstream"`,
		} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("missing %s in report=%s", want, stdout)
			}
		}
	})
}

func writeConformanceInput(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
