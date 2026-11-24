//go:build !windows

package app

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The daily digest (ADR 0113): a one-off product-owner run writes its text.

func TestAOneOffProductOwnerRunAnswersWithNoThreadAndStaysOutOfEverySessionSurface(t *testing.T) {
	e := newPOE2E(t)

	text, err := e.po.RunOnce(e.ctx, e.project, "write the digest")
	if err != nil {
		t.Fatal(err)
	}
	if text != "answer to: write the digest" {
		t.Fatalf("the run answered %q, want the fake's answer", text)
	}

	launches := e.launches()
	if len(launches) != 1 {
		t.Fatalf("claude was launched %d times, want once: %v", len(launches), launches)
	}
	if b, _ := os.ReadFile(filepath.Join(e.fakeDir, "received")); !strings.Contains(string(b), "write the digest") {
		t.Errorf("claude received %q, want the run's prompt", b)
	}

	var threads struct {
		Threads []any `json:"threads"`
	}
	e.get("product_owner_threads?project_id="+e.project, &threads)
	if len(threads.Threads) != 0 {
		t.Errorf("the run created threads: %v", threads.Threads)
	}

	sessions := e.sess.List(e.ctx, ports.SessionFilter{ProjectID: e.project, IncludeProductOwner: true})
	if len(sessions) != 1 || sessions[0].Mode != domain.SessionModeProductOwner {
		t.Fatalf("sessions = %+v, want the run's one product-owner session", sessions)
	}
	eventuallyPO(t, "the run's process ends", func() string {
		s := e.sess.Get(sessions[0].ID)
		if s == nil || !s.Status.IsTerminal() || e.orch.ProductOwnerRunning(s.ID) {
			return "the run's process is still alive"
		}
		return ""
	})
	sid := sessions[0].ID

	var list struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	e.get("sessions?project_id="+e.project, &list)
	for _, s := range list.Sessions {
		if s.ID == sid {
			t.Errorf("the project's session list shows the run's session")
		}
	}
	res, err := http.Get(e.srv.URL + "/api/agent_board")
	if err != nil {
		t.Fatal(err)
	}
	board, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Contains(string(board), sid) {
		t.Errorf("the agent board shows the run's session")
	}
	notes, err := e.notes.ListNotifications(e.ctx, ports.NotificationQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes.Notifications) != 0 {
		t.Errorf("the inbox has %d items after a run, want none", len(notes.Notifications))
	}

	headers, _ := os.ReadFile(filepath.Join(e.fakeDir, "headers"))
	if want := "harness:product-owner:" + e.project; !strings.Contains(string(headers), want) {
		t.Errorf("claude was given the headers %q, want the attribution %s", headers, want)
	}
}

func TestAOneOffRunOfAnUnknownProjectIsRefused(t *testing.T) {
	e := newPOE2E(t)
	if _, err := e.po.RunOnce(e.ctx, "no-such-project", "x"); err == nil || !strings.Contains(err.Error(), "PROJECT_NOT_FOUND") {
		t.Fatalf("err = %v, want PROJECT_NOT_FOUND", err)
	}
	if n := len(e.launches()); n != 0 {
		t.Errorf("claude was launched %d times for an unknown project", n)
	}
}
