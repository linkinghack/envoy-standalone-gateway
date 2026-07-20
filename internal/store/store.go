package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Version is the immutable version index stored in SQLite. The corresponding
// source files and artifacts live under versions/%06d on disk.
type Version struct {
	Seq        int64
	CreatedAt  time.Time
	Author     string
	Message    string
	Mode       string
	IRVersion  string
	State      string
	ParentSeq  int64
	RollbackOf int64
	StatsJSON  string
}

// PublishRun is the durable record of one publish state-machine execution.
// Error and diff payloads are JSON kept opaque to M-STORE.
type PublishRun struct {
	ID         int64
	VersionSeq int64
	TriggerBy  string
	BaseHash   string
	State      string
	ErrorsJSON string
	DiffJSON   string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Store owns one SQLite database connection.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database and applies all migrations.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS versions (
  seq INTEGER PRIMARY KEY,
  created_at TEXT NOT NULL,
  author TEXT NOT NULL,
  message TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL,
  ir_version TEXT NOT NULL,
  state TEXT NOT NULL,
  parent_seq INTEGER NOT NULL DEFAULT 0,
  rollback_of INTEGER NOT NULL DEFAULT 0,
  stats_json TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS publish_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  version_seq INTEGER,
  trigger_by TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL,
  base_hash TEXT NOT NULL DEFAULT '',
  errors_json TEXT NOT NULL DEFAULT '[]',
  diff_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS one_active_publish
  ON publish_runs((state IN ('VALIDATING','VALIDATED','PUBLISHING','CONFIRMING')))
  WHERE state IN ('VALIDATING','VALIDATED','PUBLISHING','CONFIRMING');
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY,
  username TEXT UNIQUE NOT NULL,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  token TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id),
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  ip TEXT,
  user_agent TEXT
);
CREATE TABLE IF NOT EXISTS audit_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts TEXT NOT NULL,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  object TEXT NOT NULL DEFAULT '',
  detail_json TEXT NOT NULL DEFAULT '{}',
  ip TEXT
);
CREATE TABLE IF NOT EXISTS certificates (
  name TEXT PRIMARY KEY,
  sans_json TEXT NOT NULL DEFAULT '[]',
  not_after TEXT,
  updated_at TEXT NOT NULL
);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("run sqlite migration: %w", err)
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version == 0 {
		if _, err := s.db.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at) VALUES(1, ?)", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("record schema migration: %w", err)
		}
	}
	// S3's first migration predated version_seq/trigger_by. Keep upgrades
	// additive so an existing data directory remains usable.
	for _, alter := range []string{
		"ALTER TABLE publish_runs ADD COLUMN version_seq INTEGER",
		"ALTER TABLE publish_runs ADD COLUMN trigger_by TEXT NOT NULL DEFAULT ''",
	} {
		if _, err := s.db.ExecContext(ctx, alter); err != nil && !isDuplicateColumn(err) {
			return fmt.Errorf("upgrade publish_runs: %w", err)
		}
	}
	return nil
}

func isDuplicateColumn(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "duplicate column") || strings.Contains(err.Error(), "already exists"))
}

// GetSetting returns a setting, or (false, nil) when it is absent.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// SetSetting upserts a setting.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		key, value)
	return err
}

// NextVersionSeq reserves the next monotonically increasing version number.
// The connection is serialized by Store's single-connection configuration.
func (s *Store) NextVersionSeq(ctx context.Context) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var seq int64
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(seq), 0) + 1 FROM versions").Scan(&seq); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO versions(seq,created_at,author,message,mode,ir_version,state,parent_seq,rollback_of,stats_json) VALUES(?,?,?,?,?,?,?,?,?,?)",
		seq, time.Now().UTC().Format(time.RFC3339Nano), "", "", "", "", "RESERVED", 0, 0, "{}"); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return seq, nil
}

// InsertVersion inserts or replaces the metadata for a reserved sequence.
func (s *Store) InsertVersion(ctx context.Context, v Version) error {
	if v.Seq <= 0 {
		return errors.New("version sequence must be positive")
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
	}
	if v.StatsJSON == "" {
		v.StatsJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO versions(seq,created_at,author,message,mode,ir_version,state,parent_seq,rollback_of,stats_json)
VALUES(?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(seq) DO UPDATE SET
 created_at=excluded.created_at, author=excluded.author, message=excluded.message,
 mode=excluded.mode, ir_version=excluded.ir_version, state=excluded.state,
 parent_seq=excluded.parent_seq, rollback_of=excluded.rollback_of, stats_json=excluded.stats_json`,
		v.Seq, v.CreatedAt.UTC().Format(time.RFC3339Nano), v.Author, v.Message,
		v.Mode, v.IRVersion, v.State, v.ParentSeq, v.RollbackOf, v.StatsJSON)
	return err
}

// GetVersion reads one version by sequence.
func (s *Store) GetVersion(ctx context.Context, seq int64) (Version, error) {
	var v Version
	var created string
	err := s.db.QueryRowContext(ctx, `
SELECT seq,created_at,author,message,mode,ir_version,state,parent_seq,rollback_of,stats_json
FROM versions WHERE seq = ?`, seq).Scan(
		&v.Seq, &created, &v.Author, &v.Message, &v.Mode, &v.IRVersion,
		&v.State, &v.ParentSeq, &v.RollbackOf, &v.StatsJSON)
	if err != nil {
		return Version{}, err
	}
	v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return Version{}, fmt.Errorf("parse version created_at: %w", err)
	}
	return v, nil
}

// CreatePublishRun inserts a new run. SQLite's one_active_publish partial
// index turns concurrent active submissions into a deterministic constraint
// error for the caller to report as a conflict.
func (s *Store) CreatePublishRun(ctx context.Context, r PublishRun) (int64, error) {
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = r.CreatedAt
	}
	if r.ErrorsJSON == "" {
		r.ErrorsJSON = "[]"
	}
	if r.DiffJSON == "" {
		r.DiffJSON = "{}"
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO publish_runs
		(version_seq,trigger_by,state,base_hash,errors_json,diff_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`, nullSeq(r.VersionSeq), r.TriggerBy, r.State, r.BaseHash,
		r.ErrorsJSON, r.DiffJSON, r.CreatedAt.Format(time.RFC3339Nano), r.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdatePublishRun updates the mutable state and payload of a run.
func (s *Store) UpdatePublishRun(ctx context.Context, r PublishRun) error {
	if r.ID <= 0 {
		return errors.New("publish run id must be positive")
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE publish_runs SET
		version_seq=?, trigger_by=?, state=?, base_hash=?, errors_json=?, diff_json=?, updated_at=?
		WHERE id=?`, nullSeq(r.VersionSeq), r.TriggerBy, r.State, r.BaseHash, r.ErrorsJSON,
		r.DiffJSON, r.UpdatedAt.Format(time.RFC3339Nano), r.ID)
	return err
}

// GetPublishRun loads a run by id.
func (s *Store) GetPublishRun(ctx context.Context, id int64) (PublishRun, error) {
	var r PublishRun
	var version sql.NullInt64
	var created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id,version_seq,trigger_by,state,base_hash,
		errors_json,diff_json,created_at,updated_at FROM publish_runs WHERE id=?`, id).Scan(
		&r.ID, &version, &r.TriggerBy, &r.State, &r.BaseHash, &r.ErrorsJSON, &r.DiffJSON,
		&created, &updated)
	if err != nil {
		return PublishRun{}, err
	}
	if version.Valid {
		r.VersionSeq = version.Int64
	}
	var parseErr error
	r.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created)
	if parseErr != nil {
		return PublishRun{}, parseErr
	}
	r.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updated)
	if parseErr != nil {
		return PublishRun{}, parseErr
	}
	return r, nil
}

// ActivePublishRuns returns all non-terminal publish runs.
func (s *Store) ActivePublishRuns(ctx context.Context) ([]PublishRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,version_seq,trigger_by,state,base_hash,
		errors_json,diff_json,created_at,updated_at FROM publish_runs
		WHERE state IN ('VALIDATING','VALIDATED','PUBLISHING','CONFIRMING')
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []PublishRun
	for rows.Next() {
		var r PublishRun
		var version sql.NullInt64
		var created, updated string
		if err := rows.Scan(&r.ID, &version, &r.TriggerBy, &r.State, &r.BaseHash,
			&r.ErrorsJSON, &r.DiffJSON, &created, &updated); err != nil {
			return nil, err
		}
		if version.Valid {
			r.VersionSeq = version.Int64
		}
		var parseErr error
		r.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created)
		if parseErr != nil {
			return nil, parseErr
		}
		r.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updated)
		if parseErr != nil {
			return nil, parseErr
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func nullSeq(seq int64) any {
	if seq == 0 {
		return nil
	}
	return seq
}
