package orchestration

import (
	"fmt"
	"strings"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"

	"github.com/rfbatista/llmkit/claude"
)

// How the agent learns of an encerramento por memória (ADR 0006): the harness
// cannot write into the output of a process it has just ended, so the reason
// goes into the PostToolUse answer of the Bash call that was running, or, when
// none was, into a user turn through the courier.

var (
	_ ports.MemorySink    = (*Service)(nil)
	_ ports.MemoryNotices = (*Service)(nil)
)

// memoryNotices is what the agent has yet to be told, and which Bash calls run.
type memoryNotices struct {
	mu      sync.Mutex
	running map[string]map[string]bool // session → running Bash call ids
	pending map[string][]domain.MemoryKill
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

// end drops the call; it answers the kills held for the session's calls when
// the call was one of the running ones.
func (n *memoryNotices) end(session, call string) []domain.MemoryKill {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.running[session][call] {
		return nil
	}
	delete(n.running[session], call)
	kills := n.pending[session]
	delete(n.pending, session)
	return kills
}

// hold keeps k for a running call's answer; false when no call runs.
func (n *memoryNotices) hold(k domain.MemoryKill) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.running[k.SessionID]) == 0 {
		return false
	}
	if n.pending == nil {
		n.pending = map[string][]domain.MemoryKill{}
	}
	n.pending[k.SessionID] = append(n.pending[k.SessionID], k)
	return true
}

// release answers and forgets the kills held for a session.
func (n *memoryNotices) release(session string) []domain.MemoryKill {
	n.mu.Lock()
	defer n.mu.Unlock()
	kills := n.pending[session]
	delete(n.pending, session)
	delete(n.running, session)
	return kills
}

// Sampled is not the notice's business; the events ticket takes it.
func (s *Service) Sampled(domain.MemorySample) {}

// Killed handles one encerramento por memória: the inbox is told, and so is the
// agent, by the running Bash call's answer or by the courier.
func (s *Service) Killed(k domain.MemoryKill) {
	s.announce(domain.SessionMemoryKilled{Kill: k})
	if s.memoryNotes.hold(k) {
		return
	}
	s.deliverMemoryNotice(k.SessionID, k)
}

func (s *Service) deliverMemoryNotice(sessionID string, kills ...domain.MemoryKill) {
	if len(kills) == 0 {
		return
	}
	key := fmt.Sprintf("memory:%s:%d", sessionID, kills[0].KilledAt.UnixNano())
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
		s.courier.enqueue(sessionID, parcel{key: fmt.Sprintf("memory:%s:%d", sessionID, kills[0].KilledAt.UnixNano()), text: memoryNoticeText(kills)})
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
