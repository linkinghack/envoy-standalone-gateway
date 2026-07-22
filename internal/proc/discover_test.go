package proc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeEnvoy(t *testing.T, versionOutput, extra string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "envoy")
	script := "#!/bin/sh\n" + extra + "\nprintf '%s\\n' '" + versionOutput + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoverPriorityAndCompatibility(t *testing.T) {
	environment := fakeEnvoy(t, "envoy version: hash/1.39.0/Clean/RELEASE/BoringSSL", "")
	configured := fakeEnvoy(t, "envoy 1.38.3", "")
	t.Setenv(EnvoyPathEnv, environment)
	got, err := Discover(context.Background(), configured, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != environment || got.Source != "environment" || got.Version != "1.39.0" || got.Warning != "" {
		t.Fatalf("Discover() = %+v", got)
	}

	t.Setenv(EnvoyPathEnv, "")
	got, err = Discover(context.Background(), configured, time.Second)
	if err != nil || got.Path != configured || got.Source != "configuration" {
		t.Fatalf("configured Discover() = %+v, %v", got, err)
	}
}

func TestDiscoverPATHAndVersionEdges(t *testing.T) {
	path := fakeEnvoy(t, "envoy version: build/1.40.1/Clean", "")
	t.Setenv(EnvoyPathEnv, "")
	t.Setenv("PATH", filepath.Dir(path))
	got, err := Discover(context.Background(), "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "PATH" || !strings.Contains(got.Warning, "newer than tested") {
		t.Fatalf("Discover() = %+v", got)
	}

	old := fakeEnvoy(t, "envoy version: build/1.36.9/Clean", "")
	if _, err := Discover(context.Background(), old, time.Second); err == nil || !strings.Contains(err.Error(), "below supported") {
		t.Fatalf("old version error = %v", err)
	}
}

func TestDiscoverFailures(t *testing.T) {
	t.Setenv(EnvoyPathEnv, filepath.Join(t.TempDir(), "missing"))
	if _, err := Discover(context.Background(), "", time.Second); !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("missing error = %v", err)
	}

	t.Setenv(EnvoyPathEnv, fakeEnvoy(t, "not a version", ""))
	if _, err := Discover(context.Background(), "", time.Second); err == nil || !strings.Contains(err.Error(), "could not parse") {
		t.Fatalf("parse error = %v", err)
	}

	t.Setenv(EnvoyPathEnv, fakeEnvoy(t, "envoy 1.39.0", "sleep 2"))
	if _, err := Discover(context.Background(), "", 20*time.Millisecond); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
}
