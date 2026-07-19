package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	schemav6 "github.com/santhosh-tekuri/jsonschema/v6"
	yamlv3 "gopkg.in/yaml.v3"
)

// compileSchema 断言 Schemas() 的 bundle 能被标准 JSON Schema 校验器加载。
func compileSchema(t *testing.T) *schemav6.Schema {
	t.Helper()
	bundle, err := Schemas()
	if err != nil {
		t.Fatalf("Schemas: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(bundle, &raw); err != nil {
		t.Fatalf("bundle is not valid JSON: %v", err)
	}
	if oneOf, ok := raw["oneOf"].([]any); !ok || len(oneOf) != 6 {
		t.Fatalf("bundle oneOf should cover 6 kinds: %v", raw["oneOf"])
	}
	doc, err := schemav6.UnmarshalJSON(bytes.NewReader(bundle))
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	c := schemav6.NewCompiler()
	if err := c.AddResource(SchemaID, doc); err != nil {
		t.Fatalf("AddResource: %v", err)
	}
	sch, err := c.Compile(SchemaID)
	if err != nil {
		t.Fatalf("schema compile: %v", err)
	}
	return sch
}

func TestSchemasCompile(t *testing.T) {
	compileSchema(t)
}

// TestSchemasValidateExerciseYAML 用 schema 校验协议 §8.1/§8.2 的全部文档。
func TestSchemasValidateExerciseYAML(t *testing.T) {
	sch := compileSchema(t)
	for _, file := range []string{"s1", "s2"} {
		content, err := os.ReadFile(filepath.Join("testdata", file, "exercise.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		dec := yamlv3.NewDecoder(bytes.NewReader(content))
		for idx := 0; ; idx++ {
			var node yamlv3.Node
			err := dec.Decode(&node)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("%s doc[%d]: %v", file, idx, err)
			}
			j, err := yamlNodeToJSON(&node)
			if err != nil {
				t.Fatalf("%s doc[%d]: %v", file, idx, err)
			}
			if string(j) == "null" {
				continue
			}
			var v any
			if err := json.Unmarshal(j, &v); err != nil {
				t.Fatalf("%s doc[%d]: %v", file, idx, err)
			}
			if err := sch.Validate(v); err != nil {
				t.Fatalf("%s doc[%d] schema validation failed: %v", file, idx, err)
			}
		}
	}
}

// TestSchemasRejectsBadDocs 校验器必须拒绝坏枚举与未知字段（与 strict decode 对齐）。
func TestSchemasRejectsBadDocs(t *testing.T) {
	sch := compileSchema(t)
	bad := []struct{ name, doc string }{
		{"unknown field", `{"apiVersion":"esgw/v1alpha1","kind":"Listener","metadata":{"name":"x"},"spec":{"port":80,"protocol":"HTTP","typo":1}}`},
		{"bad enum", `{"apiVersion":"esgw/v1alpha1","kind":"Listener","metadata":{"name":"x"},"spec":{"port":80,"protocol":"HTTP2"}}`},
		{"bad name", `{"apiVersion":"esgw/v1alpha1","kind":"Listener","metadata":{"name":"Bad_Name"},"spec":{"port":80,"protocol":"HTTP"}}`},
		{"bad apiVersion", `{"apiVersion":"esgw/v9","kind":"Listener","metadata":{"name":"x"},"spec":{"port":80,"protocol":"HTTP"}}`},
		{"bad duration", `{"apiVersion":"esgw/v1alpha1","kind":"Gateway","metadata":{"name":"default"},"spec":{"http":{"idleTimeout":"soon"}}}`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			var v any
			if err := json.Unmarshal([]byte(tc.doc), &v); err != nil {
				t.Fatal(err)
			}
			if err := sch.Validate(v); err == nil {
				t.Fatalf("schema should reject %s", tc.doc)
			}
		})
	}
}
