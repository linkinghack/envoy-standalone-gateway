package conf

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// SourceFile is one file in a complete draft replacement.
type SourceFile struct {
	Path    string
	Content []byte
}

// ReplaceDraft validates and replaces the entire filesystem draft. It uses a
// staging directory and restores the prior draft if the commit cannot finish.
func ReplaceDraft(dataDir, mode string, files []SourceFile, expectedHash string) (string, error) {
	if mode != ModeAbstract && mode != ModeNative {
		return "", fmt.Errorf("invalid draft mode %q", mode)
	}
	current, err := DraftHash(dataDir)
	if err != nil {
		return "", err
	}
	if expectedHash != "" && expectedHash != current {
		return "", fmt.Errorf("%w: want %s, current %s", ErrDraftChanged, expectedHash, current)
	}
	if err := validateSourceSet(mode, files); err != nil {
		return "", err
	}
	tmpRoot := filepath.Join(dataDir, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o700); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(tmpRoot, "draft-stage-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := os.MkdirAll(filepath.Join(stage, "config.d"), 0o700); err != nil {
		return "", err
	}
	for _, file := range files {
		target := filepath.Join(stage, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, file.Content, 0o600); err != nil {
			return "", err
		}
	}
	draft, loadErrs, err := LoadDraft(stage)
	if err != nil {
		return "", fmt.Errorf("validate replacement draft: %w", err)
	}
	if len(loadErrs) != 0 {
		return "", fmt.Errorf("validate replacement draft: %s", loadErrs[0])
	}
	if draft.Mode != mode {
		return "", fmt.Errorf("replacement draft resolved to mode %q, want %q", draft.Mode, mode)
	}
	if mode == ModeNative {
		if _, err := LoadNative(filepath.Join(stage, "native.yaml")); err != nil {
			return "", fmt.Errorf("validate native draft: %w", err)
		}
	}
	if err := commitDraftStage(dataDir, stage); err != nil {
		return "", err
	}
	return DraftHash(dataDir)
}

func validateSourceSet(mode string, files []SourceFile) error {
	seen := make(map[string]struct{}, len(files))
	if mode == ModeNative && len(files) != 1 {
		return errors.New("native draft requires exactly one native.yaml file")
	}
	for _, file := range files {
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(file.Path)))
		if file.Path == "" || clean != file.Path || filepath.IsAbs(filepath.FromSlash(file.Path)) || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("unsafe draft path %q", file.Path)
		}
		if _, ok := seen[clean]; ok {
			return fmt.Errorf("duplicate draft path %q", clean)
		}
		seen[clean] = struct{}{}
		switch mode {
		case ModeNative:
			if clean != "native.yaml" {
				return errors.New("native draft path must be native.yaml")
			}
		case ModeAbstract:
			ext := filepath.Ext(clean)
			if filepath.Dir(clean) != "config.d" || (ext != ".yaml" && ext != ".yml") {
				return fmt.Errorf("protocol draft path %q must be config.d/*.yaml|*.yml", clean)
			}
		}
	}
	return nil
}

func commitDraftStage(dataDir, stage string) error {
	backup, err := os.MkdirTemp(filepath.Join(dataDir, "tmp"), "draft-backup-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(backup) }()
	targets := []string{"config.d", "native.yaml"}
	moved := make([]string, 0, len(targets))
	for _, name := range targets {
		old := filepath.Join(dataDir, name)
		if _, err := os.Stat(old); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := moveDraftPath(old, filepath.Join(backup, name), os.Rename); err != nil {
			restoreDraftBackup(dataDir, backup, moved)
			return fmt.Errorf("backup draft %s: %w", name, err)
		}
		moved = append(moved, name)
	}
	installed := make([]string, 0, len(targets))
	for _, name := range targets {
		source := filepath.Join(stage, name)
		if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			rollbackDraftInstall(dataDir, backup, installed, moved)
			return err
		}
		if err := moveDraftPath(source, filepath.Join(dataDir, name), os.Rename); err != nil {
			rollbackDraftInstall(dataDir, backup, installed, moved)
			return fmt.Errorf("install draft %s: %w", name, err)
		}
		installed = append(installed, name)
	}
	return nil
}

func rollbackDraftInstall(dataDir, backup string, installed, moved []string) {
	for _, name := range installed {
		_ = os.RemoveAll(filepath.Join(dataDir, name))
	}
	restoreDraftBackup(dataDir, backup, moved)
}

func restoreDraftBackup(dataDir, backup string, moved []string) {
	sort.Strings(moved)
	for _, name := range moved {
		_ = moveDraftPath(filepath.Join(backup, name), filepath.Join(dataDir, name), os.Rename)
	}
}

// moveDraftPath falls back to copy-and-remove when a container overlay cannot
// rename an image-layer directory into its writable layer (EXDEV). The backup
// is fully copied before the source is removed, preserving rollback safety.
func moveDraftPath(source, target string, rename func(string, string) error) error {
	if err := rename(source, target); err == nil {
		return nil
	} else if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyDraftPath(source, target); err != nil {
		_ = os.RemoveAll(target)
		return err
	}
	if err := os.RemoveAll(source); err != nil {
		_ = os.RemoveAll(target)
		return fmt.Errorf("remove copied draft source: %w", err)
	}
	return nil
}

func copyDraftPath(source, target string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("draft path %s is a symbolic link", source)
	}
	if !info.IsDir() {
		return copyDraftFile(source, target, info.Mode().Perm())
	}
	if err := os.Mkdir(target, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyDraftPath(filepath.Join(source, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyDraftFile(source, target string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}
