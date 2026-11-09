//go:build !windows

package shim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"operators-mcp/internal/adapter/out/gitprobe"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The git shim of the branch gate (contract "Harness server ↔ Agent sessions —
// branch gate" §3): git's own arguments are read by the classifier the hook
// uses too, and a branch action runs only with a branch grant.

// neededMessage is what the agent is told when a branch action has no grant.
func neededMessage(args []string) string {
	return fmt.Sprintf("BRANCH_REQUEST_NEEDED: run \"git %s\" as its own Bash command so the harness can ask the person", strings.Join(args, " "))
}

// gateUnavailable is what the agent is told when the harness cannot be reached:
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
	actions := domain.ClassifyGit(args, cwd, gitprobe.Probe{Git: real})
	for _, a := range actions {
		if a.Refused != "" {
			fmt.Fprintln(os.Stderr, a.Refused)
			return 1
		}
	}
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
	var pushRequests []string
	for _, a := range gated {
		ans, err := askGrant(base, ports.BranchGrantRequest{BranchAction: a, CWD: cwd})
		if err != nil {
			fmt.Fprintln(os.Stderr, gateUnavailable)
			return 1
		}
		if !ans.Allow {
			if ans.Reason != "" && strings.HasPrefix(ans.Reason, "REFUSED") {
				fmt.Fprintln(os.Stderr, ans.Reason)
			} else {
				fmt.Fprintln(os.Stderr, neededMessage(args))
			}
			return 1
		}
		if a.Kind == domain.BranchPush && ans.RequestID != "" {
			pushRequests = append(pushRequests, ans.RequestID)
		}
	}
	if len(pushRequests) == 0 {
		return become(real)
	}
	// A push runs as a child, so the harness hears it succeeded and can keep
	// the push against its request (ADR 0071).
	cmd := exec.Command(real, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	if cmd.ProcessState == nil { // git did not start
		fmt.Fprintf(os.Stderr, "git: %v\n", err)
		return 126
	}
	code := cmd.ProcessState.ExitCode()
	if err == nil {
		for _, id := range pushRequests {
			tellPushed(base, ports.BranchPushed{RequestID: id, CWD: cwd})
		}
	}
	return code
}

// tellPushed is best effort: a push that happened stays one when the harness
// cannot be told.
func tellPushed(base string, p ports.BranchPushed) {
	body, _ := json.Marshal(p)
	res, err := gateClient.Post(base+"/pushed", "application/json", bytes.NewReader(body))
	if err == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}
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
