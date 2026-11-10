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

// Personal profile, CLI: -scope takes a comma list and -k defaults to 8. A
// comma list of one scope is that scope; an empty list means all scopes.
func TestParseProfileArgs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		scopes []string
		k      int
	}{
		{name: "defaults", args: nil, scopes: nil, k: 8},
		{name: "one scope", args: []string{"-scope", "a"}, scopes: []string{"a"}, k: 8},
		{name: "comma list", args: []string{"-scope", "a,b"}, scopes: []string{"a", "b"}, k: 8},
		{name: "spaces trimmed", args: []string{"-scope", " a , b "}, scopes: []string{"a", "b"}, k: 8},
		{name: "k over cap passes through", args: []string{"-k", "200"}, scopes: nil, k: 200},
		{name: "k zero is empty", args: []string{"-k", "0"}, scopes: nil, k: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scopes, k, err := parseProfileArgs(tc.args)
			if err != nil {
				t.Fatalf("parseProfileArgs(%v): %v", tc.args, err)
			}
			if strings.Join(scopes, ",") != strings.Join(tc.scopes, ",") {
				t.Errorf("scopes = %v, want %v", scopes, tc.scopes)
			}
			if k != tc.k {
				t.Errorf("k = %d, want %d", k, tc.k)
			}
		})
	}
}

// Personal profile, CLI: an unknown flag and stray positional arguments are
// rejected, like every other verb.
func TestParseProfileArgsRejects(t *testing.T) {
	for _, args := range [][]string{{"-nope"}, {"stray"}} {
		if _, _, err := parseProfileArgs(args); err == nil {
			t.Errorf("parseProfileArgs(%v) accepted, want an error", args)
		}
	}
}
