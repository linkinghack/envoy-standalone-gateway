package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// AdminClient is the read-only Envoy admin API contract.
type AdminClient interface {
	Get(ctx context.Context, path string) ([]byte, error)
	Prometheus(ctx context.Context, w io.Writer) error
}

var allowedPaths = map[string]bool{
	"/ready":                   true,
	"/server_info":             true,
	"/config_dump?include_eds": true,
	"/clusters?format=json":    true,
	"/stats?format=json":       true,
	"/stats/prometheus":        true,
	"/certs":                   true,
}

// HTTPClient connects to an Envoy admin UDS or TCP endpoint and permits only
// the read-only paths defined by the M-STATE design.
type HTTPClient struct {
	Address string
	Client  *http.Client
}

// Get performs a read-only admin GET request.
func (c *HTTPClient) Get(ctx context.Context, path string) ([]byte, error) {
	if !allowedPaths[path] {
		return nil, fmt.Errorf("admin path %q is not allowed", path)
	}
	req, err := c.newRequest(ctx, path)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("admin GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("admin GET %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// Prometheus streams Envoy's Prometheus exposition endpoint to w.
func (c *HTTPClient) Prometheus(ctx context.Context, w io.Writer) error {
	req, err := c.newRequest(ctx, "/stats/prometheus")
	if err != nil {
		return err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("admin prometheus: status %d", resp.StatusCode)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func (c *HTTPClient) newRequest(ctx context.Context, path string) (*http.Request, error) {
	if c == nil || c.Address == "" {
		return nil, errors.New("admin address is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://esgw-admin"+path, nil)
	if err != nil {
		return nil, err
	}
	return req, nil
}

func (c *HTTPClient) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	transport := &http.Transport{DialContext: c.dial}
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}

func (c *HTTPClient) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	if rest, ok := strings.CutPrefix(c.Address, "unix:///"); ok {
		var d net.Dialer
		return d.DialContext(ctx, "unix", "/"+rest)
	}
	host, port, err := net.SplitHostPort(c.Address)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
}

// DecodeJSON decodes an admin response with a useful size-independent error.
func DecodeJSON[T any](body []byte) (T, error) {
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("decode admin JSON: %w", err)
	}
	return out, nil
}
