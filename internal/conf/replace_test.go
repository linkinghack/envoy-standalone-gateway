package conf

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceDraftProtocolNativeAndConflict(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	protocolFile := SourceFile{Path: "config.d/listener.yaml", Content: []byte(`apiVersion: esgw/v1alpha1
kind: Listener
metadata: {name: web}
spec: {port: 8080, protocol: HTTP}
`)}
	firstHash, err := ReplaceDraft(dataDir, ModeAbstract, []SourceFile{protocolFile}, "")
	if err != nil || firstHash == "" {
		t.Fatalf("replace protocol hash=%q err=%v", firstHash, err)
	}
	draft, loadErrs, err := LoadDraft(dataDir)
	if err != nil || len(loadErrs) != 0 || draft.Mode != ModeAbstract || len(draft.Config.Listeners) != 1 {
		t.Fatalf("protocol draft=%+v load=%v err=%v", draft, loadErrs, err)
	}
	if _, err := ReplaceDraft(dataDir, ModeAbstract, nil, "stale-hash"); !errors.Is(err, ErrDraftChanged) {
		t.Fatalf("conflict error = %v", err)
	}

	native := SourceFile{Path: "native.yaml", Content: []byte(`
node: {id: native-node}
static_resources: {}
`)}
	secondHash, err := ReplaceDraft(dataDir, ModeNative, []SourceFile{native}, firstHash)
	if err != nil || secondHash == firstHash {
		t.Fatalf("replace native hash=%q err=%v", secondHash, err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "config.d", "listener.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old protocol source retained: %v", err)
	}
	draft, loadErrs, err = LoadDraft(dataDir)
	if err != nil || len(loadErrs) != 0 || draft.Mode != ModeNative {
		t.Fatalf("native draft=%+v load=%v err=%v", draft, loadErrs, err)
	}
}

func TestReplaceDraftRejectsUnsafeAndInvalidSources(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	for _, file := range []SourceFile{
		{Path: "../escape.yaml", Content: []byte("x")},
		{Path: "/absolute.yaml", Content: []byte("x")},
		{Path: "config.d/not-yaml.txt", Content: []byte("x")},
	} {
		if _, err := ReplaceDraft(dataDir, ModeAbstract, []SourceFile{file}, ""); err == nil {
			t.Fatalf("unsafe source accepted: %q", file.Path)
		}
	}
	bad := SourceFile{Path: "config.d/bad.yaml", Content: []byte("kind: Listener\nbogus: true\n")}
	if _, err := ReplaceDraft(dataDir, ModeAbstract, []SourceFile{bad}, ""); err == nil {
		t.Fatal("invalid protocol source accepted")
	}
	if _, err := ReplaceDraft(dataDir, ModeNative, []SourceFile{{Path: "native.yaml", Content: []byte("bogus: true\n")}}, ""); err == nil {
		t.Fatal("invalid native source accepted")
	}
}
