package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	schemav6 "github.com/santhosh-tekuri/jsonschema/v6"
	yamlv3 "gopkg.in/yaml.v3"
)

func TestReleasedSchemaAndExamples(t *testing.T) {
	root := filepath.Join("..", "..", "protocol")
	committed, err := os.ReadFile(filepath.Join(root, "schema", "v1alpha1.json"))
	if err != nil {
		t.Fatal(err)
	}
	generated, err := Schemas()
	if err != nil {
		t.Fatal(err)
	}
	generated = append(generated, '\n')
	if !bytes.Equal(committed, generated) {
		t.Fatal("released schema is stale; run `esgw schema -o protocol/schema/v1alpha1.json`")
	}

	doc, err := schemav6.UnmarshalJSON(bytes.NewReader(committed))
	if err != nil {
		t.Fatal(err)
	}
	compiler := schemav6.NewCompiler()
	if err := compiler.AddResource(SchemaID, doc); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(SchemaID)
	if err != nil {
		t.Fatal(err)
	}

	valid, err := filepath.Glob(filepath.Join(root, "examples", "valid", "*", "config.yaml"))
	if err != nil || len(valid) == 0 {
		t.Fatalf("valid example discovery: files=%v err=%v", valid, err)
	}
	for _, file := range valid {
		t.Run(filepath.Base(filepath.Dir(file)), func(t *testing.T) {
			validateReleasedExample(t, schema, file, false)
		})
	}

	invalid, err := filepath.Glob(filepath.Join(root, "examples", "invalid", "*", "config.yaml"))
	if err != nil || len(invalid) == 0 {
		t.Fatalf("invalid example discovery: files=%v err=%v", invalid, err)
	}
	for _, file := range invalid {
		name := filepath.Base(filepath.Dir(file))
		t.Run(name, func(t *testing.T) {
			validateReleasedExample(t, schema, file, strings.HasPrefix(name, "schema-"))
		})
	}
}

func validateReleasedExample(t *testing.T, schema *schemav6.Schema, file string, wantSchemaError bool) {
	t.Helper()
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	dec := yamlv3.NewDecoder(bytes.NewReader(content))
	hadSchemaError := false
	for idx := 0; ; idx++ {
		var node yamlv3.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("doc[%d]: %v", idx, err)
		}
		data, err := yamlNodeToJSON(&node)
		if err != nil {
			t.Fatalf("doc[%d]: %v", idx, err)
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatalf("doc[%d]: %v", idx, err)
		}
		if err := schema.Validate(value); err != nil {
			hadSchemaError = true
			if !wantSchemaError {
				t.Fatalf("doc[%d] schema validation: %v", idx, err)
			}
		}
	}
	if hadSchemaError != wantSchemaError {
		t.Fatalf("schema error=%v, want %v", hadSchemaError, wantSchemaError)
	}
}
