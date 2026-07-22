package proc

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// StartSpec contains the complete, auditable Envoy process command.
type StartSpec struct {
	Binary             string
	ConfigPath         string
	BaseID             uint32
	Epoch              int
	DrainTime          time.Duration
	ParentShutdownTime time.Duration
	SkipNoParent       bool
}

// Exit is the normalized process termination result.
type Exit struct {
	Code   int
	Signal string
	Err    error
	At     time.Time
}

// Process is a started Envoy generation.
type Process interface {
	PID() int
	Done() <-chan Exit
	Kill() error
	StderrTail() string
}

// Runner starts Envoy generations. It is injectable for lifecycle tests.
type Runner interface {
	Start(StartSpec) (Process, error)
}

// OSRunner is the production os/exec implementation. Children are not tied to
// the management context so esgw shutdown does not kill the data plane.
type OSRunner struct{ Stderr io.Writer }

// Start launches one Envoy generation and begins asynchronous reaping.
func (r OSRunner) Start(spec StartSpec) (Process, error) {
	args := []string{
		"-c", spec.ConfigPath,
		"--base-id", strconv.FormatUint(uint64(spec.BaseID), 10),
		"--restart-epoch", strconv.Itoa(spec.Epoch),
		"--drain-time-s", strconv.FormatInt(int64(spec.DrainTime/time.Second), 10),
		"--parent-shutdown-time-s", strconv.FormatInt(int64(spec.ParentShutdownTime/time.Second), 10),
	}
	if spec.SkipNoParent {
		args = append(args, "--skip-hot-restart-on-no-parent")
	}
	cmd := exec.Command(spec.Binary, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	tail := &tailBuffer{limit: 4096}
	stderr := r.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	cmd.Stdout = io.MultiWriter(stderr, tail)
	cmd.Stderr = io.MultiWriter(stderr, tail)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("proc: start Envoy epoch %d: %w", spec.Epoch, err)
	}
	process := &osProcess{cmd: cmd, tail: tail, done: make(chan Exit, 1)}
	go process.wait()
	return process, nil
}

type osProcess struct {
	cmd  *exec.Cmd
	tail *tailBuffer
	done chan Exit
}

func (p *osProcess) PID() int           { return p.cmd.Process.Pid }
func (p *osProcess) Done() <-chan Exit  { return p.done }
func (p *osProcess) Kill() error        { return p.cmd.Process.Kill() }
func (p *osProcess) StderrTail() string { return p.tail.String() }

func (p *osProcess) wait() {
	err := p.cmd.Wait()
	exit := Exit{Code: -1, Err: err, At: time.Now().UTC()}
	if state := p.cmd.ProcessState; state != nil {
		exit.Code = state.ExitCode()
		if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			exit.Signal = status.Signal().String()
		}
	}
	p.done <- exit
	close(p.done)
}

type tailBuffer struct {
	mu    sync.Mutex
	data  []byte
	limit int
}

func (b *tailBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	b.data = append(b.data, value...)
	if len(b.data) > b.limit {
		b.data = append([]byte(nil), b.data[len(b.data)-b.limit:]...)
	}
	b.mu.Unlock()
	return len(value), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(bytes.TrimSpace(b.data))
}
