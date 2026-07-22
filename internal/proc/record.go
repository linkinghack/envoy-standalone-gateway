package proc

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrNoRecord indicates that no managed process has been recorded yet.
var ErrNoRecord = errors.New("proc: no process record")

// Record is the durable adoption clue. It is not sufficient to authorize a
// signal until the live /proc executable identity is also verified.
type Record struct {
	PID          int       `json:"pid"`
	BaseID       uint32    `json:"baseID"`
	Epoch        int       `json:"epoch"`
	NextEpoch    int       `json:"nextEpoch"`
	ConfigPath   string    `json:"configPath"`
	BinaryPath   string    `json:"binaryPath"`
	StartedAt    time.Time `json:"startedAt"`
	EnvoyVersion string    `json:"envoyVersion"`
	State        string    `json:"state"`
}

// RecordStore atomically persists proc.json with owner-only permissions.
type RecordStore struct{ Path string }

// Load reads and validates the current process record.
func (s RecordStore) Load() (Record, error) {
	payload, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, ErrNoRecord
	}
	if err != nil {
		return Record{}, fmt.Errorf("proc: read record: %w", err)
	}
	var record Record
	if err := json.Unmarshal(payload, &record); err != nil {
		return Record{}, fmt.Errorf("proc: decode record: %w", err)
	}
	if record.PID <= 0 || record.Epoch < 0 || record.ConfigPath == "" || record.BinaryPath == "" {
		return Record{}, errors.New("proc: invalid process record")
	}
	if record.NextEpoch <= record.Epoch {
		record.NextEpoch = record.Epoch + 1
	}
	return record, nil
}

// Save atomically replaces the current process record and syncs its directory.
func (s RecordStore) Save(record Record) error {
	if s.Path == "" {
		return errors.New("proc: empty record path")
	}
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("proc: create record directory: %w", err)
	}
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("proc: encode record: %w", err)
	}
	payload = append(payload, '\n')
	file, err := os.CreateTemp(dir, ".proc.json-*")
	if err != nil {
		return fmt.Errorf("proc: create record temp: %w", err)
	}
	tempPath := file.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("proc: chmod record temp: %w", err)
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return fmt.Errorf("proc: write record temp: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("proc: sync record temp: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("proc: close record temp: %w", err)
	}
	if err := os.Rename(tempPath, s.Path); err != nil {
		return fmt.Errorf("proc: replace record: %w", err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("proc: open record directory: %w", err)
	}
	defer func() { _ = directory.Close() }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("proc: sync record directory: %w", err)
	}
	return nil
}
