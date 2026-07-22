package proc

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOSRunnerArgumentsAndStderr(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	binary := filepath.Join(dir, "envoy")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsPath + "\necho lifecycle-error >&2\nexit 7\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	process, err := (OSRunner{Stderr: &stderr}).Start(StartSpec{
		Binary: binary, ConfigPath: "/tmp/envoy.yaml", BaseID: 8, Epoch: 3,
		DrainTime: 5 * time.Minute, ParentShutdownTime: 8 * time.Minute, SkipNoParent: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	exit := <-process.Done()
	if exit.Code != 7 || !strings.Contains(process.StderrTail(), "lifecycle-error") {
		t.Fatalf("exit = %+v, stderr = %q", exit, process.StderrTail())
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--base-id\n8", "--restart-epoch\n3", "--drain-time-s\n300", "--parent-shutdown-time-s\n480", "--skip-hot-restart-on-no-parent"} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
}
