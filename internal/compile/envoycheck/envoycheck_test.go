package envoycheck_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/compile/envoycheck"
)

// writeFakeEnvoy 在临时目录写一个名为 envoy 的可执行 shell 脚本。
func writeFakeEnvoy(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "envoy")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFindBinaryExplicit(t *testing.T) {
	good := writeFakeEnvoy(t, "exit 0")
	p, err := envoycheck.FindBinary(good)
	if err != nil || p != good {
		t.Fatalf("FindBinary(explicit) = %q, %v", p, err)
	}
	if _, err := envoycheck.FindBinary(filepath.Join(t.TempDir(), "nonexistent")); !errors.Is(err, envoycheck.ErrBinaryNotFound) {
		t.Fatalf("want ErrBinaryNotFound, got %v", err)
	}
}

func TestFindBinaryEnvVar(t *testing.T) {
	good := writeFakeEnvoy(t, "exit 0")
	t.Setenv(envoycheck.EnvVarPath, good)
	p, err := envoycheck.FindBinary("")
	if err != nil || p != good {
		t.Fatalf("FindBinary(env) = %q, %v", p, err)
	}
	t.Setenv(envoycheck.EnvVarPath, filepath.Join(t.TempDir(), "nonexistent"))
	if _, err := envoycheck.FindBinary(""); !errors.Is(err, envoycheck.ErrBinaryNotFound) {
		t.Fatalf("want ErrBinaryNotFound, got %v", err)
	}
	// 显式路径优先于环境变量。
	p, err = envoycheck.FindBinary(good)
	if err != nil || p != good {
		t.Fatalf("explicit must win over env: %q, %v", p, err)
	}
}

func TestFindBinaryNotExecutable(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "envoy")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o644); err != nil { // 无可执行位
		t.Fatal(err)
	}
	if _, err := envoycheck.FindBinary(p); !errors.Is(err, envoycheck.ErrBinaryNotFound) {
		t.Fatalf("want ErrBinaryNotFound, got %v", err)
	}
}

func TestValidateSuccess(t *testing.T) {
	fake := writeFakeEnvoy(t, `echo "config 'ok' OK" >&2; exit 0`)
	if err := envoycheck.Validate(context.Background(), fake, []byte("node: {id: x}\n"), 5*time.Second); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateFailureCapturesStderr(t *testing.T) {
	fake := writeFakeEnvoy(t, `echo "Proto constraint validation failed: bad field" >&2; exit 1`)
	err := envoycheck.Validate(context.Background(), fake, []byte("bad: config\n"), 5*time.Second)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "bad field") {
		t.Fatalf("stderr not captured: %v", err)
	}
}

func TestValidatePassesArgs(t *testing.T) {
	// 假 envoy 断言自己收到的参数形态（--mode validate -c <file>）。
	fake := writeFakeEnvoy(t, `[ "$1" = "--mode" ] && [ "$2" = "validate" ] && [ "$3" = "-c" ] && [ -f "$4" ] || exit 3`)
	if err := envoycheck.Validate(context.Background(), fake, []byte("x\n"), 5*time.Second); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateTimeout(t *testing.T) {
	fake := writeFakeEnvoy(t, "sleep 5")
	err := envoycheck.Validate(context.Background(), fake, []byte("x\n"), 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout error, got %v", err)
	}
}
