//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// A checkpoint (ADR 0112) runs over the stand-in claude of product_owner_e2e_test.go.
// It reports as input tokens whatever the file "usage" beside it holds, which the
// server reads as the size of the thread's context; the mode's model (sonnet)
// has a window of 200,000 tokens, so 130,000 is past 60% of it.

const (
	contextBelowCheckpoint = 1000
	contextPastCheckpoint  = 130000
)

// setContext makes the stand-in report this many tokens of context from now on.
func (e *poE2E) setContext(tokens int) {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.fakeDir, "usage"), []byte(strconv.Itoa(tokens)), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// gate creates (on) or removes (off) a file of the stand-in that holds or fails
// its summary turn.
func (e *poE2E) gate(name string, on bool) {
	e.t.Helper()
	path := filepath.Join(e.fakeDir, name)
	if on {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			e.t.Fatal(err)
		}
		return
	}
	_ = os.Remove(path)
}

func kinds(items []poItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Kind
	}
	return out
}

func checkpointsOf(items []poItem) []poItem {
	var out []poItem
	for _, it := range items {
		if it.Kind == "checkpoint" {
			out = append(out, it)
		}
	}
	return out
}

type poListed struct {
	Threads []struct {
		ThreadID    string `json:"thread_id"`
		Checkpoints int    `json:"checkpoints"`
	} `json:"threads"`
}

func (e *poE2E) checkpointsListed(threadID string) int {
	e.t.Helper()
	var out poListed
	if status, code := e.get("product_owner_threads?project_id="+e.project, &out); status != 200 {
		e.t.Fatalf("GET threads = %d %s", status, code)
	}
	for _, th := range out.Threads {
		if th.ThreadID == threadID {
			return th.Checkpoints
		}
	}
	e.t.Fatalf("thread %s is not listed", threadID)
	return 0
}

func TestATurnThatEndsBelowTheCheckpointSizeDoesNotCheckpoint(t *testing.T) {
	e := newPOE2E(t)
	e.setContext(contextBelowCheckpoint)
	threadID, _ := e.startThread("hello")
	th := e.waitTurn(threadID, "idle")
	if got := checkpointsOf(th.Items); len(got) != 0 {
		t.Errorf("items = %v, want no checkpoint", kinds(th.Items))
	}
	if n := e.checkpointsListed(threadID); n != 0 {
		t.Errorf("checkpoints = %d, want 0", n)
	}
	if l := e.launches(); len(l) != 1 {
		t.Errorf("launches = %q, want the one", l)
	}
}

func TestAThreadPastTheCheckpointSizeCheckpointsAfterItsAnswerAndGoesOnInANewConversation(t *testing.T) {
	e := newPOE2E(t)
	e.setContext(contextPastCheckpoint)
	threadID, _ := e.startThread("we decided to export tasks as csv")
	th := e.waitTurn(threadID, "idle")

	// The answer, then the checkpoint carrying the summary; the summary turn's
	// own text is not an item of the thread.
	if got, want := kinds(th.Items), "message message checkpoint"; strings.Join(got, " ") != want {
		t.Fatalf("items = %v, want %s", got, want)
	}
	cp := th.Items[2]
	if !strings.Contains(cp.Summary, "answer to: Checkpoint:") {
		t.Errorf("the checkpoint's summary = %q, want the product owner's answer to the summary turn", cp.Text)
	}
	if n := e.checkpointsListed(threadID); n != 1 {
		t.Errorf("checkpoints = %d, want 1", n)
	}

	// The thread moved to a new session, a fresh claude conversation.
	newSession := th.Thread.SessionID
	eventuallyPO(t, "the second conversation to start", func() string {
		if l := e.launches(); len(l) != 2 {
			return "launches = " + strings.Join(l, "|")
		}
		return ""
	})
	launches := e.launches()
	if strings.Contains(launches[1], "--resume") || !strings.Contains(launches[1], "--session-id "+newSession) {
		t.Errorf("second launch = %.300s, want a fresh conversation %s", launches[1], newSession)
	}
	if strings.Contains(launches[0], "--session-id "+newSession) {
		t.Errorf("the thread still runs in its first conversation")
	}
	if !strings.Contains(launches[1], "answer to: Checkpoint:") {
		t.Errorf("the new conversation's brief lacks the summary: %.300s", launches[1])
	}
	sess := e.sess.Get(newSession)
	if sess == nil || sess.ProjectID != e.project || sess.Mode != domain.SessionModeProductOwner {
		t.Fatalf("new session = %+v", sess)
	}

	// The person's next message is answered by the new conversation, and the
	// whole thread reads on from the checkpoint.
	e.setContext(contextBelowCheckpoint)
	if status, code := e.send(threadID, "and what next"); status != 200 {
		t.Fatalf("send = %d %s", status, code)
	}
	th = e.waitTurn(threadID, "idle")
	if got, want := kinds(th.Items), "message message checkpoint message message"; strings.Join(got, " ") != want {
		t.Errorf("items = %v, want %s", got, want)
	}
	if th.Thread.SessionID != newSession {
		t.Errorf("session = %s, want it to stay %s", th.Thread.SessionID, newSession)
	}
	if l := e.launches(); len(l) != 2 {
		t.Errorf("launches = %q, want the new process kept for the next message", l)
	}
}

func TestAMessageSentWhileTheThreadCheckpointsIsRefusedBusyAndNothingIsLost(t *testing.T) {
	e := newPOE2E(t)
	e.setContext(contextPastCheckpoint)
	e.gate("summaryhold", true)
	threadID, _ := e.startThread("first")
	eventuallyPO(t, "the summary turn to be under way", func() string {
		b, _ := os.ReadFile(filepath.Join(e.fakeDir, "received"))
		if !strings.Contains(string(b), "Checkpoint:") {
			return "received = " + string(b)
		}
		return ""
	})
	if status, code := e.send(threadID, "too soon"); status != 409 || code != "THREAD_BUSY" {
		t.Errorf("send during the checkpoint = %d %s, want 409 THREAD_BUSY", status, code)
	}
	if got := e.thread(threadID).Thread.Turn; got != "running" {
		t.Errorf("turn = %s, want running", got)
	}
	e.gate("summaryhold", false)
	th := e.waitTurn(threadID, "idle")
	if len(checkpointsOf(th.Items)) != 1 {
		t.Errorf("items = %v, want one checkpoint", kinds(th.Items))
	}
	for _, it := range th.Items {
		if strings.Contains(it.Text, "too soon") {
			t.Errorf("a refused message was stored: %v", it)
		}
	}
	e.setContext(contextBelowCheckpoint)
	if status, code := e.send(threadID, "now"); status != 200 {
		t.Fatalf("send after the checkpoint = %d %s", status, code)
	}
	e.waitTurn(threadID, "idle")
}

func TestAFailedSummaryTurnLeavesTheThreadAsItWasAndTheCheckpointIsTriedAfterTheNextTurn(t *testing.T) {
	e := newPOE2E(t)
	e.setContext(contextPastCheckpoint)
	e.gate("summaryfail", true)
	threadID, _ := e.startThread("first")
	th := e.waitTurn(threadID, "idle")
	if got, want := kinds(th.Items), "message message failure"; strings.Join(got, " ") != want {
		t.Fatalf("items = %v, want %s", got, want)
	}
	if n := e.checkpointsListed(threadID); n != 0 {
		t.Errorf("checkpoints = %d, want 0", n)
	}
	sessionBefore := th.Thread.SessionID

	e.gate("summaryfail", false)
	if status, code := e.send(threadID, "second"); status != 200 {
		t.Fatalf("send = %d %s", status, code)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		th = e.thread(threadID)
		if th.Thread.Turn == "idle" && len(checkpointsOf(th.Items)) == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(checkpointsOf(th.Items)) != 1 || th.Thread.Turn != "idle" {
		t.Fatalf("turn %s items %v, want the checkpoint made after the next turn", th.Thread.Turn, kinds(th.Items))
	}
	if th.Thread.SessionID == sessionBefore {
		t.Errorf("the thread did not move to a new conversation")
	}
}
