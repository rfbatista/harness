//go:build !windows

// Package shim is the PATH shim for sed, tee, cp, mv, rm, touch and mkdir
// in a task session (contract "Harness server ↔ Agent sessions — virtual
// shell" §5). The server binary runs it when invoked under one of those
// names: each session's shim dir, first on its PATH, links the names to
// the server binary.
//
// A command whose operands are all outside the session's shell
// ($HARNESS_SHELL), or sed without -i, becomes the real command (the next
// one on PATH) unchanged. Otherwise the shim asks the session's shell
// endpoint ($HARNESS_SHELL_URL) to check the operation, runs the real
// command, then commits it; a refusal goes to stderr, starting with its
// code, and exits 1.
package shim

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"operators-mcp/internal/ports"
)

const unavailable = "SHELL_UNAVAILABLE: the harness could not be reached; nothing was written"

// Is says whether a process started as argv0 is a shim.
func Is(argv0 string) bool {
	return slices.Contains(ports.ShellShimCommands, filepath.Base(argv0))
}

// Main runs the shim for os.Args and answers its exit code. It answers only
// when it does not become the real command.
func Main() int {
	name := filepath.Base(os.Args[0])
	args := os.Args[1:]
	real, err := realBinary(name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		return 127
	}
	shell := os.Getenv("HARNESS_SHELL")
	if shell == "" {
		return become(real)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return become(real)
	}
	edits := parse(name, args, real, cwd)
	if !inside(shell, edits) {
		return become(real)
	}
	h := harness{base: strings.TrimRight(os.Getenv("HARNESS_SHELL_URL"), "/"), tool: name, shell: newShellDir(shell)}
	return h.run(real, edits)
}

// become replaces this process with the real command, so its output and
// exit code are the real command's own.
func become(real string) int {
	err := syscall.Exec(real, os.Args, os.Environ())
	fmt.Fprintf(os.Stderr, "%s: %v\n", filepath.Base(os.Args[0]), err)
	return 126
}

// realBinary is name's first executable on PATH that is not this shim.
func realBinary(name string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	selfInfo, err := os.Stat(self)
	if err != nil {
		return "", err
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || os.SameFile(info, selfInfo) {
			continue
		}
		if !filepath.IsAbs(p) {
			if p, err = filepath.Abs(p); err != nil {
				continue
			}
		}
		return p, nil
	}
	return "", errors.New("command not found")
}

// edit is one operation for the shell endpoints: {op, paths}. walk marks a
// cp whose source is a directory: once copied, the files under its
// destination are committed as writes.
type edit struct {
	op    string
	paths []string
	walk  bool
}

// shellDir is the session's shell, as made and with its symlinks resolved.
type shellDir []string

func newShellDir(dir string) shellDir {
	s := shellDir{filepath.Clean(dir)}
	if r, err := filepath.EvalSymlinks(dir); err == nil && r != s[0] {
		s = append(s, r)
	}
	return s
}

// rel is p relative to the shell, and whether p is in it.
func (s shellDir) rel(p string) (string, bool) {
	for _, root := range s {
		if p == root {
			return ".", true
		}
		if r, ok := strings.CutPrefix(p, root+string(filepath.Separator)); ok {
			return r, true
		}
	}
	return "", false
}

// changed is what e changes in the shell, relative to it: a cp only its
// destination, an mv its destination or else its source.
func (s shellDir) changed(e edit) []string {
	paths := e.paths
	switch e.op {
	case "cp":
		paths = paths[1:]
	case "mv":
		if _, in := s.rel(paths[1]); in {
			paths = paths[1:]
		} else {
			paths = paths[:1]
		}
	}
	var out []string
	for _, p := range paths {
		if r, in := s.rel(p); in {
			out = append(out, r)
		}
	}
	return out
}

// inside says whether any edit changes a path in the shell.
func inside(shell string, edits []edit) bool {
	s := newShellDir(shell)
	return slices.ContainsFunc(edits, func(e edit) bool { return len(s.changed(e)) > 0 })
}

// harness talks to the session's shell endpoints.
type harness struct {
	base  string // …/api/sessions/<id>/shell
	tool  string
	shell shellDir
}

type answer struct {
	Allow  bool   `json:"allow"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

var client = &http.Client{Timeout: 5 * time.Second}

// call posts e to step (check or commit); err means the harness did not
// answer.
func (h harness) call(step string, e edit) (answer, error) {
	if h.base == "" {
		return answer{}, errors.New("HARNESS_SHELL_URL is not set")
	}
	body, _ := json.Marshal(ports.ShellEdit{Op: e.op, Paths: e.paths, Tool: h.tool})
	res, err := client.Post(h.base+"/"+step, "application/json", bytes.NewReader(body))
	if err != nil {
		return answer{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return answer{}, fmt.Errorf("%s answered %s", step, res.Status)
	}
	var a answer
	if err := json.NewDecoder(res.Body).Decode(&a); err != nil {
		return answer{}, err
	}
	return a, nil
}

// run checks every edit, runs the real command, and commits them. A
// refused check runs nothing; the edits already allowed are committed
// unmade, which releases them and stores nothing, without a word.
func (h harness) run(real string, edits []edit) int {
	for i, e := range edits {
		a, err := h.call("check", e)
		if err == nil && a.Allow {
			continue
		}
		for _, allowed := range edits[:i] {
			_, _ = h.call("commit", allowed)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, unavailable)
		} else {
			fmt.Fprintln(os.Stderr, a.Reason)
		}
		return 1
	}
	code := runChild(real)
	if !h.commitAll(edits) {
		return 1
	}
	return code
}

// commitAll commits edits, printing each refusal; false when one was
// refused or not answered. Once the harness does not answer, every path
// left is named as changed on disk but not stored.
func (h harness) commitAll(edits []edit) bool {
	ok, reached := true, true
	for _, e := range edits {
		for _, c := range append([]edit{e}, copied(e)...) {
			var a answer
			var err error
			if reached {
				a, err = h.call("commit", c)
				reached = err == nil
			}
			switch {
			case !reached:
				for _, rel := range h.shell.changed(c) {
					fmt.Fprintf(os.Stderr, "SHELL_UNAVAILABLE: the harness could not be reached; %s changed on disk but was not stored — redo the change once the harness is back\n", rel)
				}
				ok = false
			case !a.Allow:
				fmt.Fprintln(os.Stderr, a.Reason)
				ok = false
			}
		}
	}
	return ok
}

// copied is a write of every file a directory copy made under its
// destination.
func copied(e edit) []edit {
	if !e.walk {
		return nil
	}
	var files []string
	_ = filepath.WalkDir(e.paths[1], func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if len(files) == 0 {
		return nil
	}
	return []edit{{op: "write", paths: files}}
}

// runChild runs the real command with this process's arguments and stdio
// and answers its exit code. Interrupts are left to the child, so the
// commit still runs after it.
func runChild(real string) int {
	// Caught, not ignored: an ignored signal would stay ignored in the child.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGINT, syscall.SIGQUIT)
	cmd := &exec.Cmd{Path: real, Args: os.Args, Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}
	err := cmd.Run()
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		fmt.Fprintf(os.Stderr, "%s: %v\n", filepath.Base(os.Args[0]), err)
		return 126
	}
	if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return exit.ExitCode()
}
