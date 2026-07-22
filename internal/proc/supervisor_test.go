package proc

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeProbe struct {
	mu  sync.Mutex
	obs Observation
	err error
}

func (p *fakeProbe) ObserveProcess(context.Context) (Observation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.obs, p.err
}

type fakeProcess struct {
	pid  int
	done chan Exit
}

func (p *fakeProcess) PID() int           { return p.pid }
func (p *fakeProcess) Done() <-chan Exit  { return p.done }
func (p *fakeProcess) Kill() error        { return nil }
func (p *fakeProcess) StderrTail() string { return "fake stderr" }

type fakeRunner struct {
	mu        sync.Mutex
	specs     []StartSpec
	processes []*fakeProcess
}

func (r *fakeRunner) Start(spec StartSpec) (Process, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	process := &fakeProcess{pid: 1000 + len(r.specs), done: make(chan Exit, 1)}
	r.specs = append(r.specs, spec)
	r.processes = append(r.processes, process)
	return process, nil
}

func supervisorFixture(t *testing.T, runner *fakeRunner, probe *fakeProbe) *Supervisor {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	supervisor, err := NewSupervisor(SupervisorConfig{
		Binary: Binary{Path: binary, Version: "1.39.0"}, ConfigPath: "/tmp/envoy.yaml",
		RecordPath: filepath.Join(t.TempDir(), "run", "proc.json"), LiveTimeout: 200 * time.Millisecond,
		DrainTime: time.Minute, ParentShutdownTime: 2 * time.Minute, PollInterval: time.Millisecond,
		Backoff: &Backoff{Initial: time.Millisecond, Max: time.Millisecond, ResetAfter: time.Minute, GiveUp: 2},
	}, runner, probe, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(supervisor.Close)
	return supervisor
}

func TestSupervisorFreshStartAndBoundedRestart(t *testing.T) {
	runner := &fakeRunner{}
	probe := &fakeProbe{obs: Observation{Ready: true, Epoch: 0}}
	supervisor := supervisorFixture(t, runner, probe)
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := supervisor.Status(); status.State != "running" || status.Epoch != 0 {
		t.Fatalf("status = %+v", status)
	}
	runner.mu.Lock()
	first := runner.processes[0]
	runner.mu.Unlock()
	first.done <- Exit{Code: 1, Err: context.Canceled, At: time.Now()}
	close(first.done)
	waitFor(t, func() bool {
		runner.mu.Lock()
		defer runner.mu.Unlock()
		return len(runner.processes) == 2
	})
	runner.mu.Lock()
	second := runner.processes[1]
	runner.mu.Unlock()
	second.done <- Exit{Code: 1, Err: context.Canceled, At: time.Now()}
	close(second.done)
	waitFor(t, func() bool { return supervisor.Status().State == "degraded" })
}

func TestSupervisorAdoptsVerifiedProcessWithoutSpawn(t *testing.T) {
	runner := &fakeRunner{}
	probe := &fakeProbe{obs: Observation{Ready: true, Epoch: 4}}
	supervisor := supervisorFixture(t, runner, probe)
	binary, _ := os.Executable()
	if err := supervisor.store.Save(Record{
		PID: os.Getpid(), BaseID: 0, Epoch: 4, ConfigPath: "/tmp/envoy.yaml", BinaryPath: binary,
		StartedAt: time.Now(), EnvoyVersion: "1.39.0", State: "running",
	}); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := supervisor.Status(); status.State != "running" || status.PID != os.Getpid() || status.Epoch != 4 {
		t.Fatalf("status = %+v", status)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.specs) != 0 {
		t.Fatalf("adoption spawned %d process(es)", len(runner.specs))
	}
}

func TestSupervisorKeepDegradesOnUnconfirmedAdoption(t *testing.T) {
	runner := &fakeRunner{}
	probe := &fakeProbe{obs: Observation{Ready: false, Epoch: 3}}
	supervisor := supervisorFixture(t, runner, probe)
	supervisor.config.LiveTimeout = 5 * time.Millisecond
	binary, _ := os.Executable()
	if err := supervisor.store.Save(Record{
		PID: os.Getpid(), Epoch: 3, ConfigPath: "/tmp/envoy.yaml", BinaryPath: binary,
		StartedAt: time.Now(), EnvoyVersion: "1.39.0", State: "running",
	}); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := supervisor.Status(); status.State != "degraded" {
		t.Fatalf("status = %+v", status)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met")
}
