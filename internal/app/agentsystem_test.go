package app

import (
	"os"
	"path/filepath"
	"testing"
)

type fixedBin string

func (b fixedBin) Bin() string    { return string(b) }
func (fixedBin) Installed() bool  { return true }
func (fixedBin) Overridden() bool { return true }

// The CLI version is the one of the binary a run launches, read at each run:
// a CLI replaced between two runs shows in the second.
func TestTheCLIVersionIsReadAtEachCallFromTheBinaryInUse(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "claude")
	write := func(version string) {
		t.Helper()
		if err := os.WriteFile(bin+".new", []byte("#!/bin/sh\necho '"+version+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(bin+".new", bin); err != nil {
			t.Fatal(err)
		}
	}
	write("2.1.0 (Claude Code)")
	version := cliVersion(fixedBin(bin))
	if got := version(); got != "2.1.0 (Claude Code)" {
		t.Fatalf("version = %q", got)
	}
	write("2.2.0 (Claude Code)")
	if got := version(); got != "2.2.0 (Claude Code)" {
		t.Fatalf("version = %q after the CLI was replaced", got)
	}
	if got := cliVersion(fixedBin(filepath.Join(t.TempDir(), "missing")))(); got != "" {
		t.Fatalf("a binary that cannot run answered %q", got)
	}
}
