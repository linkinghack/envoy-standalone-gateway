package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	yamlv3 "gopkg.in/yaml.v3"
)

// LoadError 是一条加载错误。错误收集不中断：LoadDir 会把全部错误带 Origin 返回。
type LoadError struct {
	Origin  Origin
	Message string
}

// Error 实现 error 接口。
func (e LoadError) Error() string {
	return fmt.Sprintf("%s: %s", e.Origin, e.Message)
}

// ConfigSet 是一个配置目录加载出的完整配置集（协议 §2.1）。
// v0 只有一个隐式 Gateway 实例，故 Gateway 为单对象指针（可省略，省略时取默认值）。
type ConfigSet struct {
	Gateway        *Gateway
	Listeners      []*Listener
	Routes         []*Route
	Upstreams      []*Upstream
	Policies       []*Policy
	EnvoyResources []*EnvoyResources
}

// LoadDir 扫描 dir 下全部 *.yaml|*.yml 文件（按文件名字典序），逐文件按 `---`
// 拆分多文档，strict decode（未知字段报错，协议 P6）后合并为一个 ConfigSet。
// 同 kind 重名报错（错误信息带两处 Origin）；全部错误收集后返回，不中断。
func LoadDir(dir string) (*ConfigSet, []LoadError) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []LoadError{{Origin: Origin{File: dir}, Message: err.Error()}}
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch ext := filepath.Ext(e.Name()); ext {
		case ".yaml", ".yml":
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)

	cs := &ConfigSet{}
	var errs []LoadError
	seen := map[Kind]map[string]Origin{}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			errs = append(errs, LoadError{Origin: Origin{File: file}, Message: err.Error()})
			continue
		}
		docErrs := loadFile(cs, seen, file, content)
		errs = append(errs, docErrs...)
	}
	return cs, errs
}

// loadFile 解析单个文件的全部 YAML 文档并并入 ConfigSet。
func loadFile(cs *ConfigSet, seen map[Kind]map[string]Origin, file string, content []byte) []LoadError {
	var errs []LoadError
	// 多文档拆分与 YAML→JSON 转换用 yaml.v3（YAML 1.2 core schema）：比按行切 `---`
	// 可靠（字面量字符串中的 `---` 不会误切），且 `on:` 等键不会被 YAML 1.1
	// 误解析为布尔 true（协议 §3.3 的 retry.on 正是这个坑；SD2 原拟用的
	// sigs.k8s.io/yaml 基于 yaml.v2 即 YAML 1.1，故改用 v3，架构不变：
	// YAML→JSON→encoding/json DisallowUnknownFields strict decode）。
	dec := yamlv3.NewDecoder(bytes.NewReader(content))
	for idx := 0; ; idx++ {
		var node yamlv3.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		origin := Origin{File: file, DocIndex: idx}
		if err != nil {
			errs = append(errs, LoadError{Origin: origin, Message: fmt.Sprintf("YAML parse error: %v", err)})
			continue
		}
		jsonDoc, err := yamlNodeToJSON(&node)
		if err != nil {
			errs = append(errs, LoadError{Origin: origin, Message: fmt.Sprintf("YAML to JSON conversion failed: %v", err)})
			continue
		}
		if string(jsonDoc) == "null" {
			continue // 空文档（如文件以 `---` 开头）
		}
		if lerr := loadDoc(cs, seen, origin, jsonDoc); lerr != nil {
			errs = append(errs, *lerr)
		}
	}
	return errs
}

// yamlNodeToJSON 把一个 YAML 文档节点转换为 JSON（map[string]any / []any / 标量）。
func yamlNodeToJSON(doc *yamlv3.Node) ([]byte, error) {
	v, err := yamlNodeToAny(doc)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func yamlNodeToAny(n *yamlv3.Node) (any, error) {
	switch n.Kind {
	case yamlv3.DocumentNode:
		return yamlNodeToAny(n.Content[0])
	case yamlv3.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		for i := 0; i < len(n.Content); i += 2 {
			var key string
			if err := n.Content[i].Decode(&key); err != nil {
				return nil, fmt.Errorf("mapping keys must be strings: %w", err)
			}
			v, err := yamlNodeToAny(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			m[key] = v
		}
		return m, nil
	case yamlv3.SequenceNode:
		s := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := yamlNodeToAny(c)
			if err != nil {
				return nil, err
			}
			s = append(s, v)
		}
		return s, nil
	case yamlv3.ScalarNode:
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		return v, nil
	case yamlv3.AliasNode:
		return nil, errors.New("YAML aliases are not supported")
	default:
		return nil, fmt.Errorf("unsupported YAML node kind %d", n.Kind)
	}
}

// loadDoc 解析单个 YAML 文档（JSON 形态）：信封 → 按 kind 分发 → 结构层校验 → 重名检测。
func loadDoc(cs *ConfigSet, seen map[Kind]map[string]Origin, origin Origin, jsonDoc []byte) *LoadError {
	fail := func(format string, args ...any) *LoadError {
		return &LoadError{Origin: origin, Message: fmt.Sprintf(format, args...)}
	}

	var env Envelope
	if err := decodeStrict(bytes.NewReader(jsonDoc), &env); err != nil {
		return fail("strict decode failed: %v", err)
	}
	// 版本挂载点：协议 §6 的多版本共存（接受最近两个版本 + 内部转换）在后续版本实现；
	// v0 仅接受 esgw/v1alpha1。
	if env.APIVersion != APIVersionV1Alpha1 {
		return fail("unsupported apiVersion %q (want %q)", env.APIVersion, APIVersionV1Alpha1)
	}
	if !env.Kind.Valid() {
		return fail("unsupported kind %q (want Gateway | Listener | Route | Upstream | Policy | EnvoyResources)", env.Kind)
	}
	if err := ValidateName(env.Metadata.Name); err != nil {
		return fail("metadata.name: %v", err)
	}

	add := func(kind Kind, name string) *LoadError {
		if prev, ok := seen[kind][name]; ok {
			return fail("duplicate %s name %q: first defined at %s", kind, name, prev)
		}
		if seen[kind] == nil {
			seen[kind] = map[string]Origin{}
		}
		seen[kind][name] = origin
		return nil
	}

	specErr := func(spec any) error {
		if len(env.Spec) == 0 {
			return nil
		}
		return decodeStrict(bytes.NewReader(env.Spec), spec)
	}

	switch env.Kind {
	case KindGateway:
		// v0 只有一个隐式 Gateway 实例：显式声明时 name 必须为 default（协议 §2.2）。
		if env.Metadata.Name != DefaultGatewayName {
			return fail("Gateway metadata.name must be %q (v0 supports a single implicit Gateway)", DefaultGatewayName)
		}
		var g Gateway
		if err := specErr(&g.Spec); err != nil {
			return fail("spec: strict decode failed: %v", err)
		}
		if err := g.Spec.validate(); err != nil {
			return fail("%v", err)
		}
		if lerr := add(env.Kind, env.Metadata.Name); lerr != nil {
			return lerr
		}
		g.APIVersion, g.Kind, g.Metadata, g.Origin = env.APIVersion, env.Kind, env.Metadata, origin
		cs.Gateway = &g
	case KindListener:
		var l Listener
		if err := specErr(&l.Spec); err != nil {
			return fail("spec: strict decode failed: %v", err)
		}
		if err := l.Spec.validate(); err != nil {
			return fail("%v", err)
		}
		if lerr := add(env.Kind, env.Metadata.Name); lerr != nil {
			return lerr
		}
		l.APIVersion, l.Kind, l.Metadata, l.Origin = env.APIVersion, env.Kind, env.Metadata, origin
		cs.Listeners = append(cs.Listeners, &l)
	case KindRoute:
		var r Route
		if err := specErr(&r.Spec); err != nil {
			return fail("spec: strict decode failed: %v", err)
		}
		if err := r.Spec.validate(); err != nil {
			return fail("%v", err)
		}
		if lerr := add(env.Kind, env.Metadata.Name); lerr != nil {
			return lerr
		}
		r.APIVersion, r.Kind, r.Metadata, r.Origin = env.APIVersion, env.Kind, env.Metadata, origin
		cs.Routes = append(cs.Routes, &r)
	case KindUpstream:
		var u Upstream
		if err := specErr(&u.Spec); err != nil {
			return fail("spec: strict decode failed: %v", err)
		}
		if err := u.Spec.validate(); err != nil {
			return fail("%v", err)
		}
		if lerr := add(env.Kind, env.Metadata.Name); lerr != nil {
			return lerr
		}
		u.APIVersion, u.Kind, u.Metadata, u.Origin = env.APIVersion, env.Kind, env.Metadata, origin
		cs.Upstreams = append(cs.Upstreams, &u)
	case KindPolicy:
		var p Policy
		if err := specErr(&p.Spec); err != nil {
			return fail("spec: strict decode failed: %v", err)
		}
		if err := p.Spec.validate("spec"); err != nil {
			return fail("%v", err)
		}
		if lerr := add(env.Kind, env.Metadata.Name); lerr != nil {
			return lerr
		}
		p.APIVersion, p.Kind, p.Metadata, p.Origin = env.APIVersion, env.Kind, env.Metadata, origin
		cs.Policies = append(cs.Policies, &p)
	case KindEnvoyResources:
		var er EnvoyResources
		if err := specErr(&er.Spec); err != nil {
			return fail("spec: strict decode failed: %v", err)
		}
		if err := er.Spec.validate(); err != nil {
			return fail("%v", err)
		}
		if lerr := add(env.Kind, env.Metadata.Name); lerr != nil {
			return lerr
		}
		er.APIVersion, er.Kind, er.Metadata, er.Origin = env.APIVersion, env.Kind, env.Metadata, origin
		cs.EnvoyResources = append(cs.EnvoyResources, &er)
	}
	return nil
}

// decodeStrict 用 DisallowUnknownFields 做严格 JSON 解码（协议 P6：未知字段报错）。
func decodeStrict(r io.Reader, v any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected trailing data after JSON value")
	}
	return nil
}
