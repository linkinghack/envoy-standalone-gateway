package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/api"
	"github.com/linkinghack/envoy-standalone-gateway/internal/auth"
	"github.com/linkinghack/envoy-standalone-gateway/internal/certstore"
	"github.com/linkinghack/envoy-standalone-gateway/internal/compile"
	"github.com/linkinghack/envoy-standalone-gateway/internal/conf"
	"github.com/linkinghack/envoy-standalone-gateway/internal/config"
	"github.com/linkinghack/envoy-standalone-gateway/internal/console"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver/xds"
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/state"
	"github.com/linkinghack/envoy-standalone-gateway/internal/store"
)

// App is the only production composition root. It owns module construction,
// listener lifecycle and reverse-order shutdown.
type App struct {
	config    *config.Config
	log       *slog.Logger
	store     *store.Store
	deliver   *xds.Server
	state     *state.Service
	publisher *conf.Publisher
	api       *api.Server

	ready       atomic.Bool
	closeOnce   sync.Once
	closeErr    error
	detachState func()
}

// NewApp constructs every M1 management-plane module without opening network
// listeners. The filesystem draft under dataDir/config.d is the only source.
func NewApp(cfg *config.Config, assets fs.FS, log *slog.Logger) (*App, error) {
	if cfg == nil {
		return nil, errors.New("core: nil config")
	}
	if cfg.Deliver.Mode == config.ModeStatic {
		return nil, fmt.Errorf("core: deliver.mode=static: static 运行时下发未实现（S7）；纯导出路径请用 esgw compile --mode static")
	}
	if log == nil {
		log = slog.Default()
	}
	if assets == nil {
		assets = console.Assets()
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("core: create data directory: %w", err)
	}
	durable, err := store.Open(filepath.Join(cfg.DataDir, "esgw.db"))
	if err != nil {
		return nil, fmt.Errorf("core: open store: %w", err)
	}
	fail := func(cause error) (*App, error) {
		_ = durable.Close()
		return nil, cause
	}

	output, err := loadInitialIR(cfg.DataDir)
	if err != nil {
		return fail(err)
	}
	deliverer := xds.NewServer(cfg.Deliver.XDS.NodeID, log)
	if err := deliverer.Apply(context.Background(), output); err != nil {
		return fail(fmt.Errorf("core: apply initial snapshot: %w", err))
	}

	authService, err := auth.New(durable, auth.Config{StartedAt: time.Now()})
	if err != nil {
		return fail(fmt.Errorf("core: initialize auth: %w", err))
	}
	if password := os.Getenv("ESGW_INITIAL_ADMIN_PASSWORD"); password != "" {
		bootstrap, stateErr := authService.BootstrapState(context.Background())
		if stateErr != nil {
			return fail(fmt.Errorf("core: inspect initial admin: %w", stateErr))
		}
		if bootstrap.Required {
			if _, err := authService.Bootstrap(context.Background(), "admin", password, "startup", "esgw"); err != nil {
				return fail(fmt.Errorf("core: initialize admin account: %w", err))
			}
		}
	}

	stateService := state.New(cfg.Deliver.XDS.NodeID, &state.HTTPClient{Address: cfg.Deliver.XDS.AdminAddress})
	publisher := &conf.Publisher{DataDir: cfg.DataDir, Store: durable, Deliver: deliverer, Mode: compile.ModeXDS}
	detachState := publisher.AttachState(stateService)
	certificateService := &certstore.Service{DataDir: cfg.DataDir, Store: durable}

	configAPI := &api.ConfigAPI{DataDir: cfg.DataDir, Store: durable, Publisher: publisher, Mode: compile.ModeXDS}
	stateAPI := &api.StateAPI{
		State: stateService, Prometheus: stateService.PrometheusProxy(),
		Mode: cfg.Deliver.Mode, Topology: cfg.API.Topology,
	}
	certificateAPI := &api.CertificateAPI{Certificates: certificateService}
	handlers, err := api.MergeHandlers(configAPI.Handlers(), stateAPI.Handlers(), certificateAPI.Handlers())
	if err != nil {
		detachState()
		return fail(fmt.Errorf("core: compose API handlers: %w", err))
	}
	app := &App{
		config: cfg, log: log, store: durable, deliver: deliverer, state: stateService,
		publisher: publisher, detachState: detachState,
	}
	apiServer, err := api.NewServer(api.Config{
		Auth: authService, Handlers: handlers, Assets: assets,
		Ready: app.ready.Load, Logger: log,
	})
	if err != nil {
		detachState()
		return fail(fmt.Errorf("core: construct management API: %w", err))
	}
	if missing := apiServer.UnimplementedOperations(); len(missing) != 0 {
		detachState()
		return fail(fmt.Errorf("core: unimplemented API operations: %v", missing))
	}
	app.api = apiServer
	return app, nil
}

// Handler exposes the fully composed management handler for in-process tests.
func (a *App) Handler() http.Handler { return a.api }

// Run opens xDS and management listeners only after the initial snapshot and
// all management modules are ready. It blocks until cancellation or failure.
func (a *App) Run(ctx context.Context) error {
	if a == nil || a.api == nil {
		return errors.New("core: app is not initialized")
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() { _ = a.Close() }()

	xdsListener, err := net.Listen("tcp", a.config.Deliver.XDS.Listen)
	if err != nil {
		return fmt.Errorf("core: listen xDS %s: %w", a.config.Deliver.XDS.Listen, err)
	}
	apiListener, err := net.Listen("tcp", a.config.API.Listen)
	if err != nil {
		_ = xdsListener.Close()
		return fmt.Errorf("core: listen API %s: %w", a.config.API.Listen, err)
	}

	if !isLoopbackListener(apiListener.Addr()) {
		a.log.Warn("management API is listening on a non-loopback address", "listen", apiListener.Addr().String())
	}
	stopState := a.state.Start(runCtx, state.PollConfig{
		ReadyInterval: a.config.State.ReadyInterval.Duration, StatsInterval: a.config.State.StatsInterval.Duration,
		ClustersInterval: a.config.State.ClustersInterval.Duration, ConfigInterval: a.config.State.ConfigInterval.Duration,
		CertsInterval: a.config.State.CertsInterval.Duration,
	})
	defer stopState()
	httpServer := &http.Server{
		Handler: a.api, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second,
	}
	type serveResult struct {
		name string
		err  error
	}
	results := make(chan serveResult, 2)
	go func() { results <- serveResult{name: "xDS", err: a.deliver.Serve(runCtx, xdsListener)} }()
	go func() {
		err := httpServer.Serve(apiListener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		results <- serveResult{name: "API", err: err}
	}()
	a.ready.Store(true)
	a.log.Info("esgw app listening", "xds", xdsListener.Addr().String(), "api", apiListener.Addr().String())

	var firstErr error
	received := 0
	select {
	case <-ctx.Done():
	case result := <-results:
		received = 1
		if result.err != nil {
			firstErr = fmt.Errorf("core: %s server: %w", result.name, result.err)
		}
	}
	a.ready.Store(false)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = httpServer.Shutdown(shutdownCtx)
	shutdownCancel()
	cancel()
	_ = xdsListener.Close()

	// Both serve goroutines must exit before Store and subscriptions are closed.
	for ; received < 2; received++ {
		select {
		case result := <-results:
			if firstErr == nil && result.err != nil && !errors.Is(result.err, net.ErrClosed) {
				firstErr = fmt.Errorf("core: %s server: %w", result.name, result.err)
			}
		case <-time.After(10 * time.Second):
			if firstErr == nil {
				firstErr = errors.New("core: timed out stopping servers")
			}
			return firstErr
		}
	}
	return firstErr
}

// Close releases subscriptions and the durable Store. It is idempotent.
func (a *App) Close() error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		a.ready.Store(false)
		if a.detachState != nil {
			a.detachState()
		}
		if a.store != nil {
			a.closeErr = a.store.Close()
		}
	})
	return a.closeErr
}

func loadInitialIR(dataDir string) (*ir.IR, error) {
	draft, loadErrs, err := conf.LoadDraft(dataDir)
	if err != nil {
		return nil, fmt.Errorf("core: load draft: %w", err)
	}
	if len(loadErrs) != 0 {
		return nil, fmt.Errorf("core: load draft: %d error(s): %s", len(loadErrs), loadErrs[0].Error())
	}
	if draft.Mode == conf.ModeNative {
		output, err := conf.LoadNative(filepath.Join(dataDir, "native.yaml"))
		if err != nil {
			return nil, fmt.Errorf("core: load native draft: %w", err)
		}
		return output, nil
	}
	output, compileErrs := compile.Compile(draft.Config, compile.Options{
		Mode: compile.ModeXDS, ManagedCertificateDir: filepath.Join(dataDir, "certs"),
	})
	for _, compileErr := range compileErrs {
		if compileErr.Severity == compile.SeverityError {
			return nil, fmt.Errorf("core: compile draft: %s", compileErr.Error())
		}
	}
	if output == nil {
		return nil, errors.New("core: compile draft returned no IR")
	}
	return output, nil
}

func isLoopbackListener(address net.Addr) bool {
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
