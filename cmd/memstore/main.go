// Command memstore is the CLI over the memory store. It carries no logic: it
// parses arguments, calls the store, and prints.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"memstore"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "put":
		err = runPut(os.Args[2:])
	case "get":
		err = runGet(os.Args[2:])
	case "search":
		err = runSearch(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, errorMessage(err))
		os.Exit(1)
	}
}

// errorMessage returns err with the program prefix exactly once: store errors
// already carry "memstore: ", CLI-local ones do not and are prefixed here.
func errorMessage(err error) string {
	msg := err.Error()
	if strings.HasPrefix(msg, "memstore: ") {
		return msg
	}
	return "memstore: " + msg
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: memstore <command> [args]

  put -kind KIND [-scope S] [-title T] [TEXT...]  append a memory; TEXT from args or stdin
  get ID                                          one memory, as JSON
  search [-k N] QUERY...                          matching memories, as JSON, best first

The store is $MEMSTORE_DB, or ~/.memstore/memory.db.
`)
}

func runPut(args []string) error {
	fs := flag.NewFlagSet("put", flag.ExitOnError)
	kind := fs.String("kind", "", "memory kind (required; any label)")
	scope := fs.String("scope", "", "scope the memory belongs to")
	title := fs.String("title", "", "short title")
	fs.Parse(args)

	text := strings.Join(fs.Args(), " ")
	if text == "" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("put: read stdin: %w", err)
		}
		text = string(b)
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()

	id, err := s.Put(&memstore.Memory{
		Kind:  *kind,
		Scope: *scope,
		Title: *title,
		Text:  text,
	})
	if err != nil {
		return err
	}
	fmt.Println(id)
	return nil
}

func runGet(args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("get: want exactly one id")
	}
	id, err := strconv.ParseInt(fs.Arg(0), 10, 64)
	if err != nil {
		return fmt.Errorf("get: bad id %q: %w", fs.Arg(0), err)
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()

	m, err := s.Get(id)
	if err != nil {
		return err
	}
	return writeJSON(m)
}

func runSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	k := fs.Int("k", 10, "maximum number of memories")
	fs.Parse(args)
	q := strings.Join(fs.Args(), " ")
	if q == "" {
		return errors.New("search: empty query")
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()

	hits, err := s.Search(q, *k)
	if err != nil {
		return err
	}
	if hits == nil {
		hits = []memstore.Memory{}
	}
	return writeJSON(hits)
}

func openStore() (*memstore.Store, error) {
	path := os.Getenv("MEMSTORE_DB")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".memstore", "memory.db")
	}
	return memstore.Open(path)
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
