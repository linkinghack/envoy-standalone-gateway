package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestServeUsageError 覆盖用法错误：-c 必填，-f 可省略（exit 2）。
func TestServeUsageError(t *testing.T) {
	if code, _, _ := runCLI(t, "serve"); code != 2 {
		t.Fatalf("missing flags: exit = %d, want 2", code)
	}
	if code, _, _ := runCLI(t, "serve", "-f", "testdata/ok"); code != 2 {
		t.Fatalf("missing -c: exit = %d, want 2", code)
	}
}

// TestServeMissingConfig 覆盖 esgw.yaml 缺失/非法：exit 1。
func TestServeMissingConfig(t *testing.T) {
	code, _, stderr := runCLI(t, "serve",
		"-c", filepath.Join(t.TempDir(), "nonexistent.yaml"), "-f", "testdata/ok")
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "error: load esgw.yaml:") {
		t.Fatalf("missing load error line:\n%s", stderr)
	}

	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("deliver: {bogus: 1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = runCLI(t, "serve", "-c", bad, "-f", "testdata/ok")
	if code != 1 {
		t.Fatalf("invalid yaml: exit = %d, want 1; stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "error: load esgw.yaml:") {
		t.Fatalf("missing strict decode error:\n%s", stderr)
	}
}

// TestServeInvalidLogLevel 覆盖非法 -log-level：exit 2（用法错误）。
func TestServeInvalidLogLevel(t *testing.T) {
	code, _, stderr := runCLI(t, "serve",
		"-c", "x.yaml", "-f", "testdata/ok", "-log-level", "verbose")
	if code != 2 {
		t.Fatalf("invalid -log-level: exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "invalid -log-level") {
		t.Fatalf("missing -log-level error line:\n%s", stderr)
	}
}

// TestServeStaticMode 覆盖 SD2：deliver.mode=static → 明确报
// 「static 运行时下发未实现（S7）」exit 1。
func TestServeStaticMode(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "esgw.yaml")
	if err := os.WriteFile(cfg, []byte("deliver: {mode: static}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLI(t, "serve", "-c", cfg, "-f", "testdata/ok")
	if code != 1 {
		t.Fatalf("exit = %d, want 1; stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "static 运行时下发未实现（S7）") {
		t.Fatalf("missing static-mode error:\n%s", stderr)
	}
}

// TestUsageListsServe 确认顶层 usage 已收录 serve。
func TestUsageListsServe(t *testing.T) {
	code, _, stderr := runCLI(t)
	if code != 2 {
		t.Fatalf("no args: exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "serve") {
		t.Fatalf("usage missing serve:\n%s", stderr)
	}
}
