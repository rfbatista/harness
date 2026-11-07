// Package gitprobe answers the branch action classifier's questions about a
// repository by asking git itself.
package gitprobe

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"operators-mcp/internal/domain"
)

var _ domain.GitProbe = Probe{}

// Probe runs the git binary at Git; "" finds git on PATH.
type Probe struct{ Git string }

func (p Probe) run(dir string, args ...string) (string, bool) {
	bin := p.Git
	if bin == "" {
		bin = "git"
	}
	cmd := exec.Command(bin, append([]string{"-C", dir}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", false
	}
	return strings.TrimSpace(out.String()), true
}

// Commit implements domain.GitProbe.
func (p Probe) Commit(dir, ref string) string {
	out, _ := p.run(dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return out
}

// Branch implements domain.GitProbe.
func (p Probe) Branch(dir, name string) bool {
	_, ok := p.run(dir, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return ok
}

// RemoteBranch implements domain.GitProbe.
func (p Probe) RemoteBranch(dir, name string) (string, bool) {
	out, _ := p.run(dir, "for-each-ref", "--format=%(refname)", "refs/remotes/*/"+name)
	var remote string
	n := 0
	for _, ref := range strings.Fields(out) {
		rest, ok := strings.CutPrefix(ref, "refs/remotes/")
		if !ok || !strings.HasSuffix(rest, "/"+name) {
			continue
		}
		remote = strings.TrimSuffix(rest, "/"+name)
		n++
	}
	return remote, n == 1 && remote != "" && !strings.Contains(remote, "/")
}

// Path implements domain.GitProbe.
func (p Probe) Path(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// Current implements domain.GitProbe.
func (p Probe) Current(dir string) string {
	out, _ := p.run(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	return out
}
