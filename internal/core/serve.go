// Package core is the M-CORE process composition and lifecycle layer.
package core

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/linkinghack/envoy-standalone-gateway/internal/config"
)

// RunServe is the CLI adapter retained for the serve command. The explicit
// configDir must resolve to dataDir/config.d so there is only one source truth.
func RunServe(ctx context.Context, cfg *config.Config, configDir string, log *slog.Logger) error {
	if cfg == nil {
		return fmt.Errorf("core: nil config")
	}
	expected := filepath.Join(cfg.DataDir, "config.d")
	if filepath.Clean(configDir) != filepath.Clean(expected) {
		return fmt.Errorf("core: config directory %s must equal dataDir source %s", configDir, expected)
	}
	app, err := NewApp(cfg, nil, log)
	if err != nil {
		return err
	}
	return app.Run(ctx)
}
