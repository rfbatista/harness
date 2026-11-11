package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"testing"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The sessions a restart interrupted, in the browser (ADR 0082, ADR 0083): the
// stopped session's row says so, the inbox item lists the sessions and
// resumes them all with one click, and the states update live.

// interruptedResume is the server's Resume all interrupted as the web client
// sees it: POST /api/notifications/{id}/resume_interrupted. It resumes the
// sessions the item lists that are still interrupted or failed, fails the
// ones in failing, then streams the changed notification and answers it. With
// hold set, the answer waits until release.
type interruptedResume struct {
	mu      sync.Mutex
	failing map[string]string
	calls   int
	hold    chan struct{}
	// resumed is called with each session the click resumes.
	resumed func(sessionID string)
}

func (f *fakeInbox) resumeInterrupted(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	var n *domain.Notification
	for _, x := range f.list {
		if x.ID == r.PathValue("id") {
			n = x
		}
	}
	if n == nil || n.Kind != domain.NotificationSessionsInterrupted {
		f.mu.Unlock()
		http.Error(w, `{"code":"NOTIFICATION_NOT_FOUND","error":"no such notification"}`, http.StatusNotFound)
		return
	}
	rs := f.resume
	rs.mu.Lock()
	rs.calls++
	failing, hold := rs.failing, rs.hold
	rs.mu.Unlock()
	results := []map[string]string{}
	left := 0
	for i, s := range n.InterruptedSessions {
		out := map[string]string{"session_id": s.SessionID}
		switch {
		case s.State == domain.InterruptedSessionResumed:
			out["outcome"] = "skipped"
		case failing[s.SessionID] != "":
			n.InterruptedSessions[i].State, n.InterruptedSessions[i].Reason = domain.InterruptedSessionFailed, failing[s.SessionID]
			out["outcome"], out["reason"] = "failed", failing[s.SessionID]
			left++
		default:
			n.InterruptedSessions[i].State, n.InterruptedSessions[i].Reason = domain.InterruptedSessionResumed, ""
			out["outcome"] = "resumed"
			if rs.resumed != nil {
				rs.resumed(s.SessionID)
			}
		}
		results = append(results, out)
	}
	if left == 0 {
		now := time.Now()
		n.ResolvedAt = &now
	}
	change := ports.NotificationChange{Changed: []*domain.Notification{n}, UnreadCount: f.unread()}
	f.mu.Unlock()
	b, _ := json.Marshal(change)
	f.stream.mu.Lock()
	for _, c := range f.stream.conns {
		c <- b
	}
	f.stream.mu.Unlock()
	if hold != nil {
		<-hold
	}
	writeJSON(w, map[string]any{"notification": n, "results": results})
}

const (
	restartLines = `[...document.querySelectorAll('main .notification .interrupted-sessions li')].map(li => li.querySelector('a').textContent.trim() + '|' + li.querySelector('.state').textContent.trim() + '|' + (li.querySelector('.reason')?.checkVisibility() ? li.querySelector('.reason').textContent.trim() : '')).join('\n')`
	resumeButton = `[...document.querySelectorAll('main .notification button.resume-all')].find(b => b.checkVisibility())`
	taskRowChip  = `[...document.querySelectorAll('.list [role=group] .row .badge')].map(b => b.textContent.trim()).filter(Boolean).join(',')`
	itemSettled  = `document.querySelector('main .notification .interrupted-sessions')?.closest('.notification')?.dataset.state === 'settled'`
)

func TestResumeAllInterruptedInTheBrowser(t *testing.T) {
	now := time.Now()
	cut := now.Add(-time.Minute)
	stopped := &domain.Session{ID: "s1", ProjectID: "p1", TicketID: "t-cur", Task: "Redesign sidebar", Status: domain.SessionStopped, Interactive: true, InterruptedAt: &cut, UpdatedAt: now}
	notification := &domain.Notification{
		ID: "n-restart", ProjectID: "p1", ProjectName: "harness", Source: domain.NotificationFromSystem, Kind: domain.NotificationSessionsInterrupted,
		Level: domain.NotificationAction, Title: "3 sessions interrupted by a restart", Link: "/notifications", CreatedAt: now.Add(-time.Minute),
		Body: "Redesign sidebar · go-developer\nFix flaky tests · devops\nNo task · architect",
		InterruptedSessions: []domain.InterruptedSession{
			{SessionID: "s1", ProjectID: "p1", TaskID: "t-cur", TaskTitle: "Redesign sidebar", AgentName: "go-developer", Link: "/projects/p1/tasks/t-cur?session=s1", State: domain.InterruptedSessionInterrupted},
			{SessionID: "s2", ProjectID: "p1", TaskID: "t-flaky", TaskTitle: "Fix flaky tests", AgentName: "devops", Link: "/projects/p1/tasks/t-flaky?session=s2", State: domain.InterruptedSessionInterrupted},
			{SessionID: "s3", ProjectID: "p1", AgentName: "architect", Link: "/projects/p1?session=s3", State: domain.InterruptedSessionInterrupted},
		},
	}
	h := newInboxHarnessWith(t, []*domain.Notification{notification}, func(d *Deps) {
		d.Sessions = fakeSessions{[]*domain.Session{stopped}}
	})
	resume := &interruptedResume{failing: map[string]string{"s3": "the worktree is gone"}, hold: make(chan struct{}), resumed: func(id string) {
		if id == "s1" {
			stopped.InterruptedAt = nil // the session row loses its label
		}
	}}
	h.inbox.resume = resume

	// The session's row says it was interrupted by the restart.
	var chips string
	h.run(t, "the session row", h.openLive("/projects/p1/tasks/t-cur"), chromedp.Evaluate(taskRowChip, &chips))
	if chips != "interrupted by restart" {
		t.Errorf("the row's chip = %q, want the interrupted label", chips)
	}

	// The inbox item lists the sessions; its button waits for the answer.
	var lines, label string
	var busy bool
	h.run(t, "the inbox item",
		h.openReady("/notifications"),
		railPoll(`!!(`+resumeButton+`)`),
		chromedp.Evaluate(restartLines, &lines),
		chromedp.Evaluate(`(`+resumeButton+`).textContent.trim()`, &label),
		chromedp.Evaluate(`(`+resumeButton+`).click(); true`, nil),
		railPoll(`(`+resumeButton+`)?.disabled === true`),
		chromedp.Evaluate(`(`+resumeButton+`).getAttribute('aria-busy') === 'true'`, &busy),
	)
	if want := "Redesign sidebar · go-developer|interrupted|\nFix flaky tests · devops|interrupted|\nNo task · architect|interrupted|"; lines != want {
		t.Errorf("listed sessions:\n%s\nwant:\n%s", lines, want)
	}
	if label != "Resume all interrupted" || !busy {
		t.Errorf("button %q, busy while in flight = %v", label, busy)
	}

	// The answer lets go; the states follow it, the failed one with its reason.
	close(resume.hold)
	var after string
	h.run(t, "after the click",
		railPoll(`!(`+resumeButton+`)?.disabled`),
		railPoll(`(`+restartLines+`).includes('resumed')`),
		chromedp.Evaluate(restartLines, &after),
	)
	if want := "Redesign sidebar · go-developer|resumed|\nFix flaky tests · devops|resumed|\nNo task · architect|failed|the worktree is gone"; after != want {
		t.Errorf("after Resume all:\n%s\nwant:\n%s", after, want)
	}

	// The next click retries only what is still marked, and settles the item.
	resume.mu.Lock()
	resume.failing, resume.hold = map[string]string{}, nil
	resume.mu.Unlock()
	h.run(t, "the retry",
		chromedp.Evaluate(`(`+resumeButton+`).click(); true`, nil),
		railPoll(itemSettled),
		railPoll(`!(`+resumeButton+`)`),
	)
	if resume.calls != 2 {
		t.Errorf("resume called %d times, want 2", resume.calls)
	}

	// The session's row no longer says interrupted.
	h.run(t, "the row after", h.openLive("/projects/p1/tasks/t-cur"), chromedp.Evaluate(taskRowChip, &chips))
	if strings.Contains(chips, "interrupted") {
		t.Errorf("after the resume the row's chip = %q", chips)
	}
	h.noErrors(t)
}
