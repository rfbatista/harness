//go:build !windows

package shim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The git shim of the branch gate (contract "Harness server ↔ Agent sessions —
// branch gate" §3): git's own arguments are read by the classifier the hook
// uses too, and a branch action runs only with a branch grant.

// NeededMessage is what the agent is told when a branch action has no grant.
func neededMessage(args []string) string {
	return fmt.Sprintf("BRANCH_REQUEST_NEEDED: run \"git %s\" as its own Bash command so the harness can ask the person", strings.Join(args, " "))
}

// Unavailable is what the agent is told when the harness cannot be reached:
// the action is not made.
const gateUnavailable = "BRANCH_GATE_UNAVAILABLE: the harness could not be reached; the branch action was not made. Try again later."

var gateClient = &http.Client{Timeout: 10 * time.Second}

// gitMain runs the real git at once unless the arguments are a branch action
// without a grant.
func gitMain(args []string) int {
	real, err := realBinary(ports.BranchGateShimCommand)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git: %v\n", err)
		return 127
	}
	cwd, err := os.Getwd()
	if err != nil {
		return become(real)
	}
	actions := domain.ClassifyGit(args, cwd, gitProbe{real: real})
	gated := actions[:0]
	for _, a := range actions {
		if a.Kind.Gated() {
			gated = append(gated, a)
		}
	}
	if len(gated) == 0 {
		return become(real)
	}
	base := strings.TrimRight(os.Getenv(ports.BranchGateURLEnv), "/")
	for _, a := range gated {
		ans, err := askGrant(base, ports.BranchGrantRequest{BranchAction: a, CWD: cwd})
		if err != nil {
			fmt.Fprintln(os.Stderr, gateUnavailable)
			return 1
		}
		if !ans.Allow {
			fmt.Fprintln(os.Stderr, neededMessage(args))
			return 1
		}
	}
	return become(real)
}

func askGrant(base string, req ports.BranchGrantRequest) (ports.BranchGrantAnswer, error) {
	if base == "" {
		return ports.BranchGrantAnswer{}, fmt.Errorf("%s is not set", ports.BranchGateURLEnv)
	}
	body, _ := json.Marshal(req)
	res, err := gateClient.Post(base+"/grant", "application/json", bytes.NewReader(body))
	if err != nil {
		return ports.BranchGrantAnswer{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ports.BranchGrantAnswer{}, fmt.Errorf("grant answered %s", res.Status)
	}
	var a ports.BranchGrantAnswer
	err = json.NewDecoder(res.Body).Decode(&a)
	return a, err
}

// gitProbe answers the classifier's questions by asking the real git.
type gitProbe struct{ real string }

func (p gitProbe) run(dir string, args ...string) (string, bool) {
	cmd := exec.Command(p.real, append([]string{"-C", dir}, args...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", false
	}
	return strings.TrimSpace(out.String()), true
}

func (p gitProbe) Commit(dir, ref string) string {
	out, _ := p.run(dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return out
}

func (p gitProbe) Branch(dir, name string) bool {
	_, ok := p.run(dir, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return ok
}

func (p gitProbe) RemoteBranch(dir, name string) (string, bool) {
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

func (p gitProbe) Path(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func (p gitProbe) Current(dir string) string {
	out, _ := p.run(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	return out
}
