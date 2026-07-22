package static

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"

	"github.com/linkinghack/envoy-standalone-gateway/internal/deliver"
	"github.com/linkinghack/envoy-standalone-gateway/internal/ir"
)

const staticEventBuffer = 16

// Restarter is the narrow M-PROC hot restart surface.
type Restarter interface{ HotRestart(context.Context) error }

// Server atomically delivers static bootstrap files and optionally asks
// M-PROC to make them live through an Envoy hot restart.
type Server struct {
	writer  Writer
	options RenderOptions
	log     *slog.Logger

	mu        sync.Mutex
	restarter Restarter
	status    deliver.Status
	subs      map[int]chan deliver.Event
	subSeq    int
}

// NewServer constructs a static deliverer. A nil restarter means file-only delivery.
func NewServer(writer Writer, options RenderOptions, restarter Restarter, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		writer: writer, options: options, restarter: restarter, log: log,
		status: deliver.Status{Phase: deliver.PhaseIdle}, subs: map[int]chan deliver.Event{},
	}
}

var _ deliver.Deliverer = (*Server)(nil)

// SetRestarter enables managed hot restart after the initial artifact and
// supervisor have been constructed.
func (s *Server) SetRestarter(restarter Restarter) {
	s.mu.Lock()
	s.restarter = restarter
	s.mu.Unlock()
}

// Apply renders in memory, atomically replaces the artifact and, when managed,
// waits for the next epoch to become LIVE. A failed restart restores last-good.
func (s *Server) Apply(ctx context.Context, input *ir.IR) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input == nil {
		return s.failLocked(errors.New("stage=render: nil IR"))
	}
	if s.restarter != nil && s.status.Version == input.Version && s.status.Version != "" {
		return nil
	}
	s.status.Phase = deliver.PhaseDelivering
	s.status.UpdatedAt = time.Now().UTC()
	payload, err := RenderWithOptions(input, s.options)
	if err != nil {
		return s.failLocked(fmt.Errorf("stage=render: %w", err))
	}
	previous, readErr := os.ReadFile(s.writer.OutputPath)
	hadPrevious := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return s.failLocked(fmt.Errorf("stage=read_last_good: %w", readErr))
	}
	if err := s.writer.Write(payload); err != nil {
		return s.failLocked(fmt.Errorf("stage=write_file: %w", err))
	}
	if s.restarter != nil {
		if err := s.restarter.HotRestart(ctx); err != nil {
			restoreErr := s.restore(previous, hadPrevious)
			detail := fmt.Errorf("stage=hot_restart: %w", err)
			if restoreErr != nil {
				detail = fmt.Errorf("%w; restore last-good: %v", detail, restoreErr)
			}
			s.broadcastLocked(deliver.Event{Kind: deliver.EventHotRestartFailed, Version: input.Version, Detail: detail.Error(), At: time.Now().UTC()})
			return s.failLocked(detail)
		}
	}
	s.status = deliver.Status{Version: input.Version, Phase: deliver.PhaseAwaitingConfirm, UpdatedAt: time.Now().UTC()}
	s.broadcastLocked(deliver.Event{Kind: deliver.EventApplied, Version: input.Version, At: s.status.UpdatedAt})
	s.log.Info("static: artifact applied", "version", input.Version, "path", s.writer.OutputPath, "managed", s.restarter != nil)
	return nil
}

func (s *Server) restore(previous []byte, hadPrevious bool) error {
	if hadPrevious {
		return s.writer.Write(previous)
	}
	return removeAndSync(s.writer.OutputPath)
}

func (s *Server) failLocked(err error) error {
	s.status.Phase = deliver.PhaseFailed
	s.status.Detail = err.Error()
	s.status.UpdatedAt = time.Now().UTC()
	return fmt.Errorf("static: apply: %w", err)
}

// UpdateEndpoints is unsupported because static mode snapshots endpoints at publish time.
func (s *Server) UpdateEndpoints(context.Context, map[string]*endpointv3.ClusterLoadAssignment) error {
	return deliver.ErrEndpointsUnsupported
}

// Status returns the current static delivery state.
func (s *Server) Status() deliver.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Events creates a bounded non-blocking delivery event subscription.
func (s *Server) Events() (<-chan deliver.Event, func()) {
	out := make(chan deliver.Event, staticEventBuffer)
	s.mu.Lock()
	id := s.subSeq
	s.subSeq++
	s.subs[id] = out
	s.mu.Unlock()
	var once sync.Once
	return out, func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.subs, id)
			s.mu.Unlock()
			close(out)
		})
	}
}

func (s *Server) broadcastLocked(event deliver.Event) {
	for _, subscriber := range s.subs {
		select {
		case subscriber <- event:
		default:
		}
	}
}
