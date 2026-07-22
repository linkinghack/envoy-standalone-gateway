package proc

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
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
type OSRunner struct {
	Stderr       io.Writer
	LauncherPath string
}

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
	command, commandArgs := spec.Binary, args
	var pidRead, pidWrite *os.File
	if r.LauncherPath != "" {
		command = r.LauncherPath
		commandArgs = append([]string{"__proc-launcher", spec.Binary}, args...)
		var pipeErr error
		pidRead, pidWrite, pipeErr = os.Pipe()
		if pipeErr != nil {
			return nil, fmt.Errorf("proc: create launcher PID pipe: %w", pipeErr)
		}
	}
	cmd := exec.Command(command, commandArgs...)
	// A new session prevents service-manager or controlling-terminal hangups
	// aimed at the management process from reaching the Envoy data plane.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	tail := &tailBuffer{limit: 4096}
	stderr := r.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	cmd.Stdout = io.MultiWriter(stderr, tail)
	cmd.Stderr = io.MultiWriter(stderr, tail)
	if pidWrite != nil {
		cmd.ExtraFiles = []*os.File{pidWrite}
	}
	if err := cmd.Start(); err != nil {
		if pidRead != nil {
			_ = pidRead.Close()
			_ = pidWrite.Close()
		}
		return nil, fmt.Errorf("proc: start Envoy epoch %d: %w", spec.Epoch, err)
	}
	pid := cmd.Process.Pid
	target := cmd.Process
	if pidRead != nil {
		_ = pidWrite.Close()
		line, readErr := bufio.NewReader(pidRead).ReadString('\n')
		_ = pidRead.Close()
		if readErr != nil {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("proc: read launched Envoy PID: %w", readErr)
		}
		pid, readErr = strconv.Atoi(strings.TrimSpace(line))
		if readErr != nil || pid <= 0 {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("proc: invalid launched Envoy PID %q", strings.TrimSpace(line))
		}
		target, readErr = os.FindProcess(pid)
		if readErr != nil {
			_ = cmd.Process.Kill()
			return nil, fmt.Errorf("proc: find launched Envoy PID %d: %w", pid, readErr)
		}
	}
	process := &osProcess{cmd: cmd, process: target, pid: pid, tail: tail, done: make(chan Exit, 1)}
	go process.wait()
	return process, nil
}

type osProcess struct {
	cmd     *exec.Cmd
	process *os.Process
	pid     int
	tail    *tailBuffer
	done    chan Exit
}

func (p *osProcess) PID() int           { return p.pid }
func (p *osProcess) Done() <-chan Exit  { return p.done }
func (p *osProcess) Kill() error        { return p.process.Kill() }
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

// RunLauncher owns one Envoy child independently of the management process.
// The PID is returned on inherited fd 3 before the launcher waits and reaps it.
func RunLauncher(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "proc launcher: Envoy binary is required")
		return 2
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintf(stderr, "proc launcher: %v\n", err)
		return 1
	}
	pidFile := os.NewFile(3, "esgw-envoy-pid")
	if pidFile == nil {
		_ = cmd.Process.Kill()
		_, _ = fmt.Fprintln(stderr, "proc launcher: PID channel is unavailable")
		return 1
	}
	_, writeErr := fmt.Fprintf(pidFile, "%d\n", cmd.Process.Pid)
	closeErr := pidFile.Close()
	if writeErr != nil || closeErr != nil {
		_ = cmd.Process.Kill()
		_, _ = fmt.Fprintln(stderr, "proc launcher: cannot report Envoy PID")
		return 1
	}
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
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
