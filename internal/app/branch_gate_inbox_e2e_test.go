//go:build !windows

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// What a branch request puts on the person's side: the inbox, the project feed
// and the REST list (contract "Harness server ↔ Web UI — branch requests and
// the Branches list" §2, §4 and §5).

// branchNotes are the inbox notifications about branch requests, and every
// notification about the session's approvals.
func (g *branchGate) notes() (branch []*domain.Notification, waiting int) {
	g.t.Helper()
	page, err := g.shellE2E.notes.ListNotifications(g.ctx, ports.NotificationQuery{Limit: 200})
	if err != nil {
		g.t.Fatal(err)
	}
	for _, n := range page.Notifications {
		if n.SessionID != g.id {
			continue
		}
		switch n.Kind {
		case domain.NotificationKind("branch_request"):
			branch = append(branch, n)
		case domain.NotificationSessionWaiting:
			waiting++
		}
	}
	return branch, waiting
}

// feedOf follows the project feed and collects the branch_request changes.
type feedOf struct {
	mu   sync.Mutex
	seen []json.RawMessage
}

func (g *branchGate) followFeed() *feedOf {
	g.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	g.t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", g.srv.URL+"/api/events?project_id="+g.project, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	f := &feedOf{}
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var change map[string]json.RawMessage
			if json.Unmarshal([]byte(data), &change) == nil {
				if raw, ok := change["branch_request"]; ok {
					f.mu.Lock()
					f.seen = append(f.seen, raw)
					f.mu.Unlock()
				}
			}
		}
	}()
	return f
}

// saw waits for a branch_request change in state for the request.
func (f *feedOf) saw(t *testing.T, id, state string) {
	t.Helper()
	eventually(t, "a "+state+" branch_request on the feed", func() error {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, raw := range f.seen {
			var r struct {
				ID    string `json:"id"`
				State string `json:"state"`
			}
			if json.Unmarshal(raw, &r) == nil && r.ID == id && r.State == state {
				return nil
			}
		}
		return http.ErrNotSupported
	})
}

func TestABranchRequestIsOneActionNotificationAndItsFeedFollowsItsLife(t *testing.T) {
	g := startBranchGate(t)
	feed := g.followFeed()
	hook := g.askHook("git checkout -b feature-x", "")
	req := g.pending(1)[0]
	feed.saw(t, req.ID, "pending")

	var notes []*domain.Notification
	eventually(t, "the branch notification", func() error {
		notes, _ = g.notes()
		if len(notes) != 1 {
			return http.ErrNotSupported
		}
		return nil
	})
	n := notes[0]
	if n.Level != domain.NotificationAction || !strings.HasPrefix(n.Title, "Branch: create feature-x") ||
		!strings.Contains(n.Body, g.id) || !strings.Contains(n.Body, g.repo) || !strings.Contains(n.Link, "session="+g.id) || n.ResolvedAt != nil {
		t.Errorf("notification = %+v", n)
	}
	if _, waiting := g.notes(); waiting != 0 {
		t.Errorf("%d \"session is waiting\" notifications beside the branch one", waiting)
	}

	g.answer(req.ID, "approve", "")
	answered(t, hook)
	feed.saw(t, req.ID, "approved")
	if after, _ := g.notes(); len(after) != 1 || after[0].ResolvedAt == nil {
		t.Errorf("an answer must post nothing and settle the notification: %+v", after)
	}
	if r := g.git("git checkout -b feature-x"); r.code != 0 {
		t.Fatalf("the approved command: %+v", r)
	}
	feed.saw(t, req.ID, "used")

	// A pending request expires with its session.
	_, gone := g.askHookCancelable("git branch b", "")
	second := g.pending(1)[0]
	gone()
	if _, err := g.orch.EndInteractive(g.ctx, g.id, 0, true); err != nil {
		t.Fatal(err)
	}
	feed.saw(t, second.ID, "expired")
}

func TestTheBranchRequestListFiltersBySessionTaskAndState(t *testing.T) {
	g := startBranchGate(t)
	hook := g.askHook("git branch a", "")
	first := g.pending(1)[0]
	g.answer(first.ID, "approve", "")
	answered(t, hook)
	g.askHook("git branch b", "")
	second := g.pending(1)[0]

	all := g.requests("")
	if len(all) != 2 || all[0].ID != second.ID || all[1].ID != first.ID {
		t.Fatalf("session list = %+v, want newest first", all)
	}
	if got := g.requests("pending"); len(got) != 1 || got[0].ID != second.ID {
		t.Errorf("pending = %+v", got)
	}
	var byTask struct {
		Requests []map[string]any `json:"requests"`
	}
	res, err := http.Get(g.srv.URL + "/api/branch_requests?task_id=" + *first.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&byTask); err != nil || len(byTask.Requests) != 2 {
		t.Fatalf("by task: %v %+v", err, byTask)
	}
	for _, key := range []string{"id", "session_id", "task_id", "repository_id", "session_branch", "action", "branch", "new_name", "start_point", "remote", "remote_branch", "commits", "command", "via", "reason", "state", "note", "requested_at", "answered_at"} {
		if _, ok := byTask.Requests[0][key]; !ok {
			t.Errorf("a request has no %q: %v", key, byTask.Requests[0])
		}
	}
	other, err := http.Get(g.srv.URL + "/api/branch_requests?task_id=nope")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Body.Close()
	var none struct {
		Requests []any `json:"requests"`
	}
	if err := json.NewDecoder(other.Body).Decode(&none); err != nil || len(none.Requests) != 0 {
		t.Errorf("another task's requests: %v %+v", err, none)
	}
}
