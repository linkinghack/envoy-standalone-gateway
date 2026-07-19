package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"
)

// RawJSON 承载任意 JSON（escape hatch 的 patch value 与 EnvoyResources 原生资源）。
type RawJSON json.RawMessage

// MarshalJSON 实现 json.Marshaler。
func (r RawJSON) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}
	return r, nil
}

// UnmarshalJSON 实现 json.Unmarshaler。
func (r *RawJSON) UnmarshalJSON(b []byte) error {
	*r = append((*r)[:0], b...)
	return nil
}

// JSONSchema 实现自定义 schema 钩子：任意 JSON 值。
func (RawJSON) JSONSchema() *jsonschema.Schema {
	return jsonschema.TrueSchema
}

// PatchOp 是 envoyPatch.op 的枚举（协议 §7.1）。
type PatchOp string

// PatchOp 的合法取值。
const (
	PatchOpMerge     PatchOp = "merge"     // JSON Merge Patch (RFC 7386)
	PatchOpJSONPatch PatchOp = "jsonPatch" // RFC 6902
)

// Valid 报告取值是否合法。
func (o PatchOp) Valid() bool {
	return o == PatchOpMerge || o == PatchOpJSONPatch
}

// JSONSchema 实现自定义 schema 钩子。
func (o PatchOp) JSONSchema() *jsonschema.Schema {
	return enumSchema(PatchOpMerge, PatchOpJSONPatch)
}

// EnvoyPatch 是对象级 escape hatch（协议 §7.1）：作用于该对象编译出的 Envoy 资源。
// Target 的合法取值由对象 kind 决定（全集见编译层未决事项 C1），加载期不做封闭枚举校验。
type EnvoyPatch struct {
	Target string  `json:"target"`
	Op     PatchOp `json:"op"`
	Value  RawJSON `json:"value"`
}

func (p *EnvoyPatch) validate(field string) error {
	if p.Target == "" {
		return fmt.Errorf("%s: target is required", field)
	}
	if !p.Op.Valid() {
		return fmt.Errorf("%s: invalid op %q (want merge | jsonPatch)", field, p.Op)
	}
	if len(p.Value) == 0 {
		return fmt.Errorf("%s: value is required", field)
	}
	return nil
}

func validateEnvoyPatches(field string, patches []EnvoyPatch) error {
	for i := range patches {
		if err := patches[i].validate(fmt.Sprintf("%s[%d]", field, i)); err != nil {
			return err
		}
	}
	return nil
}
