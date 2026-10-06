package memstore

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"math"
	"sort"
	"strings"
)

// matchReader is the read-only handle the matchers rank the store through: it
// runs one query, and it carries no Exec, no transaction and no schema method,
// so a Matchers holds no write path. *Store implements it.
type matchReader interface {
	readRows(query string, args ...any) (*sql.Rows, error)
}

// Matchers owns recall: it ranks live memories lexically (FTS5 bm25) and, when
// an embedder is configured, by vector (cosine over record_vec), fuses the two
// lists by reciprocal rank fusion, and fills each hit with the relations a
// found memory shows. It reads through matchReader and holds no write path;
// the store keeps persistence and the lifecycle verbs.
type Matchers struct {
	reader   matchReader
	embedder func(text string) ([]float32, error)
}

// Search returns up to k memories matching q, best first. Lexical (FTS5 bm25)
// and, when an embedder is configured, vector (cosine over record_vec) rank
// the live memories; the two ranked lists are fused by reciprocal rank fusion,
// and the relation walk then fills every hit's Related — the outgoing
// superseded-by and found-by edges — so a found memory shows where it sits
// (the supersedes column rides on Supersedes, as always). A query with no
// lexical token still consults the vector matcher when an embedder is
// configured. The vector matcher is fail-open on read: if the query will not
// embed, lexical still answers, with fewer results. Soft-deleted rows are
// excluded from both matchers. No match is an empty result, not an error.
func (m *Matchers) Search(q string, k int) ([]Memory, error) {
	if k <= 0 {
		return nil, nil
	}
	match := ftsQuery(q)
	var lex []int64
	if match != "" {
		var err error
		lex, err = m.lexical(match, k)
		if err != nil {
			return nil, err
		}
	}
	var vec []int64
	if m.embedder != nil {
		// Fail-open on read: a query the embedder will not take yields the
		// lexical results, never an error.
		vec, _ = m.vector(q, k)
	}
	out, err := m.memoriesByID(fuse(lex, vec, k))
	if err != nil {
		return nil, err
	}
	for i := range out {
		rel, err := m.related(out[i].Id)
		if err != nil {
			return nil, err
		}
		out[i].Related = rel
	}
	return out, nil
}

// lexical ranks up to k live memories by FTS5 bm25, best first.
func (m *Matchers) lexical(match string, k int) ([]int64, error) {
	rows, err := m.reader.readRows(`
		SELECT memory.id
		FROM record_fts
		JOIN memory ON memory.id = record_fts.rowid
		WHERE record_fts MATCH ?
		AND memory.deleted_at IS NULL
		ORDER BY bm25(record_fts)
		LIMIT ?`, match, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

// vector ranks up to k live memories by cosine against the query's embedding,
// best first. A row whose vector width differs from the query's is skipped,
// never compared across widths, and a row with no shared direction (cosine <=
// 0) is not a match. The query is embedded once per search, not once per row.
func (m *Matchers) vector(q string, k int) ([]int64, error) {
	qv, err := m.embedder(q)
	if err != nil {
		return nil, err
	}
	if len(qv) == 0 {
		return nil, errors.New("memstore: search: embedder returned an empty query vector")
	}
	rows, err := m.reader.readRows(`
		SELECT record_vec.id, record_vec.vector
		FROM record_vec
		JOIN memory ON memory.id = record_vec.id
		WHERE memory.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		id    int64
		score float64
	}
	var out []scored
	for rows.Next() {
		var id int64
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		v := vecFromBytes(blob)
		if len(v) != len(qv) {
			continue
		}
		if c := cosine(qv, v); c > 0 {
			out = append(out, scored{id, c})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].id < out[j].id
	})
	if len(out) > k {
		out = out[:k]
	}
	ids := make([]int64, len(out))
	for i, x := range out {
		ids[i] = x.id
	}
	return ids, nil
}

// rrfK is the reciprocal-rank-fusion constant: score(d) = sum 1/(rrfK+rank).
// 60 is the value the RRF paper uses; it needs no score calibration and is
// the one knob, fixed, not per-query.
const rrfK = 60

// fuse merges the lexical and vector ranked lists into one order by
// reciprocal rank fusion, at most k ids. A tie in fused score breaks to the
// better rank, then to the lexical list, then to the lower id — so a vector
// hit that merely ties the lexical head never displaces it, and the order is
// deterministic. With an empty vector list the fused order is exactly the
// lexical order.
func fuse(lex, vec []int64, k int) []int64 {
	type entry struct {
		score    float64
		bestRank int
		lexical  bool
	}
	e := map[int64]*entry{}
	add := func(ids []int64, lexical bool) {
		for i, id := range ids {
			rank := i + 1
			x, ok := e[id]
			if !ok {
				x = &entry{bestRank: rank, lexical: lexical}
				e[id] = x
			}
			if rank < x.bestRank {
				x.bestRank = rank
			}
			if lexical {
				x.lexical = true
			}
			x.score += 1 / float64(rrfK+rank)
		}
	}
	add(lex, true)
	add(vec, false)

	out := make([]int64, 0, len(e))
	for id := range e {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := e[out[i]], e[out[j]]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.bestRank != b.bestRank {
			return a.bestRank < b.bestRank
		}
		if a.lexical != b.lexical {
			return a.lexical
		}
		return out[i] < out[j]
	})
	if len(out) > k {
		out = out[:k]
	}
	return out
}

// memoriesByID loads the memories named by ids, in that order, dropping any
// id no longer present. Search uses it to materialise its fused order after
// the matchers ranked ids.
func (m *Matchers) memoriesByID(ids []int64) ([]Memory, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id
	}
	rows, err := m.reader.readRows(`SELECT `+memoryCols+` FROM memory WHERE id IN (`+strings.Join(ph, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[int64]Memory{}
	for rows.Next() {
		mm, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		byID[mm.Id] = mm
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Memory, 0, len(ids))
	for _, id := range ids {
		if mm, ok := byID[id]; ok {
			out = append(out, mm)
		}
	}
	return out, nil
}

// related returns the outgoing superseded-by and found-by edges of id, the
// relations a Search hit surfaces so a found memory shows where it sits.
func (m *Matchers) related(id int64) ([]Related, error) {
	rows, err := m.reader.readRows(`
		SELECT rel, to_id FROM edge
		WHERE from_id = ? AND rel IN ('superseded-by', 'found-by')
		ORDER BY ts, to_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Related{}
	for rows.Next() {
		var r Related
		if err := rows.Scan(&r.Rel, &r.To); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// vecFromBytes carries a float32 vector's little-endian bytes back to a
// vector; a length that is not a whole number of float32s reads back nil,
// never a torn value.
func vecFromBytes(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// cosine is the cosine similarity of two equal-length vectors; a mismatched
// length or a zero norm scores 0, never NaN.
func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
