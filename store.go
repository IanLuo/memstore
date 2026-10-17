// Package memstore is a portable, local memory store: one SQLite file holds
// every memory, and search finds memories by their words.
package memstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Memory is one row of the memory table, the store's only source of truth.
// Id..Hash plus the provenance columns Origin..FsNode are content, written
// once; Status, DeletedAt and Tier are lifecycle columns and are the only
// fields Put may change later. Related is not a column at all: Search fills
// it, from the edge table, when the row is a search hit; the other verbs
// leave it nil.
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

	// Provenance — where this row came from. Put writes what the caller
	// supplies, defaulting an empty Origin to 'native'; Import fills the
	// imported shape. An empty OriginId, Ref or FsNode is stored as NULL.
	Origin   string
	OriginId string
	Ref      string
	FsNode   string

	Related []Related
}

// Related is one memory another memory points at: the target of an outgoing
// superseded-by or found-by edge. Search surfaces these on a hit so a found
// memory shows where it sits; the supersedes column is carried separately, on
// Supersedes.
type Related struct {
	Rel string
	To  int64
}

// Store is an open memory file.
type Store struct {
	db   *sql.DB
	path string

	// matchers ranks and fuses reads over this store; it holds the optional
	// embedder behind vector recall. dim is the vector width the embedder is
	// pinned to; 0 until it is known.
	matchers Matchers
	dim      int
}

// schemaVersion is the schema_version row's current value: 5, the porter
// tokenizer on the FTS index over the v3 format (unix-nanosecond timestamps
// plus the slice-3 provenance columns) with the (origin, origin_id) unique
// index over those columns. Version 4 (same format, no unique index), version 3
// (default tokenizer) and version 2 (no provenance) are upgraded on Open,
// through the same path that creates the index and rebuilds record_fts;
// version 1 (unix seconds, written by the first slice release, 5aa287a) is
// refused, never misread; a file stamped with anything else is refused on
// Open, never silently migrated or misread. Keep the INSERT literal in schema
// equal to this; TestSchemaVersion pins both.
const schemaVersion = 5

// busyTimeout is how long a statement waits for the file's write lock before
// it fails. It is named once because two places set the same bound — the
// connection's busy_timeout, which SQLite applies to every statement, and
// switchToWAL's retry loop, which exists because SQLite does not apply the
// handler to a journal-mode change. Written twice, they could drift.
const busyTimeout = 5 * time.Second

// schema is the whole database. record_fts is FTS5 external content over
// memory (title, text), tokenized by the porter stemmer so a query reaches a
// stored word's inflections ("gate" reaches "gates"): the text is indexed in
// place, never copied. Only an INSERT trigger exists, because content is
// immutable; status, deleted_at and tier may change without touching the
// index. The tokenizer is baked into the table at CREATE, so a file written
// before v4 has its index rebuilt on open under the schema_version bump (see
// Open). edge is the thin relation table of slice 2: enough to store an
// arbitrary relation between two memories and walk it, nothing more.
const schema = `
CREATE TABLE IF NOT EXISTS edge (
	from_id INTEGER NOT NULL REFERENCES memory(id),
	rel     TEXT    NOT NULL,
	to_id   INTEGER NOT NULL REFERENCES memory(id),
	ts      INTEGER NOT NULL,
	PRIMARY KEY (from_id, rel, to_id)
);
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
	hash       TEXT NOT NULL,
	origin     TEXT NOT NULL DEFAULT 'native',
	origin_id  TEXT,
	ref        TEXT,
	fs_node    TEXT
);

CREATE VIRTUAL TABLE IF NOT EXISTS record_fts USING fts5(
	title, text,
	content = 'memory',
	content_rowid = 'id',
	tokenize = 'porter'
);

CREATE TRIGGER IF NOT EXISTS memory_ai AFTER INSERT ON memory BEGIN
	INSERT INTO record_fts(rowid, title, text) VALUES (new.id, new.title, new.text);
END;

CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);
INSERT INTO schema_version(version)
	SELECT 5 WHERE NOT EXISTS (SELECT 1 FROM schema_version);
`

// vecSchema is record_vec, the optional projection behind the vector matcher.
// Only SetEmbedder creates it, so a store with no embedder never has it, and
// it adds no schema_version: vectors are an enablement, not a format. A row
// absent from record_vec is simply unembedded; the matcher skips it.
const vecSchema = `
CREATE TABLE IF NOT EXISTS record_vec (
	id     INTEGER PRIMARY KEY REFERENCES memory(id),
	vector BLOB    NOT NULL
);
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
	// The DSN is a file: URI with the path escaped, so that a '?' or '#' in it
	// is data rather than the start of the parameters: the driver ends the
	// filename at the first '?', and a concatenated DSN wrote a different file
	// than the caller named while Open reported success. OmitHost keeps a
	// relative path from being read as the URI's authority.
	// _txlock=immediate makes every transaction take the write lock at BEGIN.
	// A deferred transaction that reads before it writes is refused with
	// SQLITE_BUSY when another writer got there first, and SQLite does not run
	// the busy timeout for that upgrade, so the timeout alone would not make
	// writers queue.
	dsn := (&url.URL{Scheme: "file", Path: path, OmitHost: true, RawQuery: "_txlock=immediate"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("memstore: open %s: %w", path, err)
	}
	// One writer by design. One connection serializes callers inside this
	// process; the busy timeout makes a caller in another process wait for the
	// lock instead of failing, which SetMaxOpenConns cannot do — the CLI is a
	// new process per command.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA busy_timeout = %d;`, busyTimeout.Milliseconds())); err != nil {
		db.Close()
		return nil, fmt.Errorf("memstore: configure %s: %w", path, err)
	}
	// Refuse a file stamped with a schema_version we do not expect before
	// anything touches it — no WAL, no schema write, no chmod. The recognized
	// versions are 2, 3 and 4 (previous formats, upgraded below) and the current
	// one; a version-1 file stores ts in seconds, not nanoseconds, and reading
	// it as current would silently show 1970.
	v := 0
	var stamped int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_version'`).Scan(&stamped); err != nil {
		db.Close()
		return nil, fmt.Errorf("memstore: read %s: %w", path, err)
	}
	if stamped == 1 {
		if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&v); err != nil {
			db.Close()
			return nil, fmt.Errorf("memstore: read schema_version from %s: %w", path, err)
		}
		if v != 2 && v != 3 && v != 4 && v != schemaVersion {
			db.Close()
			return nil, fmt.Errorf("memstore: open %s: schema_version is %d, want %d (porter-tokenized FTS index)", path, v, schemaVersion)
		}
	}
	// journal_mode is stored in the file, so switch to WAL only when it is not
	// already WAL. synchronous is per connection and is set on every open.
	// Neither is a schema write, so a file already at the current version takes
	// no write lock on open — otherwise a reader racing a writer fails.
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode;`).Scan(&mode); err != nil {
		db.Close()
		return nil, fmt.Errorf("memstore: configure %s: %w", path, err)
	}
	if !strings.EqualFold(mode, "wal") {
		if err := switchToWAL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("memstore: configure %s: %w", path, err)
		}
	}
	if _, err := db.Exec(`PRAGMA synchronous = FULL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("memstore: configure %s: %w", path, err)
	}
	// A file already at the current version has its whole schema; applying it
	// again would take the write lock on every open for nothing. Only a fresh or
	// older file is brought to the current shape.
	if v != schemaVersion {
		// The schema and the upgrade that follows it are the one transaction,
		// and the version is re-read inside it. Begin is IMMEDIATE (the DSN's
		// _txlock), so the write lock is held from here: a second opener that
		// read an old version before the winner committed waits on that lock,
		// re-reads the current version and does nothing — without the re-read it
		// replayed the rebuild outside any transaction and died on its own
		// CREATE TRIGGER memory_ai already exists. One transaction also makes
		// schema_version's table and its row visible together, so a concurrent
		// opener never reads the table before its row exists — the half state
		// that failed it with "sql: no rows in result set".
		tx, err := db.Begin()
		if err != nil {
			db.Close()
			return nil, fmt.Errorf("memstore: apply schema to %s: %w", path, err)
		}
		// Re-read under the lock: v was read before Begin, so it is stale if
		// another opener reached the current version first. A file whose
		// schema_version table is present and current needs nothing — the
		// second opener does not replay the upgrade; a fresh file (no table)
		// still gets the schema and the fs_node index.
		var had int
		if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_version'`).Scan(&had); err != nil {
			tx.Rollback()
			db.Close()
			return nil, fmt.Errorf("memstore: read %s: %w", path, err)
		}
		cur := 0
		if had == 1 {
			if err := tx.QueryRow(`SELECT version FROM schema_version`).Scan(&cur); err != nil {
				tx.Rollback()
				db.Close()
				return nil, fmt.Errorf("memstore: read schema_version from %s: %w", path, err)
			}
		}
		if had == 0 || cur != schemaVersion {
			if _, err := tx.Exec(schema); err != nil {
				tx.Rollback()
				db.Close()
				return nil, fmt.Errorf("memstore: apply schema to %s: %w", path, err)
			}
			// A file stamped schema_version 2 predates the provenance columns;
			// a fresh file gets them from the schema just applied. The upgrade
			// is additive, guarded step by step by what already exists: the
			// missing columns are added with their v3 definition, the fs_node
			// index is created, and the version row moves to 5. Existing rows
			// are untouched — their origin picks up the column's default
			// 'native'.
			for _, add := range []struct{ col, ddl string }{
				{"origin", "TEXT NOT NULL DEFAULT 'native'"},
				{"origin_id", "TEXT"},
				{"ref", "TEXT"},
				{"fs_node", "TEXT"},
			} {
				var have int
				if err := tx.QueryRow(`SELECT count(*) FROM pragma_table_info('memory') WHERE name = ?`, add.col).Scan(&have); err != nil {
					tx.Rollback()
					db.Close()
					return nil, fmt.Errorf("memstore: upgrade %s: %w", path, err)
				}
				if have == 0 {
					if _, err := tx.Exec(`ALTER TABLE memory ADD COLUMN ` + add.col + ` ` + add.ddl); err != nil {
						tx.Rollback()
						db.Close()
						return nil, fmt.Errorf("memstore: upgrade %s: %w", path, err)
					}
				}
			}
			if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_memory_fs_node ON memory(fs_node)`); err != nil {
				tx.Rollback()
				db.Close()
				return nil, fmt.Errorf("memstore: upgrade %s: %w", path, err)
			}
			// The partial unique index is what makes a re-put idempotent: a
			// duplicate (origin, origin_id) is refused at the INSERT, and two
			// rows cannot coexist under a race. It is partial because empty
			// provenance is stored as NULL, and NULL never collides. The index
			// is built over the rows a file already carries, so a file holding
			// pre-existing duplicates fails this open loudly, naming the index;
			// skipping it would leave that file without the guarantee.
			if _, err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_origin_origin_id ON memory(origin, origin_id) WHERE origin_id IS NOT NULL`); err != nil {
				tx.Rollback()
				db.Close()
				return nil, fmt.Errorf("memstore: upgrade %s: create unique index idx_memory_origin_origin_id: %w", path, err)
			}
			// A file stamped 2 or 3 carries record_fts built with the default
			// tokenizer, and FTS5 bakes the tokenizer into the table at CREATE.
			// Drop and recreate the index with the porter stemmer, then rebuild
			// it from memory: it is external content, so the rebuild re-derives
			// the index from the stored text, never recopies it. The trigger is
			// recreated with the table it writes; IF NOT EXISTS matches const
			// schema, so the rebuild stays idempotent even if it is re-entered.
			if cur == 2 || cur == 3 {
				for _, stmt := range []string{
					`DROP TRIGGER IF EXISTS memory_ai`,
					`DROP TABLE IF EXISTS record_fts`,
					`CREATE VIRTUAL TABLE record_fts USING fts5(
						title, text,
						content = 'memory',
						content_rowid = 'id',
						tokenize = 'porter'
					)`,
					`CREATE TRIGGER IF NOT EXISTS memory_ai AFTER INSERT ON memory BEGIN
						INSERT INTO record_fts(rowid, title, text) VALUES (new.id, new.title, new.text);
					END`,
					`INSERT INTO record_fts(record_fts) VALUES ('rebuild')`,
				} {
					if _, err := tx.Exec(stmt); err != nil {
						tx.Rollback()
						db.Close()
						return nil, fmt.Errorf("memstore: rebuild record_fts in %s: %w", path, err)
					}
				}
			}
			if _, err := tx.Exec(`UPDATE schema_version SET version = ? WHERE version IN (2, 3, 4)`, schemaVersion); err != nil {
				tx.Rollback()
				db.Close()
				return nil, fmt.Errorf("memstore: upgrade %s: %w", path, err)
			}
		}
		if err := tx.Commit(); err != nil {
			db.Close()
			return nil, fmt.Errorf("memstore: apply schema to %s: %w", path, err)
		}
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
	s := &Store{db: db, path: path}
	// The matchers read through the store's own read handle; the store
	// implements matchReader without exposing its write side.
	s.matchers.reader = s
	return s, nil
}

// switchToWAL puts db's file in WAL mode. The switch takes the file's
// exclusive lock, and SQLite does not run the busy timeout for a journal-mode
// change, so a second opener racing a fresh file is refused with SQLITE_BUSY
// instead of waiting. The refusal is not a failure: another opener is making
// the same switch, the mode is stored in the file, and a retry succeeds once
// that switch has released the lock. Bounded by the same 5 s the busy timeout
// gives every other statement. A refusal that is not SQLITE_BUSY is returned
// at once, so an unwritable file still fails open fast.
func switchToWAL(db *sql.DB) error {
	deadline := time.Now().Add(busyTimeout)
	for {
		_, err := db.Exec(`PRAGMA journal_mode = WAL;`)
		if err == nil {
			return nil
		}
		var se *sqlite.Error
		if !errors.As(err, &se) || (se.Code()&0xff) != sqlite3.SQLITE_BUSY || time.Now().After(deadline) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
}

// tsMin and tsMax are the true bounds of the unix-nanosecond range time.Time
// can represent, inclusive (time.Unix(0, math.MinInt64) and MaxInt64)
// rendered in UTC. UnixNano would wrap silently outside these — a wrong
// value, not an error — so Put refuses before storing.
var (
	tsMin = time.Date(1677, 9, 21, 0, 12, 43, 145224192, time.UTC)
	tsMax = time.Date(2262, 4, 11, 23, 47, 16, 854775807, time.UTC)
)

// Path is the file this store was opened on.
func (s *Store) Path() string { return s.path }

// Close closes the underlying file.
func (s *Store) Close() error { return s.db.Close() }

// readRows runs one read statement for the matchers. It is Store's half of
// matchReader: the matchers query the store's tables through it and hold no
// write handle, because Store keeps the write methods itself.
func (s *Store) readRows(query string, args ...any) (*sql.Rows, error) {
	return s.db.Query(query, args...)
}

// ErrDuplicateOrigin is returned by Put when another memory already carries
// the same (origin, origin_id) pair; errors.Is matches it. The check is the
// partial unique index, so it holds under a race as well as in sequence. A pair
// whose origin_id is empty is never compared: empty provenance is stored as
// NULL, and the index skips NULL.
var ErrDuplicateOrigin = errors.New("memstore: put: (origin, origin_id) already exists")

// Put appends one memory and returns its id. Text and kind are required; an
// explicit nonzero id is honoured only if it is free. Content is never
// updated, so a second Put of an existing id is refused. A caller-supplied
// DeletedAt is refused: deletion is forget's job. Provenance is content: the
// four fields are written from the struct, an empty Origin becomes 'native',
// and an empty OriginId, Ref or FsNode is stored as NULL. A row that already
// carries the same (origin, origin_id) is refused with ErrDuplicateOrigin, so
// a caller projecting an event log decides whether a re-run is a skip.
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
	origin := m.Origin
	if origin == "" {
		origin = "native"
	}
	sum := sha256.Sum256([]byte(m.Text))

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("memstore: put: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`
		INSERT INTO memory
			(id, ts, kind, scope, title, text, extra, importance, tier, status,
			 deleted_at, parent, supersedes, found_by, hash,
			 origin, origin_id, ref, fs_node)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableId(m.Id), ts.UnixNano(), m.Kind, m.Scope, m.Title, m.Text, m.Extra,
		m.Importance, m.Tier, status, nil, m.Parent,
		m.Supersedes, m.FoundBy, hex.EncodeToString(sum[:]),
		origin, nullableText(m.OriginId), nullableText(m.Ref), nullableText(m.FsNode))
	if err != nil {
		if isDuplicateOrigin(err) {
			return 0, fmt.Errorf("memstore: put: %w", ErrDuplicateOrigin)
		}
		return 0, fmt.Errorf("memstore: put: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("memstore: put: %w", err)
	}
	// With an embedder configured the text is embedded in the same
	// transaction, so an embedding failure leaves no memory behind.
	if s.matchers.embedder != nil {
		v, err := s.matchers.embedder(m.Text)
		if err != nil {
			return 0, fmt.Errorf("memstore: put: embed: %w", err)
		}
		if err := s.storeVector(tx, id, v); err != nil {
			return 0, fmt.Errorf("memstore: put: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("memstore: put: %w", err)
	}
	return id, nil
}

// isDuplicateOrigin reports whether err is the provenance index's refusal of a
// duplicate (origin, origin_id). The constraint's message names the columns,
// which is what separates it from the primary-key refusal of an explicit id.
func isDuplicateOrigin(err error) bool {
	var se *sqlite.Error
	if !errors.As(err, &se) || (se.Code()&0xff) != sqlite3.SQLITE_CONSTRAINT {
		return false
	}
	return strings.Contains(se.Error(), "memory.origin, memory.origin_id")
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

// Search returns up to k memories matching q, best first. Matching is the
// store's Matchers: lexical (FTS5 bm25), vector (cosine over record_vec, only
// when an embedder is configured), their reciprocal-rank fusion, and the
// relation walk that fills each hit's Related. Soft-deleted rows are excluded
// from both matchers; no match is an empty result, not an error. The store
// owns persistence and delegates the ranking.
func (s *Store) Search(q string, k int) ([]Memory, error) {
	out, err := s.matchers.Search(q, k)
	if err != nil {
		return nil, fmt.Errorf("memstore: search: %w", err)
	}
	return out, nil
}

// SetEmbedder registers the embedder behind vector recall. It is a post-Open
// setter: a store with no embedder never creates record_vec and searches
// lexically. The first call pins the dimension — from a vector the file
// already carries, or from the first text embedded — and creates record_vec
// if it is missing; a later call is refused, so the dimension cannot change
// under rows already stored. A nil embedder is refused. Rows written before
// the call stay unembedded; the vector matcher skips them.
func (s *Store) SetEmbedder(embed func(text string) ([]float32, error)) error {
	if embed == nil {
		return errors.New("memstore: SetEmbedder: nil embedder")
	}
	if s.matchers.embedder != nil {
		return errors.New("memstore: SetEmbedder: embedder already set; the first call pins the dimension")
	}
	if _, err := s.db.Exec(vecSchema); err != nil {
		return fmt.Errorf("memstore: SetEmbedder: %w", err)
	}
	// A file that already carries vectors pins their width, so an embedder of
	// another width cannot be mixed into the same table.
	var n int
	err := s.db.QueryRow(`SELECT length(vector) FROM record_vec LIMIT 1`).Scan(&n)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("memstore: SetEmbedder: %w", err)
	}
	s.matchers.embedder = embed
	if n > 0 && n%4 == 0 {
		s.dim = n / 4
	}
	return nil
}

// storeVector writes one row's vector, pinned to the store's dimension. The
// first vector seen pins the dimension; a later vector of another width is
// refused, never truncated.
func (s *Store) storeVector(tx *sql.Tx, id int64, v []float32) error {
	if len(v) == 0 {
		return errors.New("embedder returned an empty vector")
	}
	if s.dim == 0 {
		s.dim = len(v)
	} else if len(v) != s.dim {
		return fmt.Errorf("embedder returned a %d-dimension vector, want %d", len(v), s.dim)
	}
	if _, err := tx.Exec(`INSERT INTO record_vec (id, vector) VALUES (?, ?)`, id, vecToBytes(v)); err != nil {
		return err
	}
	return nil
}

// vecToBytes carries a float32 vector to the little-endian bytes record_vec
// stores.
func vecToBytes(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// CloseMemory retires one memory: status becomes closed, and Close is the
// only writer of that state. With a replacement, supersedes and the
// superseded-by edge are recorded in the same transaction. Closing a row that
// is already closed is a no-op — the first close owns the state; unknown and
// forgotten ids are errors.
func (s *Store) CloseMemory(id int64, replacedBy *int64) error {
	if err := s.requireLive(id); err != nil {
		return fmt.Errorf("memstore: close: %w", err)
	}
	if replacedBy != nil {
		if err := s.requireLive(*replacedBy); err != nil {
			return fmt.Errorf("memstore: close: replacement: %w", err)
		}
	}
	var status string
	if err := s.db.QueryRow(`SELECT status FROM memory WHERE id = ?`, id).Scan(&status); err != nil {
		return fmt.Errorf("memstore: close: %w", err)
	}
	if status == "closed" {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("memstore: close: %w", err)
	}
	defer tx.Rollback()
	if replacedBy != nil {
		if _, err := tx.Exec(`UPDATE memory SET status = 'closed', supersedes = ? WHERE id = ?`, *replacedBy, id); err != nil {
			return fmt.Errorf("memstore: close: %w", err)
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO edge (from_id, rel, to_id, ts) VALUES (?, 'superseded-by', ?, ?)`, id, *replacedBy, time.Now().UnixNano()); err != nil {
			return fmt.Errorf("memstore: close: %w", err)
		}
	} else {
		if _, err := tx.Exec(`UPDATE memory SET status = 'closed' WHERE id = ?`, id); err != nil {
			return fmt.Errorf("memstore: close: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memstore: close: %w", err)
	}
	return nil
}

// Forget softly deletes one memory: deleted_at is set, and the row is hidden
// from Get, Search, Recent and Stats' open count while its bytes remain. No
// verb destroys bytes and none restores — this slice has no un-forget. An
// unknown or already-forgotten id is an error.
func (s *Store) Forget(id int64) error {
	res, err := s.db.Exec(`UPDATE memory SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, time.Now().UnixNano(), id)
	if err != nil {
		return fmt.Errorf("memstore: forget: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("memstore: forget: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("memstore: forget: no live memory %d", id)
	}
	return nil
}

// Recent returns up to k memories by time — newest ts first — optionally
// restricted to one scope. Forgotten rows are excluded and k <= 0 is an empty
// result. It never ranks by meaning; that is Search's job.
func (s *Store) Recent(scope string, k int) ([]Memory, error) {
	if k <= 0 {
		return nil, nil
	}
	query := `SELECT ` + memoryCols + ` FROM memory WHERE deleted_at IS NULL`
	args := []any{}
	if scope != "" {
		query += ` AND scope = ?`
		args = append(args, scope)
	}
	query += ` ORDER BY ts DESC, id DESC LIMIT ?`
	args = append(args, k)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("memstore: recent: %w", err)
	}
	defer rows.Close()

	out, err := scanAll(rows)
	if err != nil {
		return nil, fmt.Errorf("memstore: recent: %w", err)
	}
	return out, nil
}

// ImportOptions carries the knobs an import can be turned with — none today;
// the parameter exists so a later option (a scope filter, a stop-at-ts) is
// additive and does not change callers.
type ImportOptions struct{}

// Import copies every event of an fs-style store (a database with an events
// table) into this one, in one transaction: one memory row per event, with
// the kind mapping and the provenance columns filled. It is one-way — a row
// that already carries (origin, origin_id) is skipped, never re-created or
// updated, and nothing else in this store is touched. A source it cannot read
// or an event it cannot map fails the whole import: the transaction rolls
// back and nothing is written. It returns the number of events imported and
// the number skipped as already present.
//
// The source is never opened in place: SQLite writes -shm/-wal sidecars
// beside any WAL-mode database it opens, read-only mode included, so opening
// ~/.fs/store.db would write into the other store's directory. Import
// snapshots the file (and its -wal, when hot) to a temp copy and reads that.
func (s *Store) Import(srcStorePath string, _ ImportOptions) (imported, skipped int, err error) {
	dir, err := os.MkdirTemp("", "memstore-import-")
	if err != nil {
		return 0, 0, fmt.Errorf("memstore: import from %s: %w", srcStorePath, err)
	}
	defer os.RemoveAll(dir)
	srcPath := filepath.Join(dir, "source.db")
	if err := copyFile(srcPath, srcStorePath); err != nil {
		return 0, 0, fmt.Errorf("memstore: import from %s: %w", srcStorePath, err)
	}
	if _, err := os.Stat(srcStorePath + "-wal"); err == nil {
		if err := copyFile(srcPath+"-wal", srcStorePath+"-wal"); err != nil {
			return 0, 0, fmt.Errorf("memstore: import from %s: %w", srcStorePath, err)
		}
	}

	src, err := sql.Open("sqlite", srcPath)
	if err != nil {
		return 0, 0, fmt.Errorf("memstore: import from %s: %w", srcStorePath, err)
	}
	defer src.Close()
	src.SetMaxOpenConns(1)

	// nodeID maps each fs node to the local id of its earliest imported row —
	// rows of earlier runs included, so a child of a node imported before still
	// resolves (import is resumable). Native rows can never be parents: their
	// fs_node is NULL, and the map reads only origin = 'imported'.
	nodeID := map[string]int64{}
	{
		have, err := s.db.Query(`SELECT fs_node, id FROM memory WHERE origin = 'imported' AND fs_node IS NOT NULL ORDER BY ts, id`)
		if err != nil {
			return 0, 0, fmt.Errorf("memstore: import: %w", err)
		}
		for have.Next() {
			var node string
			var id int64
			if err := have.Scan(&node, &id); err != nil {
				have.Close()
				return 0, 0, fmt.Errorf("memstore: import: %w", err)
			}
			if _, ok := nodeID[node]; !ok {
				nodeID[node] = id
			}
		}
		have.Close()
		if err := have.Err(); err != nil {
			return 0, 0, fmt.Errorf("memstore: import: %w", err)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("memstore: import: %w", err)
	}
	defer tx.Rollback()

	// Events in timestamp order, so within a node the first row seen is the
	// earliest — the row a parent link resolves to.
	events, err := src.Query(`SELECT id, timestamp, project_id, node_id, parent_node_id, type, payload, commit_sha
		FROM events ORDER BY timestamp, id`)
	if err != nil {
		return 0, 0, fmt.Errorf("memstore: import from %s: %w", srcStorePath, err)
	}
	defer events.Close()

	type pendingParent struct {
		id     int64
		parent string
	}
	var pend []pendingParent

	for events.Next() {
		var (
			eventID, ts, project, eventType, payload string
			node, parentNode, commit                 sql.NullString
		)
		if err := events.Scan(&eventID, &ts, &project, &node, &parentNode, &eventType, &payload, &commit); err != nil {
			return 0, 0, fmt.Errorf("memstore: import from %s: %w", srcStorePath, err)
		}
		var already bool
		if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM memory WHERE origin = 'imported' AND origin_id = ?)`, eventID).Scan(&already); err != nil {
			return 0, 0, fmt.Errorf("memstore: import: %w", err)
		}
		if already {
			skipped++
			continue
		}

		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return 0, 0, fmt.Errorf("memstore: import from %s: event %s: %w", srcStorePath, eventID, err)
		}
		if t.Before(tsMin) || t.After(tsMax) {
			return 0, 0, fmt.Errorf("memstore: import from %s: event %s: ts %s outside %s..%s", srcStorePath, eventID, t, tsMin, tsMax)
		}
		kind := fsTypeToKind[eventType]
		if kind == "" {
			kind = eventType
		}
		title, text, extra := importPayload(payload, eventType)
		sum := sha256.Sum256([]byte(text))
		res, err := tx.Exec(`
			INSERT INTO memory
				(id, ts, kind, scope, title, text, extra, importance, tier, status,
				 deleted_at, parent, supersedes, found_by, hash,
				 origin, origin_id, ref, fs_node)
			VALUES (?, ?, ?, ?, ?, ?, ?, 0, '', 'open', NULL, NULL, NULL, '', ?, ?, ?, ?, ?)`,
			nil, t.UnixNano(), kind, project, title, text, extra,
			hex.EncodeToString(sum[:]), "imported", eventID, commit, node)
		if err != nil {
			return 0, 0, fmt.Errorf("memstore: import from %s: event %s: %w", srcStorePath, eventID, err)
		}
		local, err := res.LastInsertId()
		if err != nil {
			return 0, 0, fmt.Errorf("memstore: import from %s: event %s: %w", srcStorePath, eventID, err)
		}
		// With an embedder configured the row embeds in this same transaction;
		// an embedding failure rolls the whole import back.
		if s.matchers.embedder != nil {
			v, err := s.matchers.embedder(text)
			if err != nil {
				return 0, 0, fmt.Errorf("memstore: import from %s: event %s: embed: %w", srcStorePath, eventID, err)
			}
			if err := s.storeVector(tx, local, v); err != nil {
				return 0, 0, fmt.Errorf("memstore: import from %s: event %s: %w", srcStorePath, eventID, err)
			}
		}
		if node.Valid && node.String != "" {
			if _, ok := nodeID[node.String]; !ok {
				nodeID[node.String] = local
			}
		}
		if parentNode.Valid && parentNode.String != "" {
			pend = append(pend, pendingParent{local, parentNode.String})
		}
		imported++
	}
	if err := events.Err(); err != nil {
		return 0, 0, fmt.Errorf("memstore: import from %s: %w", srcStorePath, err)
	}

	// Parents resolve after every insert, so a parent later in the file links;
	// a parent_node_id that names no imported row (now or in an earlier run)
	// stays NULL and does not fail the import.
	for _, p := range pend {
		pid, ok := nodeID[p.parent]
		if !ok {
			continue
		}
		if _, err := tx.Exec(`UPDATE memory SET parent = ? WHERE id = ?`, pid, p.id); err != nil {
			return 0, 0, fmt.Errorf("memstore: import: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("memstore: import: %w", err)
	}
	return imported, skipped, nil
}

// fsTypeToKind maps the fs event types the card names to memory kinds; a type
// the table does not name keeps its fs name (kinds are open labelled).
var fsTypeToKind = map[string]string{
	"decision-recorded": "decision",
	"knowledge-added":   "knowledge",
	"task-created":      "task",
	"report-recorded":   "report",
	"plan-recorded":     "plan",
	"delivery-recorded": "event",
	"status-changed":    "event",
}

// payloadTextOrder is the order title and text take the payload's text-ish
// field in: first present wins.
var payloadTextOrder = []string{"goal", "summary", "line", "body"}

// importPayload splits an event's payload into title, text and extra. title
// and text both take the first present text-ish field (trimmed); with none,
// title falls back to the event type and text to the raw payload. extra is
// whatever of the payload remains once the taken field is set aside — a
// payload whose text is the whole payload has no remainder.
func importPayload(payload, eventType string) (title, text, extra string) {
	raw := strings.TrimSpace(payload)
	var obj map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &obj) == nil {
		for _, k := range payloadTextOrder {
			rawv, ok := obj[k]
			if !ok {
				continue
			}
			var v string
			if err := json.Unmarshal(rawv, &v); err != nil {
				continue
			}
			if v = strings.TrimSpace(v); v == "" {
				continue
			}
			title, text = v, v
			delete(obj, k)
			if len(obj) > 0 {
				if rest, err := json.Marshal(obj); err == nil {
					extra = string(rest)
				}
			}
			return title, text, extra
		}
	}
	return eventType, raw, ""
}

// copyFile copies one file's bytes to another; a copy that fails halfway
// leaves no destination behind.
func copyFile(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// scanAll reads every Memory a query's rows carry; the caller closes rows.
func scanAll(rows *sql.Rows) ([]Memory, error) {
	var out []Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Link records one edge from the memory from to the memory to, labelled with
// the open text rel. Both ends must be live memory rows; a repeated identical
// Link is a no-op. The walk surfaces the superseded-by and found-by relations
// on Search hits; every other rel is stored but not surfaced.
func (s *Store) Link(from int64, rel string, to int64) error {
	if err := s.requireLive(from); err != nil {
		return fmt.Errorf("memstore: link: %w", err)
	}
	if err := s.requireLive(to); err != nil {
		return fmt.Errorf("memstore: link: %w", err)
	}
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO edge (from_id, rel, to_id, ts) VALUES (?, ?, ?, ?)`, from, rel, to, time.Now().UnixNano()); err != nil {
		return fmt.Errorf("memstore: link: %w", err)
	}
	return nil
}

// Stats is the store's own shape: row counts over all rows, the db file's
// byte size (and the WAL's, while it exists), and the newest and oldest ts
// present. The ts bounds span hidden rows too, because their bytes are still
// in the file.
type Stats struct {
	Total     int64
	Open      int64
	Forgotten int64
	Bytes     int64
	WalBytes  int64
	Newest    *time.Time
	Oldest    *time.Time

	// VectorRows is how many rows carry a vector; nil when no embedder is
	// configured, so the coverage (VectorRows/Total) is absent, not zero.
	VectorRows *int64 `json:"VectorRows,omitempty"`
}

// Stats returns the store's shape; it writes nothing.
func (s *Store) Stats() (Stats, error) {
	var st Stats
	fi, err := os.Stat(s.path)
	if err != nil {
		return st, fmt.Errorf("memstore: stats: %w", err)
	}
	st.Bytes = fi.Size()
	if wfi, err := os.Stat(s.path + "-wal"); err == nil {
		st.WalBytes = wfi.Size()
	}
	if err := s.db.QueryRow(`SELECT count(*), count(deleted_at) FROM memory`).Scan(&st.Total, &st.Forgotten); err != nil {
		return st, fmt.Errorf("memstore: stats: %w", err)
	}
	st.Open = st.Total - st.Forgotten
	var newest, oldest sql.NullInt64
	if err := s.db.QueryRow(`SELECT max(ts), min(ts) FROM memory`).Scan(&newest, &oldest); err != nil {
		return st, fmt.Errorf("memstore: stats: %w", err)
	}
	st.Newest, st.Oldest = unixTime(newest), unixTime(oldest)
	if s.matchers.embedder != nil {
		var n int64
		if err := s.db.QueryRow(`SELECT count(*) FROM record_vec`).Scan(&n); err != nil {
			return st, fmt.Errorf("memstore: stats: %w", err)
		}
		st.VectorRows = &n
	}
	return st, nil
}

// requireLive succeeds only when id names a row the verbs still show: present
// and not forgotten. A forgotten row hides from the whole surface, "as if
// missing".
func (s *Store) requireLive(id int64) error {
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM memory WHERE id = ? AND deleted_at IS NULL`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("no live memory %d", id)
	}
	return nil
}

// unixTime turns a stored nanosecond timestamp into a UTC time; a NULL (there
// are no rows) is nil.
func unixTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.Unix(0, v.Int64).UTC()
	return &t
}

// memoryCols is the full column list, in the order scanMemory reads it.
const memoryCols = `memory.id, memory.ts, memory.kind, memory.scope, memory.title,
	memory.text, memory.extra, memory.importance, memory.tier, memory.status,
	memory.deleted_at, memory.parent, memory.supersedes, memory.found_by, memory.hash,
	memory.origin, memory.origin_id, memory.ref, memory.fs_node`

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
		originID   sql.NullString
		ref        sql.NullString
		fsNode     sql.NullString
	)
	if err := sc.Scan(&m.Id, &ts, &m.Kind, &m.Scope, &m.Title, &m.Text, &m.Extra,
		&m.Importance, &m.Tier, &m.Status, &deletedAt, &parent, &supersedes,
		&m.FoundBy, &m.Hash, &m.Origin, &originID, &ref, &fsNode); err != nil {
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
	m.OriginId = originID.String
	m.Ref = ref.String
	m.FsNode = fsNode.String
	return m, nil
}

// ftsQuery turns free text into an FTS5 MATCH expression: one double-quoted
// phrase per run of letters and digits, OR-joined. Quoting is what keeps a
// user's query from being read as FTS5 syntax; OR is what keeps one absent
// word from sinking a query whose other words match.
func ftsQuery(q string) string {
	var terms []string
	for _, tok := range strings.FieldsFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		terms = append(terms, `"`+tok+`"`)
	}
	return strings.Join(terms, " OR ")
}

// nullableId maps the zero id to NULL, which lets AUTOINCREMENT assign one.
func nullableId(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// nullableText maps the empty string to NULL for a provenance column. Empty
// string is a value the unique index would compare, so two rows leaving
// origin_id empty would share the key ('native', empty); NULL is what the index
// skips.
func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
