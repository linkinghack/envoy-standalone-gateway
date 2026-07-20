package conf

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/linkinghack/envoy-standalone-gateway/internal/protocol"
	yamlv3 "gopkg.in/yaml.v3"
)

// WriteDraftDocument replaces one YAML document identified by Origin. The
// source is atomically rewritten and the resulting directory is strict-loaded
// before commit, so an invalid edit never reaches the filesystem.
func WriteDraftDocument(dataDir string, origin protocol.Origin, document []byte) error {
	path, err := sourcePath(dataDir, origin.File)
	if err != nil {
		return err
	}
	replacement, err := parseSingleYAML(document)
	if err != nil {
		return fmt.Errorf("parse replacement document: %w", err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read draft source: %w", err)
	}
	var root yamlv3.Node
	dec := yamlv3.NewDecoder(bytes.NewReader(original))
	var docs []*yamlv3.Node
	for {
		var node yamlv3.Node
		if err := dec.Decode(&node); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return fmt.Errorf("parse draft source: %w", err)
		}
		if len(node.Content) == 0 {
			continue
		}
		docs = append(docs, node.Content[0])
	}
	if origin.DocIndex < 0 || origin.DocIndex >= len(docs) {
		return fmt.Errorf("origin document index %d out of range (documents=%d)", origin.DocIndex, len(docs))
	}
	docs[origin.DocIndex] = replacement
	root.Kind = yamlv3.DocumentNode
	root.Content = docs
	var out bytes.Buffer
	enc := yamlv3.NewEncoder(&out)
	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			_ = enc.Close()
			return err
		}
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return commitValidated(path, out.Bytes(), dataDir)
}

// DeleteDraftDocument removes one YAML document identified by Origin.
func DeleteDraftDocument(dataDir string, origin protocol.Origin) error {
	path, err := sourcePath(dataDir, origin.File)
	if err != nil {
		return err
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := yamlv3.NewDecoder(bytes.NewReader(original))
	var docs []*yamlv3.Node
	for {
		var node yamlv3.Node
		err := dec.Decode(&node)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if len(node.Content) != 0 {
			docs = append(docs, node.Content[0])
		}
	}
	if origin.DocIndex < 0 || origin.DocIndex >= len(docs) {
		return fmt.Errorf("origin document index %d out of range (documents=%d)", origin.DocIndex, len(docs))
	}
	docs = append(docs[:origin.DocIndex], docs[origin.DocIndex+1:]...)
	if len(docs) == 0 {
		return os.Remove(path)
	}
	var out bytes.Buffer
	enc := yamlv3.NewEncoder(&out)
	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			_ = enc.Close()
			return err
		}
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return commitValidated(path, out.Bytes(), dataDir)
}

func sourcePath(dataDir, file string) (string, error) {
	if dataDir == "" || file == "" {
		return "", errors.New("data directory and origin file are required")
	}
	absData, err := filepath.Abs(dataDir)
	if err != nil {
		return "", err
	}
	absFile := file
	if !filepath.IsAbs(absFile) {
		absFile = filepath.Join(absData, file)
	}
	absFile, err = filepath.Abs(absFile)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absData, absFile)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("origin file is outside data directory")
	}
	if filepath.Ext(absFile) != ".yaml" && filepath.Ext(absFile) != ".yml" {
		return "", errors.New("origin file must be YAML")
	}
	return absFile, nil
}

func parseSingleYAML(document []byte) (*yamlv3.Node, error) {
	dec := yamlv3.NewDecoder(bytes.NewReader(document))
	var node yamlv3.Node
	if err := dec.Decode(&node); err != nil {
		return nil, err
	}
	if len(node.Content) == 0 {
		return nil, errors.New("empty YAML document")
	}
	var extra yamlv3.Node
	if err := dec.Decode(&extra); err == nil && len(extra.Content) != 0 {
		return nil, errors.New("replacement must contain one YAML document")
	}
	return node.Content[0], nil
}

func commitValidated(path string, content []byte, dataDir string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".draft-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	checkDir, err := os.MkdirTemp("", "esgw-draft-check-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(checkDir) }()
	configDir := filepath.Join(checkDir, "config.d")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	files, err := sourceFiles(dataDir)
	if err != nil {
		return err
	}
	for _, source := range files {
		rel, err := filepath.Rel(dataDir, source)
		if err != nil {
			return err
		}
		target := filepath.Join(checkDir, rel)
		absSource, _ := filepath.Abs(source)
		if absSource == path {
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(target, content, 0o600); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(source, target); err != nil {
			return err
		}
	}
	if _, errs := protocol.LoadDir(configDir); len(errs) != 0 {
		return fmt.Errorf("draft validation failed: %s", errs[0])
	}
	return os.Rename(tmpName, path)
}
