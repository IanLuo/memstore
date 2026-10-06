// Package memstore is a portable, local memory store: one SQLite file holds
// every memory, and search finds memories by their words.
package memstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

// Memory is one row of the memory table, the store's only source of truth.
// Id..Hash are content and are written once; Status, DeletedAt and Tier are
// lifecycle columns and are the only fields Put may change later.
type Memory struct {
	Id         int64
	Ts         time.Time
	Kind       string
	Scope      string
	Title      string
	Text       string
	Extra      string
	Importance float64
	Tier       string
	Status     string
	DeletedAt  *time.Time
	Parent     *int64
	Supersedes *int64
	FoundBy    string
	Hash       string
}

// Store is an open memory file.
type Store struct {
	db   *sql.DB
	path string
}

// schemaVersion is the schema_version row's current value: 2, the
// unix-nanosecond timestamp format. Version 1 (unix seconds) existed only in
// the unversioned first release; a file stamped with anything else is refused
// on Open, never silently migrated or misread. Keep the INSERT literal in
// schema equal to this; TestSchemaVersion pins both.
const schemaVersion = 2

// schema is the whole database. record_fts is FTS5 external content over
// memory (title, text): the text is indexed in place, never copied. Only an
// INSERT trigger exists, because content is immutable; status, deleted_at and
// tier may change without touching the index.
const schema = `
CREATE TABLE IF NOT EXISTS memory (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	ts         INTEGER NOT NULL,
	kind       TEXT NOT NULL,
	scope      TEXT NOT NULL DEFAULT '',
	title      TEXT NOT NULL DEFAULT '',
	text       TEXT NOT NULL,
	extra      TEXT NOT NULL DEFAULT '',
	importance REAL NOT NULL DEFAULT 0,
	tier       TEXT NOT NULL DEFAULT '',
	status     TEXT NOT NULL DEFAULT 'open',
	deleted_at INTEGER,
	parent     INTEGER,
	supersedes INTEGER,
	found_by   TEXT NOT NULL DEFAULT '',
	hash       TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS record_fts USING fts5(
	title, text,
	content = 'memory',
	content_rowid = 'id'
);

CREATE TRIGGER IF NOT EXISTS memory_ai AFTER INSERT ON memory BEGIN
	INSERT INTO record_fts(rowid, title, text) VALUES (new.id, new.title, new.text);
END;

CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);
INSERT INTO schema_version(version)
	SELECT 2 WHERE NOT EXISTS (SELECT 1 FROM schema_version);
`

// Open opens the memory file at path, creating it and its parent directory if
// needed, and applies the schema. WAL with a full sync keeps a crash from
// losing a memory that Put already reported as written.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("memstore: create %s: %w", dir, err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("memstore: open %s: %w", path, err)
	}
	// One writer by design; a single connection keeps concurrent callers from
	// racing two connections into SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	// Refuse a file stamped with a schema_version we do not expect before
	// anything touches it — no WAL, no schema write, no chmod. A version-1 file
	// stores ts in seconds, not nanoseconds, and reading it as current would
	// silently show 1970.
	var stamped int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_version'`).Scan(&stamped); err != nil {
		db.Close()
		return nil, fmt.Errorf("memstore: read %s: %w", path, err)
	}
	if stamped == 1 {
		v := 0
		if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&v); err != nil {
			db.Close()
			return nil, fmt.Errorf("memstore: read schema_version from %s: %w", path, err)
		}
		if v != schemaVersion {
			db.Close()
			return nil, fmt.Errorf("memstore: open %s: schema_version is %d, want %d (unix-nanosecond timestamps)", path, v, schemaVersion)
		}
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL; PRAGMA synchronous = FULL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("memstore: configure %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("memstore: apply schema to %s: %w", path, err)
	}
	// The store is user-only: the file and its WAL sidecars hold memory text,
	// and sqlite creates them under the umask, which may let group or other
	// read them. The parent directory is created 0700 when Open creates it; a
	// pre-existing directory is the caller's and keeps its mode. The schema
	// write above has created the sidecars, so they exist to restrict.
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(p, 0o600); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			db.Close()
			return nil, fmt.Errorf("memstore: restrict %s: %w", p, err)
		}
	}
	return &Store{db: db, path: path}, nil
}

// tsMin and tsMax bound the instants time.Time can represent as unix
// nanoseconds, inclusive. UnixNano would wrap silently outside these — a
// wrong value, not an error — so Put refuses before storing.
var (
	tsMin = time.Date(1678, 9, 21, 0, 12, 43, 145224192, time.UTC)
	tsMax = time.Date(2262, 4, 11, 23, 47, 16, 854775807, time.UTC)
)

// Path is the file this store was opened on.
func (s *Store) Path() string { return s.path }

// Close closes the underlying file.
func (s *Store) Close() error { return s.db.Close() }

// Put appends one memory and returns its id. Text and kind are required; an
// explicit nonzero id is honoured only if it is free. Content is never
// updated, so a second Put of an existing id is refused. A caller-supplied
// DeletedAt is refused: deletion is forget's job, the one writer of that state.
func (s *Store) Put(m *Memory) (int64, error) {
	if m.Text == "" {
		return 0, errors.New("memstore: put: text is empty")
	}
	if m.Kind == "" {
		return 0, errors.New("memstore: put: kind is empty")
	}
	if m.DeletedAt != nil {
		return 0, errors.New("memstore: put: deleted_at is set; deletion is forget's job")
	}
	ts := m.Ts
	if ts.IsZero() {
		ts = time.Now()
	}
	if ts.Before(tsMin) || ts.After(tsMax) {
		return 0, fmt.Errorf("memstore: put: ts %s is outside the representable range %s..%s (unix nanoseconds)", ts, tsMin, tsMax)
	}
	status := m.Status
	if status == "" {
		status = "open"
	}
	sum := sha256.Sum256([]byte(m.Text))

	res, err := s.db.Exec(`
		INSERT INTO memory
			(id, ts, kind, scope, title, text, extra, importance, tier, status,
			 deleted_at, parent, supersedes, found_by, hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableId(m.Id), ts.UnixNano(), m.Kind, m.Scope, m.Title, m.Text, m.Extra,
		m.Importance, m.Tier, status, nil, m.Parent,
		m.Supersedes, m.FoundBy, hex.EncodeToString(sum[:]))
	if err != nil {
		return 0, fmt.Errorf("memstore: put: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("memstore: put: %w", err)
	}
	return id, nil
}

// Get returns one memory by id. A missing id is an error, never a zero memory.
// A soft-deleted row is hidden, as if missing.
func (s *Store) Get(id int64) (Memory, error) {
	row := s.db.QueryRow(`SELECT `+memoryCols+` FROM memory WHERE id = ? AND deleted_at IS NULL`, id)
	m, err := scanMemory(row)
	if err != nil {
		return Memory{}, fmt.Errorf("memstore: get %d: %w", id, err)
	}
	return m, nil
}

// Search returns up to k memories matching q, best first. Matching is lexical
// (FTS5) in this slice; the relation walk and vectors come later. Soft-deleted
// rows are excluded. No match is an empty result, not an error.
func (s *Store) Search(q string, k int) ([]Memory, error) {
	match := ftsQuery(q)
	if k <= 0 || match == "" {
		return nil, nil
	}
	rows, err := s.db.Query(`
		SELECT `+memoryCols+`
		FROM record_fts
		JOIN memory ON memory.id = record_fts.rowid
		WHERE record_fts MATCH ?
		AND memory.deleted_at IS NULL
		ORDER BY bm25(record_fts)
		LIMIT ?`, match, k)
	if err != nil {
		return nil, fmt.Errorf("memstore: search: %w", err)
	}
	defer rows.Close()

	var out []Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, fmt.Errorf("memstore: search: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memstore: search: %w", err)
	}
	return out, nil
}

// memoryCols is the full column list, in the order scanMemory reads it.
const memoryCols = `memory.id, memory.ts, memory.kind, memory.scope, memory.title,
	memory.text, memory.extra, memory.importance, memory.tier, memory.status,
	memory.deleted_at, memory.parent, memory.supersedes, memory.found_by, memory.hash`

// scanner is satisfied by *sql.Row and *sql.Rows, so one reader serves Get and
// Search.
type scanner interface {
	Scan(dest ...any) error
}

func scanMemory(sc scanner) (Memory, error) {
	var (
		m          Memory
		ts         int64
		deletedAt  sql.NullInt64
		parent     sql.NullInt64
		supersedes sql.NullInt64
	)
	if err := sc.Scan(&m.Id, &ts, &m.Kind, &m.Scope, &m.Title, &m.Text, &m.Extra,
		&m.Importance, &m.Tier, &m.Status, &deletedAt, &parent, &supersedes,
		&m.FoundBy, &m.Hash); err != nil {
		return Memory{}, err
	}
	m.Ts = time.Unix(0, ts).UTC()
	if deletedAt.Valid {
		t := time.Unix(0, deletedAt.Int64).UTC()
		m.DeletedAt = &t
	}
	if parent.Valid {
		v := parent.Int64
		m.Parent = &v
	}
	if supersedes.Valid {
		v := supersedes.Int64
		m.Supersedes = &v
	}
	return m, nil
}

// ftsQuery turns free text into an FTS5 MATCH expression: one double-quoted
// phrase per run of letters and digits, AND-joined. Quoting is what keeps a
// user's query from being read as FTS5 syntax.
func ftsQuery(q string) string {
	var terms []string
	for _, tok := range strings.FieldsFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		terms = append(terms, `"`+tok+`"`)
	}
	return strings.Join(terms, " AND ")
}

// nullableId maps the zero id to NULL, which lets AUTOINCREMENT assign one.
func nullableId(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
