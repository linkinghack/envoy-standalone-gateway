package protocol

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func itoa(i int) string { return strconv.Itoa(i) }
