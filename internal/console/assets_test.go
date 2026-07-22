package console

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedConsoleAssets(t *testing.T) {
	index, err := fs.ReadFile(Assets(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `<div id="root"></div>`) || !strings.Contains(string(index), `/assets/`) {
		t.Fatalf("unexpected embedded index: %s", index)
	}
	entries, err := fs.ReadDir(Assets(), "assets")
	if err != nil || len(entries) < 3 {
		t.Fatalf("embedded assets = %d, %v", len(entries), err)
	}
}
