package proc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Probe is deliberately read-only; M-PROC never calls Envoy admin directly.
type Probe interface {
	ObserveProcess(context.Context) (ready bool, epoch int, err error)
}

// SupervisorConfig contains lifecycle policy after strict config validation.
type SupervisorConfig struct {
	Binary             Binary
	ConfigPath         string
	RecordPath         string
	BaseID             uint32
	LiveTimeout        time.Duration
	DrainTime          time.Duration
	ParentShutdownTime time.Duration
	AdoptPolicy        string
	Backoff            *Backoff
	PollInterval       time.Duration
}

// SupervisorStatus is safe for API/health reads.
type SupervisorStatus struct {
	State  string
	PID    int
	Epoch  int
	Detail string
}

// SupervisorEvent reports lifecycle facts without coupling to M-DELIVER.
type SupervisorEvent struct {
	Kind   string
	PID    int
	Epoch  int
	Detail string
	At     time.Time
}

// Supervisor owns only processes it can identify. Close detaches monitoring;
// it intentionally does not signal Envoy so the data plane outlives esgw.
type Supervisor struct {
	config SupervisorConfig
	runner Runner
	probe  Probe
	store  RecordStore
	log    *slog.Logger

	mu         sync.Mutex
	status     SupervisorStatus
	current    Process
	started    time.Time
	ctx        context.Context
	cancel     context.CancelFunc
	closed     bool
	restarting bool
	events     chan SupervisorEvent
	backoff    *Backoff
}

// NewSupervisor validates dependencies and constructs a detached lifecycle owner.
func NewSupervisor(config SupervisorConfig, runner Runner, probe Probe, log *slog.Logger) (*Supervisor, error) {
	if config.Binary.Path == "" || config.ConfigPath == "" || config.RecordPath == "" {
		return nil, errors.New("proc: supervisor requires binary, config path and record path")
	}
	if runner == nil || probe == nil {
		return nil, errors.New("proc: supervisor requires runner and M-STATE probe")
	}
	if config.LiveTimeout <= 0 {
		config.LiveTimeout = 30 * time.Second
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 100 * time.Millisecond
	}
	if config.AdoptPolicy == "" {
		config.AdoptPolicy = "keep"
	}
	if log == nil {
		log = slog.Default()
	}
	if config.Backoff == nil {
		config.Backoff = &Backoff{}
	}
	return &Supervisor{
		config: config, runner: runner, probe: probe, store: RecordStore{Path: config.RecordPath}, log: log,
		status: SupervisorStatus{State: "idle"}, events: make(chan SupervisorEvent, 32), backoff: config.Backoff,
	}, nil
}

// Start adopts a verified live generation or performs a fresh epoch-zero start.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.ctx != nil {
		s.mu.Unlock()
		return errors.New("proc: supervisor already started")
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.mu.Unlock()

	record, err := s.store.Load()
	switch {
	case err == nil:
		alive, sameBinary, identityErr := processIdentity(record.PID, record.BinaryPath)
		if identityErr != nil {
			return s.degrade(fmt.Sprintf("cannot verify recorded PID %d: %v", record.PID, identityErr))
		}
		if alive && sameBinary {
			if err := s.waitForEpoch(ctx, record.Epoch, nil); err == nil {
				s.setStatus(SupervisorStatus{State: "running", PID: record.PID, Epoch: record.Epoch, Detail: "adopted existing Envoy"})
				record.State = "running"
				_ = s.store.Save(record)
				s.emit("Adopted", record.PID, record.Epoch, "management restart preserved data plane")
				return nil
			} else if s.config.AdoptPolicy == "keep" {
				return s.degrade(fmt.Sprintf("recorded Envoy PID %d is alive but epoch readiness is unconfirmed: %v", record.PID, err))
			}
			if err := stopVerifiedPID(ctx, record.PID, 5*time.Second); err != nil {
				return s.degrade(fmt.Sprintf("explicit restart could not stop verified PID %d: %v", record.PID, err))
			}
		}
		// A reused PID is never signaled. Starting epoch zero may fail on the
		// base-id lock, which is safer than touching an unrelated process.
		return s.spawnAndWait(ctx, 0, false)
	case errors.Is(err, ErrNoRecord):
		return s.spawnAndWait(ctx, 0, false)
	default:
		return fmt.Errorf("proc: load adoption record: %w", err)
	}
}

func (s *Supervisor) spawnAndWait(ctx context.Context, epoch int, skipNoParent bool) error {
	process, err := s.runner.Start(StartSpec{
		Binary: s.config.Binary.Path, ConfigPath: s.config.ConfigPath, BaseID: s.config.BaseID, Epoch: epoch,
		DrainTime: s.config.DrainTime, ParentShutdownTime: s.config.ParentShutdownTime, SkipNoParent: skipNoParent,
	})
	if err != nil {
		return err
	}
	started := time.Now().UTC()
	record := Record{
		PID: process.PID(), BaseID: s.config.BaseID, Epoch: epoch, ConfigPath: s.config.ConfigPath,
		NextEpoch: epoch + 1, BinaryPath: s.config.Binary.Path, StartedAt: started, EnvoyVersion: s.config.Binary.Version, State: "starting",
	}
	if err := s.store.Save(record); err != nil {
		_ = process.Kill()
		return err
	}
	s.setCurrent(process, epoch, started, "starting", "waiting for Envoy LIVE")
	if err := s.waitForEpoch(ctx, epoch, process.Done()); err != nil {
		_ = process.Kill()
		return fmt.Errorf("proc: Envoy epoch %d failed readiness: %w; stderr: %s", epoch, err, process.StderrTail())
	}
	record.State = "running"
	if err := s.store.Save(record); err != nil {
		_ = process.Kill()
		return err
	}
	s.setCurrent(process, epoch, started, "running", "")
	s.emit("Started", process.PID(), epoch, "Envoy is LIVE")
	go s.monitor(process, epoch, started)
	return nil
}

func (s *Supervisor) waitForEpoch(ctx context.Context, epoch int, done <-chan Exit) error {
	timeout := time.NewTimer(s.config.LiveTimeout)
	defer timeout.Stop()
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()
	var lastObservation string
	for {
		ready, observedEpoch, err := s.probe.ObserveProcess(ctx)
		if err == nil && ready && observedEpoch == epoch {
			return nil
		}
		if err != nil {
			lastObservation = err.Error()
		} else {
			lastObservation = fmt.Sprintf("ready=%t epoch=%d", ready, observedEpoch)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case exit, ok := <-done:
			if ok {
				return fmt.Errorf("exited with code %d signal %s: %v", exit.Code, exit.Signal, exit.Err)
			}
			return errors.New("process exited before readiness")
		case <-timeout.C:
			return fmt.Errorf("LIVE epoch %d not observed within %s (last: %s)", epoch, s.config.LiveTimeout, lastObservation)
		case <-ticker.C:
		}
	}
}

func (s *Supervisor) monitor(process Process, epoch int, started time.Time) {
	exit, ok := <-process.Done()
	if !ok {
		return
	}
	s.mu.Lock()
	if s.closed || s.current != process {
		s.mu.Unlock()
		return
	}
	s.current = nil
	if s.restarting {
		s.mu.Unlock()
		s.emit("ParentExited", process.PID(), epoch, "current parent exited during hot restart")
		return
	}
	s.mu.Unlock()
	delay, degraded := s.backoff.Failure(exit.At, exit.At.Sub(started))
	detail := fmt.Sprintf("unexpected Envoy exit code=%d signal=%s err=%v stderr=%s", exit.Code, exit.Signal, exit.Err, process.StderrTail())
	s.log.Error("managed Envoy exited", "pid", process.PID(), "epoch", epoch, "detail", detail)
	s.emit("Exited", process.PID(), epoch, detail)
	if degraded || epoch != 0 {
		_ = s.degrade(detail)
		return
	}
	s.setStatus(SupervisorStatus{State: "backoff", PID: process.PID(), Epoch: epoch, Detail: fmt.Sprintf("restart in %s: %s", delay, detail)})
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
		return
	case <-timer.C:
	}
	if err := s.spawnAndWait(s.ctx, 0, false); err != nil {
		_ = s.degrade(fmt.Sprintf("automatic restart failed: %v", err))
	}
}

// HotRestart allocates a never-reused epoch and waits for M-STATE to observe
// the child LIVE. Failure kills only the child and retains the parent record.
func (s *Supervisor) HotRestart(ctx context.Context) error {
	s.mu.Lock()
	if s.closed || s.ctx == nil {
		s.mu.Unlock()
		return errors.New("proc: supervisor is not running")
	}
	if s.restarting {
		s.mu.Unlock()
		return errors.New("proc: hot restart already in progress")
	}
	if s.status.State != "running" {
		state := s.status.State
		s.mu.Unlock()
		return fmt.Errorf("proc: cannot hot restart while supervisor is %s", state)
	}
	s.restarting = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.restarting = false
		s.mu.Unlock()
	}()

	record, err := s.store.Load()
	if err != nil {
		return fmt.Errorf("proc: load epoch record: %w", err)
	}
	epoch := record.NextEpoch
	record.NextEpoch++
	record.State = "restarting"
	if err := s.store.Save(record); err != nil {
		return err
	}
	child, err := s.runner.Start(StartSpec{
		Binary: s.config.Binary.Path, ConfigPath: s.config.ConfigPath, BaseID: s.config.BaseID, Epoch: epoch,
		DrainTime: s.config.DrainTime, ParentShutdownTime: s.config.ParentShutdownTime,
	})
	if err != nil {
		return fmt.Errorf("proc: hot restart epoch %d: %w", epoch, err)
	}
	started := time.Now().UTC()
	if err := s.waitForEpoch(ctx, epoch, child.Done()); err != nil {
		_ = child.Kill()
		record.State = "running"
		_ = s.store.Save(record)
		detail := fmt.Sprintf("epoch %d failed: %v; stderr: %s", epoch, err, child.StderrTail())
		s.emit("HotRestartFailed", child.PID(), epoch, detail)
		return fmt.Errorf("proc: hot restart: %s", detail)
	}
	record.PID = child.PID()
	record.Epoch = epoch
	record.StartedAt = started
	record.State = "running"
	if err := s.store.Save(record); err != nil {
		_ = child.Kill()
		return err
	}
	s.setCurrent(child, epoch, started, "running", "")
	s.emit("HotRestarted", child.PID(), epoch, "new Envoy epoch is LIVE")
	go s.monitor(child, epoch, started)
	return nil
}

// Status returns a consistent lifecycle snapshot.
func (s *Supervisor) Status() SupervisorStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Events returns the bounded lifecycle event channel.
func (s *Supervisor) Events() <-chan SupervisorEvent { return s.events }

// Close detaches monitoring without signaling the managed Envoy process.
func (s *Supervisor) Close() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		if s.cancel != nil {
			s.cancel()
		}
	}
	s.mu.Unlock()
}

func (s *Supervisor) degrade(detail string) error {
	s.setStatus(SupervisorStatus{State: "degraded", Detail: detail})
	s.emit("Degraded", 0, 0, detail)
	s.log.Warn("Envoy supervisor degraded", "detail", detail)
	return nil
}

func (s *Supervisor) setCurrent(process Process, epoch int, started time.Time, state, detail string) {
	s.mu.Lock()
	s.current, s.started = process, started
	s.status = SupervisorStatus{State: state, PID: process.PID(), Epoch: epoch, Detail: detail}
	s.mu.Unlock()
}

func (s *Supervisor) setStatus(status SupervisorStatus) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()
}

func (s *Supervisor) emit(kind string, pid, epoch int, detail string) {
	event := SupervisorEvent{Kind: kind, PID: pid, Epoch: epoch, Detail: detail, At: time.Now().UTC()}
	select {
	case s.events <- event:
	default:
		s.log.Warn("dropping Envoy supervisor event", "kind", kind)
	}
}

func processIdentity(pid int, binaryPath string) (alive bool, sameBinary bool, err error) {
	if pid <= 0 {
		return false, false, nil
	}
	if signalErr := syscall.Kill(pid, 0); signalErr != nil {
		if errors.Is(signalErr, syscall.ESRCH) {
			return false, false, nil
		}
		if !errors.Is(signalErr, syscall.EPERM) {
			return false, false, signalErr
		}
	}
	actual, err := filepath.EvalSymlinks(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return true, false, err
	}
	expected, err := filepath.EvalSymlinks(binaryPath)
	if err != nil {
		return true, false, err
	}
	actualInfo, err := os.Stat(actual)
	if err != nil {
		return true, false, err
	}
	expectedInfo, err := os.Stat(expected)
	if err != nil {
		return true, false, err
	}
	return true, os.SameFile(actualInfo, expectedInfo), nil
}

func stopVerifiedPID(ctx context.Context, pid int, timeout time.Duration) error {
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		alive, _, err := processIdentity(pid, fmt.Sprintf("/proc/%d/exe", pid))
		if err == nil && !alive {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return syscall.Kill(pid, syscall.SIGKILL)
		case <-ticker.C:
		}
	}
}
