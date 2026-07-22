package conf

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileDiff is a deterministic source-file diff between two immutable versions.
type FileDiff struct {
	Path   string `json:"path"`
	Status string `json:"status"` // added | removed | changed | unchanged
	Patch  string `json:"patch,omitempty"`
}

// SnapshotDiff is the text-view diff consumed by publish previews.
type SnapshotDiff struct {
	From  int64      `json:"from"`
	To    int64      `json:"to"`
	Files []FileDiff `json:"files"`
}

// DiffSnapshots compares source files under two version snapshots.
func DiffSnapshots(dataDir string, from, to int64) (SnapshotDiff, error) {
	if from <= 0 || to <= 0 {
		return SnapshotDiff{}, errors.New("snapshot sequence must be positive")
	}
	left, err := snapshotFiles(dataDir, from)
	if err != nil {
		return SnapshotDiff{}, err
	}
	right, err := snapshotFiles(dataDir, to)
	if err != nil {
		return SnapshotDiff{}, err
	}
	return diffFileMaps(from, to, left, right), nil
}

// DiffDraftAgainstSnapshot compares an immutable version with the current
// filesystem draft. A zero snapshot sequence treats the base as empty.
func DiffDraftAgainstSnapshot(dataDir string, from int64) (SnapshotDiff, error) {
	left := map[string]string{}
	var err error
	if from > 0 {
		left, err = snapshotFiles(dataDir, from)
		if err != nil {
			return SnapshotDiff{}, err
		}
	}
	right := map[string]string{}
	files, err := sourceFiles(dataDir)
	if err != nil {
		return SnapshotDiff{}, err
	}
	for _, file := range files {
		rel, err := filepath.Rel(dataDir, file)
		if err != nil {
			return SnapshotDiff{}, err
		}
		content, err := os.ReadFile(file)
		if err != nil {
			return SnapshotDiff{}, err
		}
		right[filepath.ToSlash(rel)] = normalizeText(string(content))
	}
	return diffFileMaps(from, 0, left, right), nil
}

func diffFileMaps(from, to int64, left, right map[string]string) SnapshotDiff {
	keys := make(map[string]struct{}, len(left)+len(right))
	for k := range left {
		keys[k] = struct{}{}
	}
	for k := range right {
		keys[k] = struct{}{}
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	out := SnapshotDiff{From: from, To: to}
	for _, name := range names {
		a, aok := left[name]
		b, bok := right[name]
		switch {
		case !aok:
			out.Files = append(out.Files, FileDiff{Path: name, Status: "added", Patch: unifiedPatch(nil, splitLines(b))})
		case !bok:
			out.Files = append(out.Files, FileDiff{Path: name, Status: "removed", Patch: unifiedPatch(splitLines(a), nil)})
		case a == b:
			out.Files = append(out.Files, FileDiff{Path: name, Status: "unchanged"})
		default:
			out.Files = append(out.Files, FileDiff{Path: name, Status: "changed", Patch: unifiedPatch(splitLines(a), splitLines(b))})
		}
	}
	return out
}

func snapshotFiles(dataDir string, seq int64) (map[string]string, error) {
	root := filepath.Join(dataDir, "versions", fmt.Sprintf("%06d", seq), "config")
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
		return nil, fmt.Errorf("read snapshot %06d: %w", seq, err)
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
		out[filepath.ToSlash(rel)] = normalizeText(string(b))
	}
	return out, nil
}

func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.Join(lines, "\n")
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// unifiedPatch emits a compact deterministic line patch. It intentionally
// keeps the implementation dependency-free; callers can render the status
// summary even when only one side exists.
func unifiedPatch(a, b []string) string {
	var out strings.Builder
	out.WriteString("--- from\n+++ to\n")
	max := len(a)
	if len(b) > max {
		max = len(b)
	}
	for i := 0; i < max; i++ {
		switch {
		case i >= len(a):
			out.WriteString("+ " + b[i] + "\n")
		case i >= len(b):
			out.WriteString("- " + a[i] + "\n")
		case a[i] == b[i]:
			out.WriteString("  " + a[i] + "\n")
		default:
			out.WriteString("- " + a[i] + "\n")
			out.WriteString("+ " + b[i] + "\n")
		}
	}
	return out.String()
}
