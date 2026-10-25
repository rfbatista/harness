//go:build !windows

package shim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"

	"operators-mcp/internal/ports"
)

// Unreachable is what the agent is told when the architecture rules could not
// be checked: the command then runs, as the check never blocks on its own
// failure.
const Unreachable = "Architecture rules could not be checked: the harness could not be reached."

// written is the paths an edit writes: a cp only its destination, anything
// else every path it names (an mv changes its source and its destination).
func written(e edit) []string {
	if e.op == "cp" && len(e.paths) > 1 {
		return e.paths[1:]
	}
	return e.paths
}

// touches says whether any edit writes a path in the worktree.
func touches(worktree string, edits []edit) bool {
	tree := newShellDir(worktree)
	return slices.ContainsFunc(edits, func(e edit) bool {
		return slices.ContainsFunc(written(e), func(p string) bool { _, in := tree.rel(p); return in })
	})
}

// checkArchitecture asks the session's architecture endpoint about every edit,
// and prints what it says: a blocking rule's text stops the command (exit 1),
// a warning is printed and the command goes on, and a harness that does not
// answer is said so and lets the command go on.
func checkArchitecture(base, tool string, edits []edit) (code int, stop bool) {
	for _, e := range edits {
		v, err := askArchitecture(base, tool, e)
		if err != nil {
			fmt.Fprintln(os.Stderr, Unreachable)
			return 0, false
		}
		if v.Deny != "" {
			fmt.Fprintln(os.Stderr, v.Deny)
			if v.Context != "" {
				fmt.Fprintln(os.Stderr, v.Context)
			}
			return 1, true
		}
		if v.Context != "" {
			fmt.Fprintln(os.Stderr, v.Context)
		}
	}
	return 0, false
}

func askArchitecture(base, tool string, e edit) (ports.ArchitectureVerdict, error) {
	body, _ := json.Marshal(map[string]any{"op": e.op, "paths": written(e), "tool": tool})
	res, err := client.Post(base+"/check", "application/json", bytes.NewReader(body))
	if err != nil {
		return ports.ArchitectureVerdict{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ports.ArchitectureVerdict{}, fmt.Errorf("check answered %s", res.Status)
	}
	var v ports.ArchitectureVerdict
	err = json.NewDecoder(res.Body).Decode(&v)
	return v, err
}
