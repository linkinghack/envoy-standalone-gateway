package compile

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

// TestPatchFuzz patch 模糊测试（编译层 §8.5）：随机 JSON Patch / Merge Patch 施加于
// 合法编译产物，断言「要么编译错误要么产物过校验」，无第三态（不允许编译成功
// 但产物非法，也不允许 panic）。固定种子保证可复现；`-short` 跳过。
func TestPatchFuzz(t *testing.T) {
	if testing.Short() {
		t.Skip("fuzz test skipped in -short mode")
	}
	rng := rand.New(rand.NewSource(260719))

	paths := []string{
		"/connect_timeout", "/name", "/lb_policy", "/dns_lookup_family",
		"/type", "/load_assignment", "/circuit_breakers", "/common_lb_config",
		"/load_assignment/cluster_name", "/load_assignment/endpoints",
		"/load_assignment/endpoints/0", "/respect_dns_ttl", "/no_such_field",
	}
	values := []any{
		"5s", "abc", "", 0, -1, 300, true, nil,
		"V4_ONLY", "AUTO", "ROUND_ROBIN", 99999,
		"us/app", "us/other", "us/",
		map[string]any{},
		[]any{},
		map[string]any{"cluster_name": "us/app"},
		map[string]any{"thresholds": []any{}},
	}
	ops := []string{"add", "remove", "replace"}

	for i := 0; i < 300; i++ {
		t.Run(fmt.Sprintf("iter-%d", i), func(t *testing.T) {
			cs := baseHTTPConfig()
			var patch protocol.EnvoyPatch
			if rng.Intn(2) == 0 {
				// JSON Merge Patch：随机 1~3 个键。
				doc := map[string]any{}
				for j := 0; j < 1+rng.Intn(3); j++ {
					p := paths[rng.Intn(len(paths))]
					doc[p[1:]] = values[rng.Intn(len(values))]
				}
				raw, _ := json.Marshal(doc)
				patch = mergePatch("cluster", string(raw))
			} else {
				// JSON Patch：随机 1~4 个操作。
				var list []map[string]any
				for j := 0; j < 1+rng.Intn(4); j++ {
					op := map[string]any{
						"op":   ops[rng.Intn(len(ops))],
						"path": paths[rng.Intn(len(paths))],
					}
					if op["op"] != "remove" {
						op["value"] = values[rng.Intn(len(values))]
					}
					list = append(list, op)
				}
				raw, _ := json.Marshal(list)
				patch = jsonPatch("cluster", string(raw))
			}
			cs.Upstreams[0].Spec.EnvoyPatch = []protocol.EnvoyPatch{patch}

			out, errs := Compile(cs, Options{Mode: ModeStatic})
			// 无第三态：产物非nil ⟺ 零错误。
			if (out == nil) != hasErrors(errs) {
				t.Fatalf("third state: IR nil=%v, errs=%v (patch=%s)", out == nil, formatErrs(errs), patch.Value)
			}
			if out == nil {
				return
			}
			// 产物必须过校验：独立重跑 F6 仍零错误。
			if verrs := validateIR(out); hasErrors(verrs) {
				t.Fatalf("compiled IR failed re-validation: %v (patch=%s)", formatErrs(verrs), patch.Value)
			}
			if _, err := out.ComputeVersion(); err != nil {
				t.Fatalf("ComputeVersion: %v", err)
			}
		})
	}
}
