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
	if err := os.RemoveAll(filepath.Join(dataDir, "config.d")); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "config.d"), 0o700); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dataDir, "native.yaml")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for rel, src := range files {
		dst := filepath.Join(dataDir, rel)
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}
	return nil
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
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		out[rel] = path
		_ = b
	}
	return out, nil
}

// RollbackPublish restores a snapshot and publishes it through the regular
// compiler/deliverer path, recording rollback metadata in the resulting version.
func (p *Publisher) RollbackPublish(ctx context.Context, seq int64, author, message string, force bool) (PublishResult, error) {
	if p == nil || p.Store == nil {
		return PublishResult{}, errors.New("publisher requires store")
	}
	if err := RollbackSource(p.DataDir, seq, force); err != nil {
		return PublishResult{}, err
	}
	res, err := p.Publish(ctx, author, message)
	if err != nil {
		return res, err
	}
	v, err := p.Store.GetVersion(ctx, res.Seq)
	if err != nil {
		return res, err
	}
	v.RollbackOf = seq
	if res.Seq > 1 {
		v.ParentSeq = res.Seq - 1
	}
	if err := p.Store.InsertVersion(ctx, v); err != nil {
		return res, err
	}
	return res, nil
}
