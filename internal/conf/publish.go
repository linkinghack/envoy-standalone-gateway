package conf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/linkinghack/envoy-standalone-gateway/internal/compile"
	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver"
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
	"github.com/linkinghack/envoy-standalone-gateway/internal/store"
)

// Publisher coordinates the minimum publish path across M-CONF, M-COMPILE,
// M-STORE and M-DELIVER.
type Publisher struct {
	DataDir string
	Store   *store.Store
	Deliver deliver.Deliverer
	Mode    compile.Mode
}

// PublishResult describes a successfully accepted or failed publish attempt.
type PublishResult struct {
	Seq       int64
	IRVersion string
	State     string
	IR        *ir.IR
}

// Publish validates the current filesystem draft, snapshots it, then applies
// the compiled IR. A failed delivery remains recorded as a failed version.
func (p *Publisher) Publish(ctx context.Context, author, message string) (PublishResult, error) {
	if p == nil || p.Store == nil || p.Deliver == nil {
		return PublishResult{}, errors.New("publisher requires store and deliverer")
	}
	draft, loadErrs, err := LoadDraft(p.DataDir)
	if err != nil {
		return PublishResult{}, err
	}
	if len(loadErrs) != 0 {
		return PublishResult{}, fmt.Errorf("draft validation failed: %d error(s)", len(loadErrs))
	}
	if draft.Config == nil {
		return PublishResult{}, errors.New("native.yaml publish is not supported yet")
	}
	out, compileErrs := compile.Compile(draft.Config, compile.Options{Mode: p.Mode})
	for _, e := range compileErrs {
		if e.Severity == compile.SeverityError {
			return PublishResult{}, fmt.Errorf("compile failed: %s", e.Message)
		}
	}
	if out == nil {
		return PublishResult{}, errors.New("compile returned no IR")
	}
	seq, err := p.Store.NextVersionSeq(ctx)
	if err != nil {
		return PublishResult{}, fmt.Errorf("reserve version: %w", err)
	}
	now := time.Now().UTC()
	meta := SnapshotMeta{
		Seq: seq, CreatedAt: now.Format(time.RFC3339Nano), Author: author,
		Message: message, Mode: string(p.Mode), IRVersion: out.Version,
		State: "publishing", Stats: map[string]int{
			"listeners": len(out.Listeners), "clusters": len(out.Clusters),
			"routes": len(out.Routes), "endpoints": len(out.Endpoints),
			"secrets": len(out.Secrets),
		},
	}
	if _, err := Snapshot(p.DataDir, seq, meta); err != nil {
		return PublishResult{}, err
	}
	stats, _ := json.Marshal(meta.Stats)
	v := store.Version{
		Seq: seq, CreatedAt: now, Author: author, Message: message,
		Mode: string(p.Mode), IRVersion: out.Version, State: "publishing",
		StatsJSON: string(stats),
	}
	if err := p.Store.InsertVersion(ctx, v); err != nil {
		return PublishResult{}, fmt.Errorf("record version: %w", err)
	}
	if err := p.Deliver.Apply(ctx, out); err != nil {
		v.State = "failed"
		_ = p.Store.InsertVersion(ctx, v)
		return PublishResult{Seq: seq, IRVersion: out.Version, State: "failed", IR: out}, err
	}
	v.State = "effective"
	if err := p.Store.InsertVersion(ctx, v); err != nil {
		return PublishResult{}, fmt.Errorf("record effective version: %w", err)
	}
	return PublishResult{Seq: seq, IRVersion: out.Version, State: "effective", IR: out}, nil
}
