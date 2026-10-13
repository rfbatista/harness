package main

import (
	"flag"
	"fmt"
	"os"

	"operators-mcp/internal/adapter/in/shim"
	"operators-mcp/internal/app"
	"operators-mcp/internal/app/cliflags"
)

// The server binary is also task sessions' PATH shim: each session's shim
// dir links sed, tee, cp, mv, rm, touch and mkdir to it, so it works as
// built, under `go run` and under Air alike, with nothing else to build.
func main() {
	if shim.Is(os.Args[0]) {
		os.Exit(shim.Main())
	}
	cfg, err := cliflags.Parse(flag.CommandLine, os.Args[1:], os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if exe, err := os.Executable(); err == nil {
		cfg.ShimBinary = exe
	}
	app.New(cfg).Run()
}
