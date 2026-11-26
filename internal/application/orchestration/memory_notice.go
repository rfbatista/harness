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
	_ ports.MemorySink       = (*Service)(nil)
	_ ports.MemoryEnding     = (*Service)(nil)
	_ ports.MemoryKillFailed = (*Service)(nil)
	_ ports.MemoryNotices    = (*Service)(nil)
)

// memoryNotices is what the agent has yet to be told, and which Bash calls run.
type memoryNotices struct {
	mu      sync.Mutex
	running map[string]map[string]bool // session → running Bash call ids
	held    map[string][]*heldNotice
	// noticed are the pids whose ending the agent was told of, until the kill is
	// confirmed, so it is never told twice; each keeps the calls that were running
	// at SIGTERM, which a later follow-up may still reach.
	noticed map[string]map[int]map[string]bool
	// failedTold are the pids whose failure the agent was told of: once each.
	failedTold map[string]map[int]bool
}

// heldNotice is a reason waiting for the answer of a call that was running when
// SIGTERM went out. failed makes it the follow-up: the process could not be ended.
type heldNotice struct {
	kill   domain.MemoryKill
	calls  map[string]bool
	failed bool
}

// memNotice is one reason the agent is told.
type memNotice struct {
	kill   domain.MemoryKill
	failed bool
}

func notices(held []*heldNotice) []memNotice {
	var out []memNotice
	for _, h := range held {
		out = append(out, memNotice{kill: h.kill, failed: h.failed})
	}
	return out
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
func (n *memoryNotices) end(session, call string) []memNotice {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.running[session], call)
	var out, rest []*heldNotice
	for _, h := range n.held[session] {
		if h.calls[call] {
			out = append(out, h)
			continue
		}
		rest = append(rest, h)
	}
	n.held[session] = rest
	return notices(out)
}

func (n *memoryNotices) init() {
	if n.noticed == nil {
		n.noticed = map[string]map[int]map[string]bool{}
		n.held = map[string][]*heldNotice{}
		n.failedTold = map[string]map[int]bool{}
	}
}

// note records that k's ending is being told of. It reports false when it was
// already (nothing more to do), and otherwise whether a call that is running
// holds it for its answer; if none, the courier must carry it.
func (n *memoryNotices) note(k domain.MemoryKill) (fresh, held bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.init()
	if n.noticed[k.SessionID] == nil {
		n.noticed[k.SessionID] = map[int]map[string]bool{}
	}
	if _, told := n.noticed[k.SessionID][k.PID]; told {
		return false, false
	}
	calls := map[string]bool{}
	for c := range n.running[k.SessionID] {
		calls[c] = true
	}
	n.noticed[k.SessionID][k.PID] = calls
	if len(calls) == 0 {
		return true, false
	}
	n.held[k.SessionID] = append(n.held[k.SessionID], &heldNotice{kill: k, calls: calls})
	return true, true
}

// failure records that the vigia gave up on k's process, and says how the agent
// is told, once: false when it already was; otherwise held when a call that was
// open at SIGTERM still is (its answer carries it, in place of the ending it has
// not yet been told), and else it goes through the courier.
func (n *memoryNotices) failure(k domain.MemoryKill) (fresh, held bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.init()
	if n.failedTold[k.SessionID] == nil {
		n.failedTold[k.SessionID] = map[int]bool{}
	}
	if n.failedTold[k.SessionID][k.PID] {
		return false, false
	}
	n.failedTold[k.SessionID][k.PID] = true
	for _, h := range n.held[k.SessionID] {
		if h.kill.PID == k.PID && !h.failed {
			h.kill, h.failed = k, true // "ended" was never said: say only that it could not be
			return true, true
		}
	}
	open := map[string]bool{}
	for c := range n.noticed[k.SessionID][k.PID] {
		if n.running[k.SessionID][c] {
			open[c] = true
		}
	}
	if len(open) == 0 {
		return true, false
	}
	n.held[k.SessionID] = append(n.held[k.SessionID], &heldNotice{kill: k, calls: open, failed: true})
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
func (n *memoryNotices) release(session string) []memNotice {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := notices(n.held[session])
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
	delete(n.failedTold, session)
}

// Ending is the vigia's SIGTERM: the agent is told now, in the answer of the Bash
// call running at this moment, or through the courier when none is.
func (s *Service) Ending(k domain.MemoryKill) {
	fresh, held := s.memoryNotes.note(k)
	if fresh && !held {
		s.deliverMemoryNotice(k.SessionID, memNotice{kill: k})
	}
}

// KillFailed is the vigia giving up on a process that survived SIGKILL: no kill
// is recorded, the person is told it could not be ended, and the agent gets one
// follow-up in the same way it got the first notice.
func (s *Service) KillFailed(k domain.MemoryKill) {
	s.announce(domain.SessionMemoryKillFailed{Kill: k})
	fresh, held := s.memoryNotes.failure(k)
	if fresh && !held {
		s.deliverMemoryNotice(k.SessionID, memNotice{kill: k, failed: true})
	}
	s.memoryNotes.confirmed(k)
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

func (s *Service) deliverMemoryNotice(sessionID string, ns ...memNotice) {
	if len(ns) == 0 {
		return
	}
	_, _ = s.Deliver(sessionID, noticeKey(sessionID, ns[0]), memoryNoticeText(ns), nil)
}

func noticeKey(sessionID string, n memNotice) string {
	kind := "memory"
	if n.failed {
		kind = "memory-failed"
	}
	return fmt.Sprintf("%s:%s:%d:%d", kind, sessionID, n.kill.PID, n.kill.KilledAt.UnixNano())
}

// BashStarted records a Bash call of the session as running.
func (s *Service) BashStarted(sessionID, callID string) { s.memoryNotes.start(sessionID, callID) }

// BashEnded is the PostToolUse answer for a Bash call: the reason of the
// encerramentos that happened while it ran, "" when none.
func (s *Service) BashEnded(sessionID, callID string) string {
	ns := s.memoryNotes.end(sessionID, callID)
	if len(ns) == 0 {
		return ""
	}
	return memoryNoticeText(ns)
}

// flushMemoryNotices hands what a turn's end finds still held to the courier:
// the call it waited on never answered.
func (s *Service) flushMemoryNotices(sessionID string) {
	if ns := s.memoryNotes.release(sessionID); len(ns) > 0 {
		s.courier.enqueue(sessionID, parcel{key: noticeKey(sessionID, ns[0]), text: memoryNoticeText(ns)})
	}
}

// memoryNoticeText says what was ended and why, or that it could not be ended,
// and what to do about it.
func memoryNoticeText(ns []memNotice) string {
	var b strings.Builder
	for i, n := range ns {
		if i > 0 {
			b.WriteString("\n")
		}
		k := n.kill
		if n.failed {
			fmt.Fprintf(&b, "[memory budget] The harness could not end `%s` (pid %d): it still holds %s after SIGKILL, so this session stays over its %s memory budget. Stop or shrink that work yourself (fewer workers, a smaller part of the suite); the harness will not retry it.",
				k.Command, k.PID, formatBytes(k.RSSBytes), formatBytes(k.Budget))
			continue
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
