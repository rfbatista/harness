package main

import (
	"flag"
	"fmt"
	"os"

	"operators-mcp/internal/app"
	"operators-mcp/internal/app/cliflags"
)

func main() {
	cfg, err := cliflags.Parse(flag.CommandLine, os.Args[1:], os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	app.New(cfg).Run()
}
