package protocol

import (
	"encoding/json"

	"github.com/invopop/jsonschema"
)

// SchemaID 是 v1alpha1 JSON Schema bundle 的 $id。
const SchemaID = "https://linkinghack.com/esgw/schemas/v1alpha1.json"

// Schemas 返回协议 v1alpha1 的 JSON Schema bundle（C3 决议：由 Go 类型用
// invopop/jsonschema 生成，单一事实来源，协议 §6）。bundle 形态：
// 顶层 oneOf 六个对象文档 schema，共享 $defs；strict decode 对应
// additionalProperties: false。
func Schemas() ([]byte, error) {
	r := &jsonschema.Reflector{
		Anonymous:                 true,  // 不生成 $id，引用统一走 #/$defs/<Type>
		AllowAdditionalProperties: false, // 与 strict decode 对齐（协议 P6）
	}

	bundle := &jsonschema.Schema{
		Version:     jsonschema.Version,
		ID:          SchemaID,
		Title:       "esgw gateway config protocol v1alpha1",
		Definitions: jsonschema.Definitions{},
	}

	docs := []struct {
		kind Kind
		v    any
	}{
		{KindGateway, &Gateway{}},
		{KindListener, &Listener{}},
		{KindRoute, &Route{}},
		{KindUpstream, &Upstream{}},
		{KindPolicy, &Policy{}},
		{KindEnvoyResources, &EnvoyResources{}},
	}
	for _, d := range docs {
		s := r.Reflect(d.v)
		bundle.OneOf = append(bundle.OneOf, &jsonschema.Schema{Ref: s.Ref})
		for name, def := range s.Definitions {
			bundle.Definitions[name] = def
		}
		// 每个文档的 apiVersion/kind 收敛为常量，保证 oneOf 精确分流。
		if def := bundle.Definitions[string(d.kind)]; def != nil {
			def.Properties.Set("apiVersion", &jsonschema.Schema{Type: "string", Const: APIVersionV1Alpha1})
			def.Properties.Set("kind", &jsonschema.Schema{Type: "string", Const: string(d.kind)})
		}
	}

	// metadata.name 的正则（枚举 tag 无法表达含逗号的量词，改为后置注入）。
	if meta := bundle.Definitions["ObjectMeta"]; meta != nil {
		if name, ok := meta.Properties.Get("name"); ok {
			name.Pattern = NamePattern
		}
	}

	return json.MarshalIndent(bundle, "", "  ")
}
