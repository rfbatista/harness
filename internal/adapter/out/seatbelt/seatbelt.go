// Package seatbelt is the macOS sandbox driver (ADR 0017): it confines a
// command with sandbox-exec and collects what the sandbox denies from the
// unified log.
package seatbelt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"operators-mcp/internal/ports"
)

// Driver wraps commands in sandbox-exec. Everything under the person's home
// directory is denied except the policy's paths; the rest of the system stays
// readable and the network open. Subprocesses inherit the profile.
type Driver struct {
	// Exec is the sandbox-exec binary. Empty: "sandbox-exec".
	Exec string
	// Home is the person's home directory. Nil: the user's.
	Home func() (string, error)
}

var _ ports.SandboxDriver = (*Driver)(nil)

// New is the Seatbelt driver.
func New() *Driver { return &Driver{} }

func (d *Driver) Name() string { return "seatbelt" }

// Wrap answers sandbox-exec running cmd under a profile for policy. Paths
// reach the profile as -D parameters, never as profile text, so no path can
// break out of it.
func (d *Driver) Wrap(policy ports.SandboxPolicy, cmd ports.SandboxCommand) (ports.SandboxCommand, error) {
	homeFn := d.Home
	if homeFn == nil {
		homeFn = os.UserHomeDir
	}
	home, err := homeFn()
	if err != nil {
		return ports.SandboxCommand{}, fmt.Errorf("sandbox: home directory: %w", err)
	}
	pr := &profile{}
	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n")
	if home == "" || !filepath.IsAbs(home) {
		return ports.SandboxCommand{}, errors.New("sandbox: no absolute home directory")
	}
	homes, err := pr.variants(home)
	if err != nil {
		return ports.SandboxCommand{}, err
	}
	// The system temp dir is shared by every process of the user, other
	// sessions' included, so it is closed like the home: a session reaches
	// only the temp dir its policy names.
	temps, err := pr.variants(os.TempDir())
	if err != nil {
		return ports.SandboxCommand{}, err
	}
	hidden, err := pr.all(policy.Hidden)
	if err != nil {
		return ports.SandboxCommand{}, err
	}
	tag := pr.param(tagFor(policy.WorkingDir))
	fmt.Fprintf(&b, "(deny file-read* file-write* (with message %s)", tag)
	// Hidden paths are denied wherever they lie; the allows below re-open
	// what the policy grants inside them.
	for _, p := range slices.Concat(homes, temps, hidden) {
		fmt.Fprintf(&b, " (subpath %s)", p)
	}
	b.WriteString(")\n(allow file-read-metadata)\n")
	// Apple's git and xcrun keep a cache file in the system temp dir, whatever
	// TMPDIR says; only those are let through it.
	for _, re := range pr.xcrunCaches(os.TempDir()) {
		fmt.Fprintf(&b, "(allow file-read* file-write* (regex %s))\n", re)
	}

	// Later rules win, so the paths go in by depth, parents first: a read-only
	// path is write-denied then read-allowed, a writable one allowed, and a
	// nested path (the other worktrees' entries inside a writable git dir, the
	// session's own entry inside those) overrides what encloses it. A
	// read-only path outside the home stays read-only too.
	type rule struct {
		path  string
		write bool
	}
	var rules []rule
	for _, p := range policy.Readable {
		rules = append(rules, rule{p, false})
	}
	for _, p := range policy.Writable {
		rules = append(rules, rule{p, true})
	}
	slices.SortStableFunc(rules, func(a, b rule) int {
		return strings.Count(filepath.Clean(a.path), "/") - strings.Count(filepath.Clean(b.path), "/")
	})
	for _, r := range rules {
		vs, err := pr.variants(r.path)
		if err != nil {
			return ports.SandboxCommand{}, err
		}
		for _, p := range vs {
			if r.write {
				fmt.Fprintf(&b, "(allow file-read* file-write* (subpath %s))\n", p)
			} else {
				fmt.Fprintf(&b, "(deny file-write* (with message %s) (subpath %s))\n(allow file-read* (subpath %s))\n", tag, p, p)
			}
		}
	}

	args := []string{}
	for i, v := range pr.values {
		args = append(args, "-D", fmt.Sprintf("P%d=%s", i, v))
	}
	args = append(args, "-p", b.String(), "--", cmd.Path)
	args = append(args, cmd.Args...)
	exe := d.Exec
	if exe == "" {
		exe = "sandbox-exec"
	}
	return ports.SandboxCommand{Path: exe, Args: args, Dir: cmd.Dir, Env: cmd.Env}, nil
}

// profile collects the path parameters of one profile.
type profile struct{ values []string }

// param registers a value and answers the profile expression naming it.
func (p *profile) param(v string) string {
	p.values = append(p.values, v)
	return fmt.Sprintf(`(param "P%d")`, len(p.values)-1)
}

// variants are a path as given and as the file system resolves it (/tmp is
// /private/tmp, /var is /private/var): the kernel matches the resolved one.
// Each comes back as a profile expression naming its parameter.
func (p *profile) variants(path string) ([]string, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("sandbox: path %q is not absolute", path)
	}
	if strings.ContainsRune(path, 0) {
		return nil, fmt.Errorf("sandbox: path %q has a NUL byte", path)
	}
	var out []string
	for _, v := range variantPaths(path) {
		out = append(out, p.param(v))
	}
	return out, nil
}

// xcrunCaches are regular expressions, as parameters, matching the cache
// files Apple's git and xcrun create in dir (xcrun_db-XXXXXXXX): the name and
// nothing else in it. Each variant of dir gets one.
func (p *profile) xcrunCaches(dir string) []string {
	var out []string
	for _, v := range variantPaths(dir) {
		out = append(out, p.param("^"+regexp.QuoteMeta(v)+"/xcrun_db(-[A-Za-z0-9]+)?$"))
	}
	return out
}

// variantPaths are path cleaned and, when it differs, as the file system
// resolves it.
func variantPaths(path string) []string {
	path = filepath.Clean(path)
	vs := []string{path}
	if r := resolve(path); r != path {
		vs = append(vs, r)
	}
	return vs
}

func (p *profile) all(paths []string) ([]string, error) {
	var out []string
	for _, path := range paths {
		vs, err := p.variants(path)
		if err != nil {
			return nil, err
		}
		out = append(out, vs...)
	}
	return out, nil
}

// resolve follows symlinks in path; for a path that does not exist yet it
// resolves the deepest ancestor that does.
func resolve(path string) string {
	rest := ""
	for cur := path; ; cur = filepath.Dir(cur) {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest)
		}
		if cur == filepath.Dir(cur) {
			return path
		}
		rest = filepath.Join(filepath.Base(cur), rest)
	}
}
