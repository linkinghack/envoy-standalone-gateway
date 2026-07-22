package main

import (
	"context"
	"flag"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultHealthcheckURL     = "http://127.0.0.1:8080/readyz"
	defaultHealthcheckTimeout = 2 * time.Second
)

// runHealthcheck provides a shell-free readiness probe for containers and
// service managers. Exit 0 means HTTP 2xx, 1 means the probe failed, and 2 is
// reserved for invalid invocation.
func runHealthcheck(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	probeURL := flags.String("url", defaultHealthcheckURL, "HTTP readiness URL")
	timeout := flags.Duration("timeout", defaultHealthcheckTimeout, "probe timeout")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		eprintf(stderr, "healthcheck: unexpected arguments: %v\n", flags.Args())
		return 2
	}
	parsed, err := url.ParseRequestURI(*probeURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		eprintf(stderr, "healthcheck: --url must be an absolute HTTP(S) URL\n")
		return 2
	}
	if *timeout <= 0 {
		eprintf(stderr, "healthcheck: --timeout must be positive\n")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		eprintf(stderr, "healthcheck: build request: %v\n", err)
		return 1
	}
	client := &http.Client{
		Timeout: *timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(req)
	if err != nil {
		eprintf(stderr, "healthcheck: %v\n", err)
		return 1
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		eprintf(stderr, "healthcheck: %s returned %s\n", parsed.Redacted(), response.Status)
		return 1
	}
	return 0
}
