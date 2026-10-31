package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/IanLuo/memstore/web"
)

// runServe starts the read-only web view on the loopback interface. It is
// process wiring only: the listener is bound before the URL is printed, so a
// port already in use fails here rather than inside http.Serve, and the store
// is opened through the same helper every other verb uses.
func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", 8765, "loopback port to listen on")
	fs.Parse(args)
	if fs.NArg() != 0 {
		return errors.New("serve: takes no arguments")
	}
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("serve: port %d is outside 1..65535", *port)
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(*port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	fmt.Printf("memstore serve: http://%s\n", addr)
	return http.Serve(ln, web.Handler(s))
}
