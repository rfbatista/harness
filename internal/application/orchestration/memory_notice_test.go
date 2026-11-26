//go:build !windows

package orchestration

import (
	"context"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/application/vshell"
	"operators-mcp/internal/domain"
)

func killOf(sessionID string) domain.MemoryKill {
	return domain.MemoryKill{Kind: domain.MemoryKilledKind, SessionID: sessionID, PID: 81234, Command: "node jest",
		RSSBytes: 3584 << 20, TreeRSS: 4200 << 20, Budget: 4 << 30, Signal: "SIGTERM", KilledAt: time.Now()}
}

// An ended process that belonged to no running call reaches the agent through
// the courier when its turn ends, once.
func TestAnEncerramentoOutsideAnyCallReachesTheAgentThroughTheCourier(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Prompt: "go"})

	svc.Killed(killOf(sess.ID))
	text, err := svc.TurnEnded(context.Background(), sess.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"node jest", "3.5 GB", "4 GB", "fewer workers"} {
		if !strings.Contains(text, want) {
			t.Errorf("the delivered turn lacks %q:\n%s", want, text)
		}
	}
	if again, _ := svc.TurnEnded(context.Background(), sess.ID, true); again != "" {
		t.Errorf("delivered twice: %q", again)
	}
}

// An ended process while a Bash call runs is told in that call's PostToolUse
// answer, and not again by the courier.
func TestAnEncerramentoDuringABashCallIsToldInThatCallsAnswerAndNotTwice(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Prompt: "go"})

	svc.BashStarted(sess.ID, "call-1")
	svc.Killed(killOf(sess.ID))
	if got := svc.BashEnded(sess.ID, "call-2"); got != "" {
		t.Errorf("another call's answer carries %q", got)
	}
	got := svc.BashEnded(sess.ID, "call-1")
	for _, want := range []string{"node jest", "3.5 GB", "4 GB", "fewer workers"} {
		if !strings.Contains(got, want) {
			t.Errorf("the PostToolUse reason lacks %q:\n%s", want, got)
		}
	}
	if again := svc.BashEnded(sess.ID, "call-1"); again != "" {
		t.Errorf("told twice: %q", again)
	}
	if text, _ := svc.TurnEnded(context.Background(), sess.ID, true); text != "" {
		t.Errorf("the courier repeated it: %q", text)
	}
}

// If the call never answers, the turn's end delivers the reason after all.
func TestAnEncerramentoDuringACallThatNeverAnswersIsDeliveredAtTurnEnd(t *testing.T) {
	svc, _ := newInteractiveService(t)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Prompt: "go"})
	svc.BashStarted(sess.ID, "call-1")
	svc.Killed(killOf(sess.ID))
	text, _ := svc.TurnEnded(context.Background(), sess.ID, false)
	if !strings.Contains(text, "node jest") {
		t.Errorf("turn end delivered %q, want the reason", text)
	}
}

// The memory hooks run on every Bash call, so they fail open: a short timeout
// and a command that succeeds whatever the route does, so the call goes on
// unchanged. They only ever add context; nothing in them denies.
func TestTheMemoryHooksFailOpenWithAShortTimeout(t *testing.T) {
	for event, groups := range MemoryNoticeHooks("http://127.0.0.1:1/api/sessions/s1/memory") {
		for _, g := range groups {
			for _, h := range g.Hooks {
				if !strings.Contains(h.Command, "-m 10") || !strings.HasSuffix(h.Command, "|| true") || h.Timeout <= 0 || h.Timeout > 15 {
					t.Errorf("%s hook is not fail-open with a short timeout: %+v", event, h)
				}
			}
		}
	}
}

// They sit beside the hooks the harness already installs for Bash and replace
// or reorder none.
func TestTheMemoryHooksMergeWithTheExistingBashHooks(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	svc.SessionMemoryURL = func(id string) string { return "http://127.0.0.1:1/api/sessions/" + id + "/memory" }
	svc.SessionArchitectureURL = func(id string) string { return "http://127.0.0.1:1/api/sessions/" + id + "/architecture" }
	_, launch := startHeadless(t, svc, w.task.ID)
	s := settingsOf(t, launch.Args)

	pre := s.hookCommands("PreToolUse", "Bash")
	post := s.hookCommands("PostToolUse", "Bash")
	index := func(cmds []string, part string) int {
		for i, c := range cmds {
			if strings.Contains(c, part) {
				return i
			}
		}
		return -1
	}
	if index(pre, "/shell/check?hook=PreToolUse") < 0 || index(pre, "/memory/hook?hook=PreToolUse") < 0 {
		t.Errorf("PreToolUse Bash hooks = %q, want the shell's and the memory notice's", pre)
	}
	if index(post, "/memory/hook?hook=PostToolUse") < 0 {
		t.Errorf("PostToolUse Bash hooks = %q, want the memory notice's", post)
	}
	if _, ok := s.hookCommand("PostToolUse", vshell.EditTools); !ok {
		t.Errorf("the edit proxy's commit hook is gone: %+v", s.Hooks)
	}
	if index(pre, "/shell/check?hook=PreToolUse") > index(pre, "/memory/hook?hook=PreToolUse") {
		t.Errorf("the memory hook was put ahead of the existing ones: %q", pre)
	}
}
