package static

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Writer atomically replaces one static artifact on the same filesystem.
type Writer struct {
	OutputPath   string
	BeforeRename func(tempPath, outputPath string) error
}

// Write performs write → fsync(file) → rename → fsync(directory).
func (w Writer) Write(payload []byte) error {
	if !filepath.IsAbs(w.OutputPath) {
		return fmt.Errorf("static: output path %q must be absolute", w.OutputPath)
	}
	dir := filepath.Dir(w.OutputPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("static: create output directory: %w", err)
	}
	file, err := os.CreateTemp(dir, ".envoy.yaml-*")
	if err != nil {
		return fmt.Errorf("static: create output temp: %w", err)
	}
	tempPath := file.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("static: chmod output temp: %w", err)
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return fmt.Errorf("static: write output temp: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("static: sync output temp: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("static: close output temp: %w", err)
	}
	if w.BeforeRename != nil {
		if err := w.BeforeRename(tempPath, w.OutputPath); err != nil {
			return fmt.Errorf("static: before rename: %w", err)
		}
	}
	if err := os.Rename(tempPath, w.OutputPath); err != nil {
		return fmt.Errorf("static: replace output: %w", err)
	}
	return syncDirectory(dir)
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("static: open output directory: %w", err)
	}
	defer func() { _ = directory.Close() }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("static: sync output directory: %w", err)
	}
	return nil
}

func removeAndSync(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
