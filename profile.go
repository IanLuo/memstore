package memstore

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The digest's bounds. The spec fixes them: at most 64 factors and 24 KB of
// JSON, the byte budget enforced while the output is built. Titles and values
// are each cut at 200 characters, so one row cannot exceed the budget alone.
const (
	digestMaxFactors = 64
	digestMaxBytes   = 24 * 1024
	digestTextCap    = 200
)

// digestJSON is the digest as the CLI prints it: json.MarshalIndent with a
// two-space indent and a trailing newline, which is what an Encoder with
// SetIndent("", "  ") writes. The byte bound is measured on these bytes, not on
// compact JSON — the two differ by roughly 8% at 64 factors, and the bound has
// to hold on what actually leaves the process.
func digestJSON(d Digest) ([]byte, error) {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Factor is one resolved (kind, title) identity: its repeat count, when it was
// first and last seen, the newest open row's text, and the live/closed split.
// Value is empty when no row is open. Scopes counts the distinct scopes the
// factor's rows fall in, so a caller sees a multi-project merge.
type Factor struct {
	Kind   string    `json:"kind"`
	Title  string    `json:"title"`
	Value  string    `json:"value"`
	Count  int       `json:"count"`
	Live   int       `json:"live"`
	Closed int       `json:"closed"`
	First  time.Time `json:"first"`
	Last   time.Time `json:"last"`
	Scopes int       `json:"scopes"`
}

// Digest is the bounded profile of current factors. Omitted counts the factors
// the bound cut, so a reader can tell a short digest from a truncated one.
type Digest struct {
	Factors []Factor `json:"factors"`
	Omitted int      `json:"omitted"`
}

// factorKey is a factor's identity: the kind as stored, and the title trimmed
// and case-folded. The fold is Unicode-aware, which SQLite's lower() is not, so
// the grouping happens here rather than in SQL.
type factorKey struct{ kind, title string }

// factorAcc accumulates one factor's rows while the read streams them.
type factorAcc struct {
	f          Factor
	scopeSet   map[string]struct{}
	newestOpen int64
	hasOpen    bool
	newestText string
}

// Profile returns a bounded digest of the live memories' current factors. A
// factor is one (kind, title) identity; the title is trimmed and case-folded,
// and a row with an empty title is not a factor. scopes filters which rows
// count (none means all scopes). k bounds the rows returned: k ≤ 0 is empty,
// and k is capped at 64. It is a read: it writes nothing and stores nothing.
func (s *Store) Profile(scopes []string, k int) (Digest, error) {
	d := Digest{Factors: []Factor{}}
	if k <= 0 {
		return d, nil
	}
	if k > digestMaxFactors {
		k = digestMaxFactors
	}

	query := `SELECT kind, title, scope, status, text, ts FROM memory WHERE deleted_at IS NULL`
	args := []any{}
	if len(scopes) > 0 {
		query += ` AND scope IN (` + placeholders(len(scopes)) + `)`
		for _, sc := range scopes {
			args = append(args, sc)
		}
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return d, fmt.Errorf("memstore: profile: %w", err)
	}
	defer rows.Close()

	accs := map[factorKey]*factorAcc{}
	for rows.Next() {
		var kind, title, scope, status, text string
		var ts int64
		if err := rows.Scan(&kind, &title, &scope, &status, &text, &ts); err != nil {
			return d, fmt.Errorf("memstore: profile: %w", err)
		}
		folded := strings.ToLower(strings.TrimSpace(title))
		if folded == "" {
			continue
		}
		key := factorKey{kind: kind, title: folded}
		a, ok := accs[key]
		if !ok {
			a = &factorAcc{
				f:        Factor{Kind: kind, Title: folded},
				scopeSet: map[string]struct{}{},
			}
			accs[key] = a
		}
		a.add(status, text, scope, ts)
	}
	if err := rows.Err(); err != nil {
		return d, fmt.Errorf("memstore: profile: %w", err)
	}

	factors := make([]Factor, 0, len(accs))
	for _, a := range accs {
		factors = append(factors, a.finish())
	}
	sort.Slice(factors, func(i, j int) bool { return factorBefore(factors[i], factors[j]) })

	d.Factors, d.Omitted, err = fitDigest(factors, k)
	if err != nil {
		return d, err
	}
	return d, nil
}

// add folds one row into the factor. The newest open row supplies the value;
// a closed row counts but never supplies it.
func (a *factorAcc) add(status, text, scope string, ts int64) {
	a.f.Count++
	if status == "closed" {
		a.f.Closed++
	} else {
		a.f.Live++
		if !a.hasOpen || ts > a.newestOpen {
			a.hasOpen = true
			a.newestOpen = ts
			a.newestText = text
		}
	}
	t := time.Unix(0, ts).UTC()
	if a.f.Count == 1 || t.Before(a.f.First) {
		a.f.First = t
	}
	if a.f.Count == 1 || t.After(a.f.Last) {
		a.f.Last = t
	}
	a.scopeSet[scope] = struct{}{}
}

// finish seals the factor: the value is cut to the text cap, and the scope
// count is the number of distinct scopes seen.
func (a *factorAcc) finish() Factor {
	f := a.f
	f.Scopes = len(a.scopeSet)
	if a.hasOpen {
		f.Value = capText(a.newestText)
	}
	f.Title = capText(f.Title)
	return f
}

// factorBefore is the spec's order: last seen first, then repeat count, then
// scope count, then title. It is total, so the digest is deterministic.
func factorBefore(a, b Factor) bool {
	if !a.Last.Equal(b.Last) {
		return a.Last.After(b.Last)
	}
	if a.Count != b.Count {
		return a.Count > b.Count
	}
	if a.Scopes != b.Scopes {
		return a.Scopes > b.Scopes
	}
	return a.Title < b.Title
}

// fitDigest takes the first k factors and keeps adding them while the digest's
// printed JSON stays within the byte budget. A factor that would break the
// budget is cut, and so is every factor after it: the order is the contract, so
// the cut is a suffix, and omitted counts it exactly. The measurement is exact,
// not estimated, and bounded by the 64-factor cap.
func fitDigest(factors []Factor, k int) ([]Factor, int, error) {
	total := len(factors)
	out := []Factor{}
	for i, f := range factors {
		if i == k {
			return out, total - i, nil
		}
		candidate := append(out, f)
		// Measure with the omitted count this candidate would print. Measuring
		// with a zero would under-count a three-digit omitted by two bytes and
		// let the output cross the bound by that much.
		b, err := digestJSON(Digest{Factors: candidate, Omitted: total - len(candidate)})
		if err != nil {
			return out, 0, fmt.Errorf("memstore: profile: encode digest: %w", err)
		}
		if len(b) > digestMaxBytes {
			return out, total - i, nil
		}
		out = candidate
	}
	return out, 0, nil
}

// capText cuts s to digestTextCap characters, counting runes, so a multi-byte
// character is never split.
func capText(s string) string {
	r := []rune(s)
	if len(r) <= digestTextCap {
		return s
	}
	return string(r[:digestTextCap])
}

// placeholders returns n comma-separated SQL parameter marks.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
