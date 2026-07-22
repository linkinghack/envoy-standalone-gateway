package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthcheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/readyz" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	if code, _, stderr := runCLI(t, "healthcheck", "--url", server.URL+"/readyz"); code != 0 {
		t.Fatalf("ready exit = %d, stderr: %s", code, stderr)
	}
	code, _, stderr := runCLI(t, "healthcheck", "--url", server.URL+"/not-ready")
	if code != 1 || !strings.Contains(stderr, "503 Service Unavailable") {
		t.Fatalf("not ready exit = %d, stderr: %s", code, stderr)
	}
}

func TestHealthcheckRejectsBadInvocation(t *testing.T) {
	for _, args := range [][]string{
		{"healthcheck", "--url", "unix:///run/esgw.sock"},
		{"healthcheck", "--timeout", "0s"},
		{"healthcheck", "extra"},
	} {
		if code, _, _ := runCLI(t, args...); code != 2 {
			t.Fatalf("run(%q) = %d, want 2", args, code)
		}
	}
}

func TestHealthcheckTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	t.Cleanup(server.Close)
	if code, _, stderr := runCLI(t, "healthcheck", "--url", server.URL, "--timeout", "10ms"); code != 1 {
		t.Fatalf("timeout exit = %d, stderr: %s", code, stderr)
	}
}
