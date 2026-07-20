package conf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/compile"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver"
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/state"
	"github.com/linkinghack/envoy-standalone-gateway/internal/store"
)

// Publisher coordinates the minimum publish path across M-CONF, M-COMPILE,
// M-STORE and M-DELIVER.
type Publisher struct {
	DataDir     string
	Store       *store.Store
	Deliver     deliver.Deliverer
	Mode        compile.Mode
	mu          sync.Mutex
	state       *state.Service
	stateCancel func()
}

// AttachState wires M-STATE confirmation events into the publish state machine.
// The returned function stops the subscription goroutine.
func (p *Publisher) AttachState(service *state.Service) func() {
	if p == nil || service == nil {
		return func() {}
	}
	p.mu.Lock()
	if p.stateCancel != nil {
		cancel := p.stateCancel
		p.mu.Unlock()
		return cancel
	}
	events := make(chan state.VersionConfirmEvent, 8)
	cancelSub := service.SubscribeConfirm(events)
	ctx, cancel := context.WithCancel(context.Background())
	p.state = service
	p.stateCancel = func() {
		cancel()
		cancelSub()
	}
	p.mu.Unlock()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-events:
				_ = p.handleConfirmEvent(context.Background(), event)
			}
		}
	}()
	return p.stateCancel
}

func (p *Publisher) handleConfirmEvent(ctx context.Context, event state.VersionConfirmEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	runs, err := p.Store.ActivePublishRuns(ctx)
	if err != nil || len(runs) == 0 {
		return err
	}
	for _, run := range runs {
		v, getErr := p.Store.GetVersion(ctx, run.VersionSeq)
		if getErr != nil || v.IRVersion != event.Expected {
			continue
		}
		if event.Status == "CONFIRMED" {
			return p.confirmLocked(ctx, run, v, event.Observed)
		}
		run.State = "TIMEOUT"
		v.State = "timeout"
		if err := p.Store.InsertVersion(ctx, v); err != nil {
			return err
		}
		run.UpdatedAt = time.Now().UTC()
		return p.Store.UpdatePublishRun(ctx, run)
	}
	return nil
}

// PublishResult describes a successfully accepted or failed publish attempt.
type PublishResult struct {
	Seq       int64
	RunID     int64
	IRVersion string
	State     string
	IR        *ir.IR
}

var (
	// ErrPublishActive indicates another non-terminal publish is in progress.
	ErrPublishActive = errors.New("conf: another publish is already active")
	// ErrDraftChanged indicates an optimistic-concurrency base hash mismatch.
	ErrDraftChanged = errors.New("conf: draft changed since it was read")
)

// Publish is the convenience form without an optimistic-concurrency token.
func (p *Publisher) Publish(ctx context.Context, author, message string) (PublishResult, error) {
	res, err := p.PublishWithBase(ctx, author, message, "")
	if err != nil || res.RunID == 0 {
		return res, err
	}
	if err := p.Confirm(ctx, res.RunID, res.IRVersion); err != nil {
		return res, err
	}
	res.State = "effective"
	return res, nil
}

// PublishWithBase validates the current filesystem draft, snapshots it, and
// applies the compiled IR while durably advancing the publish-run state machine.
// A successful delivery is CONFIRMING; M-STATE (or Confirm) moves it to EFFECTIVE.
func (p *Publisher) PublishWithBase(ctx context.Context, author, message, baseHash string) (PublishResult, error) {
	if p == nil || p.Store == nil || p.Deliver == nil {
		return PublishResult{}, errors.New("publisher requires store and deliverer")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	draft, loadErrs, err := LoadDraft(p.DataDir)
	if err != nil {
		return PublishResult{}, err
	}
	if baseHash != "" && baseHash != draft.Hash {
		return PublishResult{}, fmt.Errorf("%w: want %s, current %s", ErrDraftChanged, baseHash, draft.Hash)
	}
	run := store.PublishRun{TriggerBy: author, BaseHash: draft.Hash, State: "VALIDATING"}
	run.ID, err = p.Store.CreatePublishRun(ctx, run)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "constraint") {
			return PublishResult{}, ErrPublishActive
		}
		return PublishResult{}, fmt.Errorf("create publish run: %w", err)
	}
	failRun := func(state string, cause error) error {
		run.State = state
		if run.ErrorsJSON == "" || run.ErrorsJSON == "[]" {
			run.ErrorsJSON = marshalErrors([]string{cause.Error()})
		}
		run.UpdatedAt = time.Now().UTC()
		_ = p.Store.UpdatePublishRun(ctx, run)
		return cause
	}
	if len(loadErrs) != 0 {
		return PublishResult{}, failRun("VALIDATE_FAILED", fmt.Errorf("draft validation failed: %d error(s)", len(loadErrs)))
	}
	var out *ir.IR
	var compileErrs []compile.CompileError
	if draft.Mode == ModeNative {
		out, err = LoadNative(filepath.Join(p.DataDir, "native.yaml"))
		if err != nil {
			return PublishResult{}, failRun("VALIDATE_FAILED", err)
		}
	} else {
		if draft.Config == nil {
			return PublishResult{}, failRun("VALIDATE_FAILED", errors.New("abstract draft has no config set"))
		}
		out, compileErrs = compile.Compile(draft.Config, compile.Options{Mode: p.Mode})
	}
	if len(compileErrs) != 0 {
		run.ErrorsJSON = marshalCompileErrors(compileErrs)
	}
	for _, e := range compileErrs {
		if e.Severity == compile.SeverityError {
			return PublishResult{}, failRun("VALIDATE_FAILED", fmt.Errorf("compile failed: %s", e.Message))
		}
	}
	if out == nil {
		return PublishResult{}, failRun("VALIDATE_FAILED", errors.New("compile returned no IR"))
	}
	run.State = "VALIDATED"
	run.UpdatedAt = time.Now().UTC()
	if err := p.Store.UpdatePublishRun(ctx, run); err != nil {
		return PublishResult{}, err
	}
	seq, err := p.Store.NextVersionSeq(ctx)
	if err != nil {
		return PublishResult{}, failRun("PUBLISH_FAILED", fmt.Errorf("reserve version: %w", err))
	}
	run.VersionSeq, run.State = seq, "PUBLISHING"
	run.UpdatedAt = time.Now().UTC()
	if err := p.Store.UpdatePublishRun(ctx, run); err != nil {
		return PublishResult{}, err
	}
	now := time.Now().UTC()
	meta := SnapshotMeta{
		Seq: seq, CreatedAt: now.Format(time.RFC3339Nano), Author: author,
		Message: message, Mode: string(p.Mode), IRVersion: out.Version,
		ParentSeq: func() int64 {
			if seq > 1 {
				return seq - 1
			}
			return 0
		}(),
		State: "publishing", Stats: map[string]int{
			"listeners": len(out.Listeners), "clusters": len(out.Clusters),
			"routes": len(out.Routes), "endpoints": len(out.Endpoints),
			"secrets": len(out.Secrets),
		},
	}
	if _, err := Snapshot(p.DataDir, seq, meta); err != nil {
		return PublishResult{}, failRun("PUBLISH_FAILED", err)
	}
	if seq > 1 {
		if diff, diffErr := DiffSnapshots(p.DataDir, seq-1, seq); diffErr == nil {
			if payload, marshalErr := json.Marshal(diff); marshalErr == nil {
				run.DiffJSON = string(payload)
				run.UpdatedAt = time.Now().UTC()
				if err := p.Store.UpdatePublishRun(ctx, run); err != nil {
					return PublishResult{}, failRun("PUBLISH_FAILED", err)
				}
			}
		}
	}
	stats, _ := json.Marshal(meta.Stats)
	v := store.Version{
		Seq: seq, CreatedAt: now, Author: author, Message: message,
		Mode: string(p.Mode), IRVersion: out.Version, State: "publishing",
		ParentSeq: func() int64 {
			if seq > 1 {
				return seq - 1
			}
			return 0
		}(),
		StatsJSON: string(stats),
	}
	if err := p.Store.InsertVersion(ctx, v); err != nil {
		return PublishResult{}, failRun("PUBLISH_FAILED", fmt.Errorf("record version: %w", err))
	}
	if err := p.Deliver.Apply(ctx, out); err != nil {
		v.State = "failed"
		_ = p.Store.InsertVersion(ctx, v)
		_ = failRun("PUBLISH_FAILED", err)
		return PublishResult{Seq: seq, RunID: run.ID, IRVersion: out.Version, State: "failed", IR: out}, err
	}
	v.State = "confirming"
	if err := p.Store.InsertVersion(ctx, v); err != nil {
		return PublishResult{}, failRun("PUBLISH_FAILED", fmt.Errorf("record confirming version: %w", err))
	}
	run.State = "CONFIRMING"
	run.UpdatedAt = time.Now().UTC()
	if err := p.Store.UpdatePublishRun(ctx, run); err != nil {
		return PublishResult{}, err
	}
	if p.state != nil {
		p.state.ExpectVersion(out.Version, 30*time.Second)
	}
	return PublishResult{Seq: seq, RunID: run.ID, IRVersion: out.Version, State: "confirming", IR: out}, nil
}

// Confirm marks a delivered run effective after M-STATE observes its IR version.
func (p *Publisher) Confirm(ctx context.Context, runID int64, observedIRVersion string) error {
	if p == nil || p.Store == nil {
		return errors.New("publisher requires store")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	run, err := p.Store.GetPublishRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.State != "CONFIRMING" {
		return fmt.Errorf("publish run %d is %s, want CONFIRMING", runID, run.State)
	}
	v, err := p.Store.GetVersion(ctx, run.VersionSeq)
	if err != nil {
		return err
	}
	if observedIRVersion != v.IRVersion {
		return fmt.Errorf("confirm version mismatch: observed %s, want %s", observedIRVersion, v.IRVersion)
	}
	return p.confirmLocked(ctx, run, v, observedIRVersion)
}

func (p *Publisher) confirmLocked(ctx context.Context, run store.PublishRun, v store.Version, observedIRVersion string) error {
	v.State = "effective"
	if err := p.Store.InsertVersion(ctx, v); err != nil {
		return err
	}
	run.State = "EFFECTIVE"
	run.UpdatedAt = time.Now().UTC()
	return p.Store.UpdatePublishRun(ctx, run)
}

func marshalErrors(values []string) string {
	b, _ := json.Marshal(values)
	return string(b)
}

func marshalCompileErrors(values []compile.CompileError) string {
	b, _ := json.Marshal(values)
	return string(b)
}
