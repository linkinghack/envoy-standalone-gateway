package conf

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
)

const (
	// ModeAbstract identifies the multi-file protocol source.
	ModeAbstract = "abstract"
	// ModeNative identifies the single native.yaml source.
	ModeNative = "native"
)

// Draft is the logical combination of source files and their parsed result.
// LoadErrors are schema/semantic errors and do not prevent the management
// process from starting.
type Draft struct {
	DataDir string
	Mode    string
	Config  *protocol.ConfigSet
	Hash    string
	Files   []string
	Errors  []protocol.LoadError
}

// LoadDraft loads config.d or native.yaml from dataDir. The config.d
// directory is created when absent so a fresh installation has an empty draft.
func LoadDraft(dataDir string) (*Draft, []protocol.LoadError, error) {
	if dataDir == "" {
		return nil, nil, errors.New("data directory is empty")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create data directory: %w", err)
	}
	configDir := filepath.Join(dataDir, "config.d")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create config.d: %w", err)
	}
	nativePath := filepath.Join(dataDir, "native.yaml")
	nativeInfo, nativeErr := os.Stat(nativePath)
	if nativeErr != nil && !errors.Is(nativeErr, os.ErrNotExist) {
		return nil, nil, fmt.Errorf("stat native.yaml: %w", nativeErr)
	}
	files, err := sourceFiles(dataDir)
	if err != nil {
		return nil, nil, err
	}
	var configFiles []string
	for _, f := range files {
		if filepath.Dir(f) == configDir {
			configFiles = append(configFiles, f)
		}
	}
	if nativeInfo != nil && len(configFiles) > 0 {
		return nil, nil, fmt.Errorf("native.yaml and config.d/*.yaml|*.yml are mutually exclusive")
	}
	hash, err := hashFiles(dataDir, files)
	if err != nil {
		return nil, nil, err
	}
	d := &Draft{DataDir: dataDir, Hash: hash, Files: files}
	if nativeInfo != nil {
		d.Mode = ModeNative
		return d, nil, nil
	}
	d.Mode = ModeAbstract
	cs, loadErrs := protocol.LoadDir(configDir)
	d.Config, d.Errors = cs, loadErrs
	return d, loadErrs, nil
}

// DraftHash computes the deterministic hash of the current source files.
func DraftHash(dataDir string) (string, error) {
	files, err := sourceFiles(dataDir)
	if err != nil {
		return "", err
	}
	return hashFiles(dataDir, files)
}

func sourceFiles(dataDir string) ([]string, error) {
	var files []string
	configDir := filepath.Join(dataDir, "config.d")
	if info, err := os.Stat(configDir); err == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", configDir)
		}
		entries, err := os.ReadDir(configDir)
		if err != nil {
			return nil, fmt.Errorf("read config.d: %w", err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := filepath.Ext(e.Name())
			if ext == ".yaml" || ext == ".yml" {
				files = append(files, filepath.Join(configDir, e.Name()))
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat config.d: %w", err)
	}
	native := filepath.Join(dataDir, "native.yaml")
	if _, err := os.Stat(native); err == nil {
		files = append(files, native)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat native.yaml: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func hashFiles(dataDir string, files []string) (string, error) {
	h := sha256.New()
	var buf [8]byte
	for _, file := range files {
		rel, err := filepath.Rel(dataDir, file)
		if err != nil {
			return "", fmt.Errorf("relative source path: %w", err)
		}
		rel = filepath.ToSlash(rel)
		if err := writeHashPart(h, []byte(rel), buf[:]); err != nil {
			return "", err
		}
		content, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", file, err)
		}
		if err := writeHashPart(h, content, buf[:]); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeHashPart(w io.Writer, value, scratch []byte) error {
	binary.BigEndian.PutUint64(scratch, uint64(len(value)))
	if _, err := w.Write(scratch); err != nil {
		return err
	}
	_, err := w.Write(value)
	return err
}

// SnapshotMeta is the metadata persisted alongside a source snapshot.
type SnapshotMeta struct {
	Seq        int64  `json:"seq"`
	CreatedAt  string `json:"createdAt"`
	Author     string `json:"author"`
	Message    string `json:"message"`
	Mode       string `json:"mode"`
	IRVersion  string `json:"irVersion"`
	State      string `json:"state"`
	ParentSeq  int64  `json:"parentSeq"`
	RollbackOf int64  `json:"rollbackOf"`
	Stats      any    `json:"stats,omitempty"`
}

// Snapshot atomically copies the current source files into versions/%06d.
func Snapshot(dataDir string, seq int64, meta SnapshotMeta) (string, error) {
	if seq <= 0 {
		return "", errors.New("snapshot sequence must be positive")
	}
	files, err := sourceFiles(dataDir)
	if err != nil {
		return "", err
	}
	versionsDir := filepath.Join(dataDir, "versions")
	tmpDir := filepath.Join(dataDir, "tmp")
	if err := os.MkdirAll(versionsDir, 0o700); err != nil {
		return "", fmt.Errorf("create versions directory: %w", err)
	}
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return "", fmt.Errorf("create tmp directory: %w", err)
	}
	finalDir := filepath.Join(versionsDir, fmt.Sprintf("%06d", seq))
	if _, err := os.Stat(finalDir); err == nil {
		return "", fmt.Errorf("version %06d already exists", seq)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	tmp, err := os.MkdirTemp(tmpDir, fmt.Sprintf("version-%06d-", seq))
	if err != nil {
		return "", fmt.Errorf("create snapshot temp directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	defer cleanup()
	configOut := filepath.Join(tmp, "config")
	for _, src := range files {
		rel, err := filepath.Rel(dataDir, src)
		if err != nil {
			return "", err
		}
		dst := filepath.Join(configOut, rel)
		if err := copyFile(src, dst); err != nil {
			return "", err
		}
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode snapshot metadata: %w", err)
	}
	metaBytes = append(metaBytes, '\n')
	if err := os.WriteFile(filepath.Join(tmp, "meta.json"), metaBytes, 0o600); err != nil {
		return "", fmt.Errorf("write snapshot metadata: %w", err)
	}
	if err := os.Rename(tmp, finalDir); err != nil {
		return "", fmt.Errorf("commit snapshot: %w", err)
	}
	return finalDir, nil
}

func copyFile(src, dst string) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read snapshot source %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	if err := os.WriteFile(dst, content, 0o600); err != nil {
		return fmt.Errorf("write snapshot file %s: %w", dst, err)
	}
	return nil
}
