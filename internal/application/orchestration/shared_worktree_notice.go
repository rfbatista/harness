package orchestration

import (
	"context"
	"log/slog"
	"strings"

	"github.com/rfbatista/harnesskit/errs"
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
	var peers []sharedPeer
	for _, o := range s.sessionsOnWorktree(ctx, workspaceID) {
		if o.ID != id {
			peers = append(peers, s.sharedPeerOf(ctx, o))
		}
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
	state := "ended"
	if p.Running {
		state = "running"
	}
	return peerName(p) + " (" + peerMode(p) + ", " + peerAccess(p) + ", " + state + ")"
}

func peerName(p sharedPeer) string {
	if p.Agent == "" {
		return "claude"
	}
	return p.Agent
}

func peerMode(p sharedPeer) string {
	if p.Mode == "" {
		return "mode default"
	}
	return "mode " + string(p.Mode)
}

func peerAccess(p sharedPeer) string {
	if !p.Access.Writes() {
		return "access read-only"
	}
	return "access write"
}

// sharedPeerOf is a session as another session's list names it.
func (s *Service) sharedPeerOf(ctx context.Context, sess *domain.Session) sharedPeer {
	return sharedPeer{Agent: s.agentName(ctx, sess.AgentID), Mode: sess.Mode, Access: sess.Access, Running: !sess.Status.IsTerminal()}
}

// worktreeJoinedTurn is what a running session on a worktree is told when a
// session starts on it.
func worktreeJoinedTurn(who sharedPeer, branch string) string {
	meaning := "It may change files here: expect them to change under you."
	if !who.Access.Writes() {
		meaning = "It only reads: nothing changes in the worktree because of it."
	}
	return "[worktree · joined] " + peerName(who) + " (" + peerMode(who) + ", " + peerAccess(who) + ") joined your worktree on " + branch + ". " + meaning
}

// worktreeLeftTurn is what a running session on a worktree is told when
// another session on it ends, is stopped or is archived.
func worktreeLeftTurn(who sharedPeer, branch string) string {
	return "[worktree · left] " + peerName(who) + " (" + peerMode(who) + ") left your worktree on " + branch + "."
}

// turnSink puts a turn into a session: the courier, or the test's stand-in.
func (s *Service) turnSink() func(id, key, text string) error {
	if s.deliverUpdate != nil {
		return s.deliverUpdate
	}
	return func(id, key, text string) error {
		_, err := s.Deliver(id, key, text, nil)
		return err
	}
}

// announceJoined gives every other running session on the worktree the one
// turn that says sess joined it. A session's own architect is not told: it
// started the session and knows.
func (s *Service) announceJoined(ctx context.Context, sess *domain.Session) {
	deliver := s.turnSink()
	who := s.sharedPeerOf(ctx, sess)
	for _, o := range s.sessionsOnWorktree(ctx, sess.WorkspaceID) {
		if o.ID == sess.ID || o.ID == sess.ParentSessionID || o.Status.IsTerminal() {
			continue
		}
		if err := deliver(o.ID, "worktree-joined:"+sess.ID, worktreeJoinedTurn(who, sess.Branch)); err != nil && errs.Code(err) != "SESSION_NOT_RUNNING" {
			slog.Warn("deliver a worktree joined turn", "session", o.ID, "err", err)
		}
	}
}

// sessionsOnWorktree is every session recorded on the worktree.
func (s *Service) sessionsOnWorktree(ctx context.Context, workspaceID string) []*domain.Session {
	if workspaceID == "" {
		return nil
	}
	others, err := s.sessions.Query(ctx, ports.SessionFilter{WorkspaceID: workspaceID})
	if err != nil {
		slog.Warn("read the sessions of a worktree", "workspace", workspaceID, "err", err)
		return nil
	}
	return others
}

// announceEnded publishes the end of a session and tells the other running
// sessions on its worktree it left. A session leaves once, whichever way its
// end is recorded and however many times; resuming it lets it leave again.
func (s *Service) announceEnded(id string, status domain.SessionStatus) {
	s.announce(domain.SessionEnded{SessionID: id, Status: status})
	s.mu.Lock()
	if s.leftTold == nil {
		s.leftTold = map[string]bool{}
	}
	told := s.leftTold[id]
	s.leftTold[id] = true
	s.mu.Unlock()
	if told {
		return
	}
	sess := s.sessions.Get(id)
	if sess == nil {
		return
	}
	ctx := context.Background()
	deliver := s.turnSink()
	who := s.sharedPeerOf(ctx, sess)
	for _, o := range s.sessionsOnWorktree(ctx, sess.WorkspaceID) {
		if o.ID == id || o.Status.IsTerminal() {
			continue
		}
		if err := deliver(o.ID, "worktree-left:"+id, worktreeLeftTurn(who, sess.Branch)); err != nil && errs.Code(err) != "SESSION_NOT_RUNNING" {
			slog.Warn("deliver a worktree left turn", "session", o.ID, "err", err)
		}
	}
}
