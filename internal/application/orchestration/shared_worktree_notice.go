package orchestration

import (
	"context"
	"strings"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// sharedPeer is another session on the same worktree, as a session's opening
// names it.
type sharedPeer struct {
	Agent   string
	Mode    domain.SessionMode
	Access  domain.SessionAccess
	Running bool
}

// sharedWorktreePeers lists the sessions other than id on the worktree,
// running or ended, oldest first.
func (s *Service) sharedWorktreePeers(ctx context.Context, id, workspaceID string) []sharedPeer {
	if workspaceID == "" {
		return nil
	}
	others, err := s.sessions.Query(ctx, ports.SessionFilter{WorkspaceID: workspaceID})
	if err != nil {
		return nil
	}
	var peers []sharedPeer
	for _, o := range others {
		if o.ID == id {
			continue
		}
		peers = append(peers, sharedPeer{
			Agent:   s.agentName(ctx, o.AgentID),
			Mode:    o.Mode,
			Access:  o.Access,
			Running: !o.Status.IsTerminal(),
		})
	}
	return peers
}

// withSharedWorktree adds the "Shared worktree" section to the opening of a
// session whose worktree another session is on. The list is read when the
// opening is built, so a resume names who is there now. A session alone, or
// with no worktree, keeps its opening as it is.
func (s *Service) withSharedWorktree(ctx context.Context, cfg *llmkit.SessionConfig, id, workspaceID string, access domain.SessionAccess, branch, path string) {
	section := sharedWorktreeSection(branch, path, access, s.sharedWorktreePeers(ctx, id, workspaceID))
	if section != "" {
		cfg.AppendSystem = appendSection(cfg.AppendSystem, section)
	}
}

// sharedWorktreeSection is the opening's section for a session that is not
// alone on its worktree: who else is there and how to share it. The harness
// locks nothing, so what the agents know of each other is all there is.
func sharedWorktreeSection(branch, path string, access domain.SessionAccess, peers []sharedPeer) string {
	if len(peers) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Shared worktree\n\n")
	b.WriteString("Your worktree " + path + ", on branch " + branch + ", is shared with these other sessions:\n\n")
	for _, p := range peers {
		b.WriteString("- " + peerLine(p) + "\n")
	}
	b.WriteString("\nThe harness does not lock files. Coordinate through commits: commit small and often, ")
	b.WriteString("never reset, rebase or rewrite the shared branch, expect files to change under you, and pull in nothing.")
	if !access.Writes() {
		b.WriteString("\n\nYou cannot write this worktree: put your findings in task documents or comments.")
	}
	return b.String()
}

// peerLine is one session of the list: its agent, mode, access and whether it
// still runs.
func peerLine(p sharedPeer) string {
	agent := p.Agent
	if agent == "" {
		agent = "claude"
	}
	mode := string(p.Mode)
	if mode == "" {
		mode = "default"
	}
	access := "access write"
	if !p.Access.Writes() {
		access = "access read-only"
	}
	state := "ended"
	if p.Running {
		state = "running"
	}
	return agent + " (mode " + mode + ", " + access + ", " + state + ")"
}
