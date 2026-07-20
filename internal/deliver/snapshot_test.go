package deliver

import (
	"strings"
	"testing"

	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
)

func TestSnapshotJSONDeterministicAndNil(t *testing.T) {
	if _, err := SnapshotJSON(nil); err == nil {
		t.Fatal("expected nil IR error")
	}
	first, err := SnapshotJSON(&ir.IR{
		Version: "v1",
		Listeners: map[string]*listenerv3.Listener{
			"lis/b": {Name: "lis/b"},
			"lis/a": {Name: "lis/a"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := SnapshotJSON(&ir.IR{
		Version: "v1",
		Listeners: map[string]*listenerv3.Listener{
			"lis/a": {Name: "lis/a"},
			"lis/b": {Name: "lis/b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("snapshot is not deterministic:\n%s\n%s", first, second)
	}
	if !strings.Contains(string(first), `"version": "v1"`) || !strings.Contains(string(first), `"lis/a"`) {
		t.Fatalf("snapshot=%s", first)
	}
}
