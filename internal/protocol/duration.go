package protocol

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/invopop/jsonschema"
)

// Duration 包装 time.Duration（SD3）：JSON/YAML 中以 Go duration 字符串
// 编解码（协议 §4：5s、1m30s、500ms）。
type Duration struct {
	time.Duration
}

// ParseDuration 解析 Go duration 字符串。
func ParseDuration(s string) (Duration, error) {
	v, err := time.ParseDuration(s)
	if err != nil {
		return Duration{}, fmt.Errorf("invalid duration %q: %w", s, err)
	}
	return Duration{v}, nil
}

// UnmarshalJSON 实现 json.Unmarshaler。
func (d *Duration) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a Go duration string (e.g. \"5s\", \"1m30s\", \"500ms\"): %w", err)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = v
	return nil
}

// MarshalJSON 实现 json.Marshaler。
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// JSONSchema 实现 invopop/jsonschema 的自定义 schema 钩子。
func (Duration) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:        "string",
		Pattern:     `^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`,
		Description: "Go duration 字符串，如 5s、1m30s、500ms（协议 §4）",
	}
}
