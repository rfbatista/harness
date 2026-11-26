package orchestration

import (
	"fmt"
	"strings"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"
)

// How the agent learns of an encerramento por memória (ADR 0006): the harness
// cannot write into the output of a process it has just ended, so the reason
// goes into the PostToolUse answer of the Bash call that was running, or, when
// none was, into a user turn through the courier.

var (
	_ ports.MemorySink    = (*Service)(nil)
	_ ports.MemoryEnding  = (*Service)(nil)
	_ ports.MemoryNotices = (*Service)(nil)
)

// memoryNotices is what the agent has yet to be told, and which Bash calls run.
type memoryNotices struct {
	mu      sync.Mutex
	running map[string]map[string]bool // session → running Bash call ids
	held    map[string][]*heldNotice
	// noticed are the pids whose ending the agent was already told of, until
	// the kill is confirmed, so it is never told twice.
	noticed map[string]map[int]bool
}

// heldNotice is a reason waiting for the answer of a call that was running when
// SIGTERM went out.
type heldNotice struct {
	kill  domain.MemoryKill
	calls map[string]bool
}

func (n *memoryNotices) start(session, call string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.running == nil {
		n.running = map[string]map[string]bool{}
	}
	if n.running[session] == nil {
		n.running[session] = map[string]bool{}
	}
	n.running[session][call] = true
}

// end drops the call; it answers the reasons held for it.
func (n *memoryNotices) end(session, call string) []domain.MemoryKill {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.running[session], call)
	var out []domain.MemoryKill
	var rest []*heldNotice
	for _, h := range n.held[session] {
		if h.calls[call] {
			out = append(out, h.kill)
			continue
		}
		rest = append(rest, h)
	}
	n.held[session] = rest
	return out
}

// note records that k's ending is being told of. It reports false when it was
// already (nothing more to do), and otherwise whether a call that is running
// holds it for its answer; if none, the courier must carry it.
func (n *memoryNotices) note(k domain.MemoryKill) (fresh, held bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.noticed == nil {
		n.noticed = map[string]map[int]bool{}
		n.held = map[string][]*heldNotice{}
	}
	if n.noticed[k.SessionID] == nil {
		n.noticed[k.SessionID] = map[int]bool{}
	}
	if n.noticed[k.SessionID][k.PID] {
		return false, false
	}
	n.noticed[k.SessionID][k.PID] = true
	if len(n.running[k.SessionID]) == 0 {
		return true, false
	}
	calls := map[string]bool{}
	for c := range n.running[k.SessionID] {
		calls[c] = true
	}
	n.held[k.SessionID] = append(n.held[k.SessionID], &heldNotice{kill: k, calls: calls})
	return true, true
}

// confirmed forgets that k's ending is pending: the kill is on record.
func (n *memoryNotices) confirmed(k domain.MemoryKill) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.noticed[k.SessionID], k.PID)
}

// release answers and forgets the reasons still held for a session: the calls
// they waited on never answered.
func (n *memoryNotices) release(session string) []domain.MemoryKill {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []domain.MemoryKill
	for _, h := range n.held[session] {
		out = append(out, h.kill)
	}
	delete(n.held, session)
	delete(n.running, session)
	return out
}

// forget drops everything kept for a session that has ended.
func (n *memoryNotices) forget(session string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.held, session)
	delete(n.running, session)
	delete(n.noticed, session)
}

// Ending is the vigia's SIGTERM: the agent is told now, in the answer of the Bash
// call running at this moment, or through the courier when none is.
func (s *Service) Ending(k domain.MemoryKill) {
	fresh, held := s.memoryNotes.note(k)
	if fresh && !held {
		s.deliverMemoryNotice(k.SessionID, k)
	}
}

// Killed handles one encerramento por memória once it is confirmed: the
// session's stream and the project feed carry it and the inbox is told. The
// agent was told at SIGTERM; one that was not (no Ending came) is told now.
func (s *Service) Killed(k domain.MemoryKill) {
	s.publishMemoryKill(k)
	s.announce(domain.SessionMemoryKilled{Kill: k})
	s.Ending(k)
	s.memoryNotes.confirmed(k)
}

func (s *Service) deliverMemoryNotice(sessionID string, kills ...domain.MemoryKill) {
	if len(kills) == 0 {
		return
	}
	key := fmt.Sprintf("memory:%s:%d:%d", sessionID, kills[0].PID, kills[0].KilledAt.UnixNano())
	_, _ = s.Deliver(sessionID, key, memoryNoticeText(kills), nil)
}

// BashStarted records a Bash call of the session as running.
func (s *Service) BashStarted(sessionID, callID string) { s.memoryNotes.start(sessionID, callID) }

// BashEnded is the PostToolUse answer for a Bash call: the reason of the
// encerramentos that happened while it ran, "" when none.
func (s *Service) BashEnded(sessionID, callID string) string {
	kills := s.memoryNotes.end(sessionID, callID)
	if len(kills) == 0 {
		return ""
	}
	return memoryNoticeText(kills)
}

// flushMemoryNotices hands what a turn's end finds still held to the courier:
// the call it waited on never answered.
func (s *Service) flushMemoryNotices(sessionID string) {
	if kills := s.memoryNotes.release(sessionID); len(kills) > 0 {
		s.courier.enqueue(sessionID, parcel{key: fmt.Sprintf("memory:%s:%d:%d", sessionID, kills[0].PID, kills[0].KilledAt.UnixNano()), text: memoryNoticeText(kills)})
	}
}

// memoryNoticeText says what was ended and why, and what to do about it.
func memoryNoticeText(kills []domain.MemoryKill) string {
	var b strings.Builder
	for i, k := range kills {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[memory budget] The harness ended `%s` (pid %d, %s) with %s because this session's processes used %s of its %s memory budget. It was the heaviest process; this is not a failing test. Run fewer workers (--maxWorkers=$HARNESS_TEST_WORKERS) or a smaller part of the suite instead of retrying the same command.",
			k.Command, k.PID, formatBytes(k.RSSBytes), k.Signal, formatBytes(k.TreeRSS), formatBytes(k.Budget))
	}
	return b.String()
}

func formatBytes(n int64) string {
	s := fmt.Sprintf("%.1f", float64(n)/(1<<30))
	return strings.TrimSuffix(s, ".0") + " GB"
}

// withMemoryNotice adds the Bash hooks that carry the notice, after the ones
// the session already has. Without a URL it changes nothing.
func (s *Service) withMemoryNotice(id string, cfg *llmkit.SessionConfig) {
	if s.SessionMemoryURL == nil {
		return
	}
	if dc, ok := withHooks(cfg.Driver, MemoryNoticeHooks(s.SessionMemoryURL(id))); ok {
		cfg.Driver = dc
	}
}

// MemoryNoticeHooks are the claude settings hooks that tell the service when a
// Bash call starts and ends, and carry its end's answer back as context. A
// harness that cannot be reached changes nothing.
func MemoryNoticeHooks(url string) map[claude.HookEvent][]claude.HookMatcher {
	hook := func(event string) []claude.Hook {
		curl := "curl -fsS -m 10 -X POST -H 'Content-Type: application/json'" +
			" --data-binary @- '" + url + "/hook?hook=" + event + "' 2>/dev/null || true"
		return []claude.Hook{{Type: "command", Command: curl, Timeout: 15}}
	}
	return map[claude.HookEvent][]claude.HookMatcher{
		claude.HookPreToolUse:  {{Matcher: "Bash", Hooks: hook("PreToolUse")}},
		claude.HookPostToolUse: {{Matcher: "Bash", Hooks: hook("PostToolUse")}},
	}
}
