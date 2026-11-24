// Package store is the SQLite persistence layer.
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

//go:embed schema.sql
var schema string

// ErrNotFound is returned when a row doesn't exist or isn't visible to the caller.
var ErrNotFound = errors.New("not found")

// timeLayout is fixed-width on purpose: timestamps are stored as TEXT and
// ORDER BY compares strings, which only matches chronological order when
// every value has the same length. time.RFC3339Nano trims trailing zeros.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) { return time.Parse(timeLayout, s) }

// Store wraps the database handle.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens (or creates) the database at path and applies the schema.
func Open(ctx context.Context, path string, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(wal)")
	q.Add("_pragma", "synchronous(normal)")
	// Write transactions start with BEGIN IMMEDIATE, so two writers queue on
	// the busy timeout instead of failing halfway through with SQLITE_BUSY.
	q.Set("_txlock", "immediate")
	// SQLite URI filenames are percent-decoded, so escape the few characters
	// that would otherwise be read as URI syntax.
	name := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23", " ", "%20").Replace(filepath.ToSlash(path))
	dsn := "file:" + name + "?" + q.Encode()

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows one writer at a time anyway; a small pool keeps readers
	// concurrent without piling up connections.
	db.SetMaxOpenConns(8)

	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db, now: now}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for diagnostics and tests.
func (s *Store) DB() *sql.DB { return s.db }

// inTx runs fn in a transaction and commits only if it returns nil.
func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after a successful commit
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
