package conf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// RollbackSource copies a historical snapshot's source back into the working
// tree. It does not publish; callers must invoke the normal publish path.
func RollbackSource(dataDir string, seq int64, force bool) error {
	if seq <= 0 {
		return errors.New("rollback sequence must be positive")
	}
	source := filepath.Join(dataDir, "versions", fmt.Sprintf("%06d", seq), "config")
	if _, err := os.Stat(source); err != nil {
		return fmt.Errorf("rollback source: %w", err)
	}
	if !force {
		current, err := sourceFiles(dataDir)
		if err != nil {
			return err
		}
		// A non-empty draft is always considered potentially modified. The
		// API must pass force after presenting the overwrite confirmation.
		if len(current) != 0 {
			return errors.New("rollback would overwrite current draft; force is required")
		}
	}
	files, err := sourceFilesFrom(source)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, filepath.ToSlash(path))
	}
	sort.Strings(paths)
	mode := ModeAbstract
	if len(paths) == 1 && paths[0] == "native.yaml" {
		mode = ModeNative
	}
	replacement := make([]SourceFile, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(files[filepath.FromSlash(path)])
		if err != nil {
			return err
		}
		replacement = append(replacement, SourceFile{Path: path, Content: content})
	}
	expected, err := DraftHash(dataDir)
	if err != nil {
		return err
	}
	_, err = ReplaceDraft(dataDir, mode, replacement, expected)
	return err
}

func sourceFilesFrom(root string) (map[string]string, error) {
	var paths []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	out := make(map[string]string, len(paths))
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil, err
		}
		out[rel] = path
	}
	return out, nil
}

// RollbackPublish restores a snapshot and publishes it through the regular
// compiler/deliverer path, recording rollback metadata in the resulting version.
func (p *Publisher) RollbackPublish(ctx context.Context, seq int64, author, message string, force bool) (PublishResult, error) {
	if p == nil || p.Store == nil || p.Deliver == nil {
		return PublishResult{}, errors.New("publisher requires store and deliverer")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := RollbackSource(p.DataDir, seq, force); err != nil {
		return PublishResult{}, err
	}
	hash, err := DraftHash(p.DataDir)
	if err != nil {
		return PublishResult{}, err
	}
	return p.publishWithBaseLocked(ctx, author, message, hash, seq)
}
