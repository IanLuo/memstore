package main

import (
	"errors"
	"strings"
	"testing"
)

// Card A2: every error prints with the program prefix exactly once. Store
// errors already carry "memstore: "; CLI-local errors do not and get it added
// — never two, never zero.
func TestErrorMessageOnePrefix(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{
			name: "store error",
			err:  errors.New("memstore: put: text is empty"),
			want: "memstore: put: text is empty",
		},
		{
			name: "CLI-local bad id",
			err:  errors.New(`get: bad id "x": parsing "x": invalid syntax`),
			want: `memstore: get: bad id "x": parsing "x": invalid syntax`,
		},
		{
			name: "CLI-local empty query",
			err:  errors.New("search: empty query"),
			want: "memstore: search: empty query",
		},
		{
			name: "CLI-local close bad id",
			err:  errors.New(`close: bad id "x": parsing "x": invalid syntax`),
			want: `memstore: close: bad id "x": parsing "x": invalid syntax`,
		},
		{
			name: "CLI-local link arity",
			err:  errors.New("link: want from, relation, and to"),
			want: "memstore: link: want from, relation, and to",
		},
		{
			name: "CLI-local stats arity",
			err:  errors.New("stats: takes no arguments"),
			want: "memstore: stats: takes no arguments",
		},
	} {
		got := errorMessage(tc.err)
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
		if n := strings.Count(got, "memstore:"); n != 1 {
			t.Errorf("%s: %d memstore: prefixes, want exactly 1", tc.name, n)
		}
	}
}
