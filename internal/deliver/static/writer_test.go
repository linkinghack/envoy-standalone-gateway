package static_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver/static"
)

func TestWriterAtomicReplaceAndFaultPreservesLastGood(t *testing.T) {
	path := filepath.Join(t.TempDir(), "envoy", "envoy.yaml")
	writer := static.Writer{OutputPath: path}
	if err := writer.Write([]byte("version: one\n")); err != nil {
		t.Fatal(err)
	}
	failing := static.Writer{OutputPath: path, BeforeRename: func(_, _ string) error { return errors.New("injected rename failure") }}
	if err := failing.Write([]byte("version: partial\n")); err == nil {
		t.Fatal("want injected failure")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "version: one\n" {
		t.Fatalf("last-good = %q, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v", info.Mode().Perm(), err)
	}
}
