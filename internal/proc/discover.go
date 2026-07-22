package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/version"
)

const (
	// EnvoyPathEnv is the highest-priority managed Envoy path.
	EnvoyPathEnv = "ESGW_ENVOY_PATH"
	// DefaultVersionTimeout bounds startup discovery.
	DefaultVersionTimeout = 5 * time.Second
)

var (
	// ErrBinaryNotFound indicates that the configured discovery chain found no executable.
	ErrBinaryNotFound = errors.New("proc: envoy binary not found")
	versionPattern    = regexp.MustCompile(`\b[vV]?(\d+)\.(\d+)\.(\d+)\b`)
)

// Binary is a discovered and compatibility-checked Envoy executable.
type Binary struct {
	Path    string
	Source  string
	Version string
	Major   int
	Minor   int
	Patch   int
	Warning string
}

// Discover resolves Envoy in environment → proc.envoyPath → PATH order and
// checks its reported version against the supported minor window.
func Discover(ctx context.Context, configuredPath string, timeout time.Duration) (Binary, error) {
	path, source, err := findBinary(configuredPath)
	if err != nil {
		return Binary{}, err
	}
	if timeout <= 0 {
		timeout = DefaultVersionTimeout
	}
	versionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(versionCtx, path, "--version")
	cmd.WaitDelay = 100 * time.Millisecond
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		if errors.Is(versionCtx.Err(), context.DeadlineExceeded) {
			return Binary{}, fmt.Errorf("proc: %s --version timed out after %s", path, timeout)
		}
		return Binary{}, fmt.Errorf("proc: %s --version: %w: %s", path, err, tail(output.String(), 4096))
	}
	major, minor, patch, text, err := parseVersion(output.String())
	if err != nil {
		return Binary{}, fmt.Errorf("proc: inspect %s: %w", path, err)
	}
	result := Binary{Path: path, Source: source, Version: text, Major: major, Minor: minor, Patch: patch}
	if major != 1 || minor < version.EnvoyMinMinor {
		return Binary{}, fmt.Errorf("proc: Envoy %s is below supported range 1.%d.x-1.%d.x", text, version.EnvoyMinMinor, version.EnvoyMaxMinor)
	}
	if minor > version.EnvoyMaxMinor {
		result.Warning = fmt.Sprintf("Envoy %s is newer than tested range 1.%d.x-1.%d.x", text, version.EnvoyMinMinor, version.EnvoyMaxMinor)
	}
	return result, nil
}

func findBinary(configuredPath string) (string, string, error) {
	if path := os.Getenv(EnvoyPathEnv); path != "" {
		if err := checkExecutable(path); err != nil {
			return "", "", fmt.Errorf("%w: $%s=%q: %v", ErrBinaryNotFound, EnvoyPathEnv, path, err)
		}
		return path, "environment", nil
	}
	if configuredPath != "" {
		if err := checkExecutable(configuredPath); err != nil {
			return "", "", fmt.Errorf("%w: proc.envoyPath=%q: %v", ErrBinaryNotFound, configuredPath, err)
		}
		return configuredPath, "configuration", nil
	}
	path, err := exec.LookPath("envoy")
	if err != nil {
		return "", "", fmt.Errorf("%w: envoy not in PATH (set $%s or proc.envoyPath)", ErrBinaryNotFound, EnvoyPathEnv)
	}
	return path, "PATH", nil
}

func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	if info.Mode()&0o111 == 0 {
		return errors.New("not executable")
	}
	return nil
}

func parseVersion(output string) (int, int, int, string, error) {
	for _, match := range versionPattern.FindAllStringSubmatch(output, -1) {
		major, majorErr := strconv.Atoi(match[1])
		minor, minorErr := strconv.Atoi(match[2])
		patch, patchErr := strconv.Atoi(match[3])
		if majorErr == nil && minorErr == nil && patchErr == nil && major == 1 {
			return major, minor, patch, strings.TrimPrefix(match[0], "v"), nil
		}
	}
	return 0, 0, 0, "", fmt.Errorf("could not parse semantic version from %q", tail(output, 512))
}

func tail(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return "..." + value[len(value)-limit:]
}
