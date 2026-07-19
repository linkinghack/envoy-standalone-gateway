package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDurationJSON(t *testing.T) {
	var d Duration
	if err := json.Unmarshal([]byte(`"1m30s"`), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Duration != 90*time.Second {
		t.Fatalf("got %v", d.Duration)
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `"1m30s"` {
		t.Fatalf("marshal got %s", b)
	}

	if err := json.Unmarshal([]byte(`"10x"`), &d); err == nil {
		t.Fatal("bad duration string should error")
	}
	if err := json.Unmarshal([]byte(`42`), &d); err == nil {
		t.Fatal("non-string duration should error")
	}
}

func TestDurationYAMLPath(t *testing.T) {
	cs := expectLoadOK(t, `
apiVersion: esgw/v1alpha1
kind: Gateway
metadata: {name: default}
spec:
  http: {idleTimeout: 500ms}
`)
	if cs.Gateway.Spec.HTTP.IdleTimeout.Duration != 500*time.Millisecond {
		t.Fatalf("idleTimeout: %v", cs.Gateway.Spec.HTTP.IdleTimeout)
	}

	expectLoadErr(t, `
apiVersion: esgw/v1alpha1
kind: Gateway
metadata: {name: default}
spec:
  http: {idleTimeout: soon}
`, `invalid duration`)
}
