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
	"strings"
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
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver"
	staticdeliver "github.com/linkinghack/envoy-standalone-gateway/internal/deliver/static"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver/xds"
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/proc"
	"github.com/linkinghack/envoy-standalone-gateway/internal/state"
	"github.com/linkinghack/envoy-standalone-gateway/internal/store"
)

// App is the only production composition root. It owns module construction,
// listener lifecycle and reverse-order shutdown.
type App struct {
	config     *config.Config
	log        *slog.Logger
	store      *store.Store
	deliver    deliver.Deliverer
	xds        *xds.Server
	static     *staticdeliver.Server
	supervisor *proc.Supervisor
	state      *state.Service
	publisher  *conf.Publisher
	api        *api.Server

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

	mode := compile.Mode(cfg.Deliver.Mode)
	output, err := loadInitialIR(cfg.DataDir, mode)
	if err != nil {
		return fail(err)
	}
	stateService := state.New(cfg.Deliver.XDS.NodeID, &state.HTTPClient{Address: cfg.Deliver.XDS.AdminAddress})
	var deliverer deliver.Deliverer
	var xdsServer *xds.Server
	var staticServer *staticdeliver.Server
	if cfg.Deliver.Mode == config.ModeStatic {
		staticServer = staticdeliver.NewServer(
			staticdeliver.Writer{OutputPath: cfg.Deliver.Static.OutputPath},
			staticdeliver.RenderOptions{AdminSocketPath: staticAdminSocket(cfg.Deliver.XDS.AdminAddress)}, nil, log,
		)
		deliverer = staticServer
	} else {
		xdsServer = xds.NewServer(cfg.Deliver.XDS.NodeID, log)
		deliverer = xdsServer
	}
	if err := deliverer.Apply(context.Background(), output); err != nil {
		return fail(fmt.Errorf("core: apply initial delivery: %w", err))
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

	publisher := &conf.Publisher{DataDir: cfg.DataDir, Store: durable, Deliver: deliverer, Mode: mode}
	if cfg.Deliver.Mode == config.ModeStatic && !cfg.Proc.Enabled {
		publisher.ConfirmTimeout = 10 * time.Minute
	}
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
		config: cfg, log: log, store: durable, deliver: deliverer, xds: xdsServer, static: staticServer, state: stateService,
		publisher: publisher, detachState: detachState,
	}
	if cfg.Proc.Enabled {
		supervisor, supervisorErr := buildSupervisor(cfg, stateService, log)
		if supervisorErr != nil {
			detachState()
			return fail(supervisorErr)
		}
		app.supervisor = supervisor
		if staticServer != nil {
			staticServer.SetRestarter(supervisor)
		}
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

	var xdsListener net.Listener
	var err error
	if a.xds != nil {
		xdsListener, err = net.Listen("tcp", a.config.Deliver.XDS.Listen)
		if err != nil {
			return fmt.Errorf("core: listen xDS %s: %w", a.config.Deliver.XDS.Listen, err)
		}
	}
	apiListener, err := net.Listen("tcp", a.config.API.Listen)
	if err != nil {
		if xdsListener != nil {
			_ = xdsListener.Close()
		}
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
	serverCount := 1
	results := make(chan serveResult, 2)
	if a.xds != nil {
		serverCount++
		go func() { results <- serveResult{name: "xDS", err: a.xds.Serve(runCtx, xdsListener)} }()
	}
	go func() {
		err := httpServer.Serve(apiListener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		results <- serveResult{name: "API", err: err}
	}()

	var firstErr error
	received := 0
	if a.supervisor != nil {
		if err := a.supervisor.Start(runCtx); err != nil {
			firstErr = fmt.Errorf("core: start managed Envoy: %w", err)
		}
	}
	if firstErr == nil && (a.supervisor == nil || a.supervisor.Status().State == "running") {
		a.ready.Store(true)
	}
	if xdsListener != nil {
		a.log.Info("esgw app listening", "mode", a.config.Deliver.Mode, "xds", xdsListener.Addr().String(), "api", apiListener.Addr().String())
	} else {
		a.log.Info("esgw app listening", "mode", a.config.Deliver.Mode, "api", apiListener.Addr().String())
	}
	if firstErr == nil {
		select {
		case <-ctx.Done():
		case result := <-results:
			received = 1
			if result.err != nil {
				firstErr = fmt.Errorf("core: %s server: %w", result.name, result.err)
			}
		}
	}
	a.ready.Store(false)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = httpServer.Shutdown(shutdownCtx)
	shutdownCancel()
	cancel()
	if xdsListener != nil {
		_ = xdsListener.Close()
	}

	// All serve goroutines must exit before Store and subscriptions are closed.
	for ; received < serverCount; received++ {
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
		if a.supervisor != nil {
			a.supervisor.Close()
		}
		if a.detachState != nil {
			a.detachState()
		}
		if a.store != nil {
			a.closeErr = a.store.Close()
		}
	})
	return a.closeErr
}

func loadInitialIR(dataDir string, mode compile.Mode) (*ir.IR, error) {
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
		Mode: mode, ManagedCertificateDir: filepath.Join(dataDir, "certs"),
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

func buildSupervisor(cfg *config.Config, probe proc.Probe, log *slog.Logger) (*proc.Supervisor, error) {
	binary, err := proc.Discover(context.Background(), cfg.Proc.EnvoyPath, proc.DefaultVersionTimeout)
	if err != nil {
		return nil, fmt.Errorf("core: discover managed Envoy: %w", err)
	}
	if binary.Warning != "" {
		log.Warn("managed Envoy is outside the tested compatibility window", "warning", binary.Warning)
	}
	launcherPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("core: resolve process launcher: %w", err)
	}
	configPath := cfg.Deliver.Static.OutputPath
	if cfg.Deliver.Mode == config.ModeXDS {
		configPath = filepath.Join(cfg.DataDir, "envoy", "bootstrap.yaml")
		payload, renderErr := xds.RenderBootstrap(xds.BootstrapOpts{
			NodeID: cfg.Deliver.XDS.NodeID, NodeCluster: cfg.Deliver.XDS.NodeCluster,
			XDSListen: cfg.Deliver.XDS.Listen, AdminAddress: cfg.Deliver.XDS.AdminAddress,
		})
		if renderErr != nil {
			return nil, fmt.Errorf("core: render managed xDS bootstrap: %w", renderErr)
		}
		if writeErr := (staticdeliver.Writer{OutputPath: configPath}).Write(payload); writeErr != nil {
			return nil, fmt.Errorf("core: write managed xDS bootstrap: %w", writeErr)
		}
	}
	backoff := cfg.Proc.RestartBackoff
	return proc.NewSupervisor(proc.SupervisorConfig{
		Binary: binary, ConfigPath: configPath, RecordPath: filepath.Join(cfg.DataDir, "run", "proc.json"),
		BaseID: cfg.Proc.BaseID, LiveTimeout: cfg.Proc.LiveTimeout.Duration,
		DrainTime: cfg.Proc.DrainTime.Duration, ParentShutdownTime: cfg.Proc.ParentShutdownTime.Duration,
		AdoptPolicy: cfg.Proc.AdoptPolicy,
		Backoff: &proc.Backoff{
			Initial: backoff.Initial.Duration, Max: backoff.Max.Duration,
			ResetAfter: backoff.ResetAfter.Duration, GiveUp: backoff.GiveUpPer10m,
		},
	}, proc.OSRunner{LauncherPath: launcherPath}, probe, log)
}

func staticAdminSocket(address string) string {
	if path, ok := strings.CutPrefix(address, "unix://"); ok && path != "" {
		return path
	}
	return staticdeliver.DefaultAdminSocketPath
}

func isLoopbackListener(address net.Addr) bool {
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
