//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
)

// Agents move documents to project (ADR 0020): while the global setting is
// on, a session moves its own task's documents to project level with mv in
// its virtual shell; while it is off, the move is refused as it always was.

func (e *shellE2E) agentsMoveDocuments(on bool) {
	e.t.Helper()
	v := "false"
	if on {
		v = "true"
	}
	if _, err := e.cat.Settings.UpdateSettings(map[string]string{domain.SettingAgentsMoveDocuments: v}); err != nil {
		e.t.Fatal(err)
	}
}

func shellReadme(t *testing.T, s runningSession) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.shell, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAnAgentMovesItsTasksDocumentToProjectWhileTheSettingIsOn(t *testing.T) {
	e := newShellE2E(t)
	taskA, taskB := e.task("Moves"), e.task("Elsewhere")
	a, peer, b := e.start(taskA), e.start(taskA), e.start(taskB)
	doc := e.createDocument("Contract", "# Contract\n\nv1\n", "markdown", "task")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskA, "document_id": doc.ID}, nil)
	eventually(t, "the contract in A", func() error {
		return hasFile(a.shell, "task/docs/contract.md", "# Contract\n\nv1\n", doc.ID, "task", "")
	})

	e.agentsMoveDocuments(true)
	e.shellOp(a, "mv", func() { moveShellFile(t, a, "task/docs/contract.md", "project/docs/contract.md") }, "task/docs/contract.md", "project/docs/contract.md")

	wantDocument(t, e.document(doc.ID), "Contract", domain.DocumentFormatMarkdown, domain.DocumentScopeProject, "# Contract\n\nv1\n")
	if !e.linkedTo(taskA, doc.ID) {
		t.Error("the moved document lost its link to the task it was on")
	}
	if err := hasFile(a.shell, "project/docs/contract.md", "# Contract\n\nv1\n", doc.ID, "project", ""); err != nil {
		t.Fatal(err)
	}
	if err := lacks(a.shell, "no-such-id", "task/docs/contract.md"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the contract in the peer's project/docs", func() error {
		if err := hasFile(peer.shell, "project/docs/contract.md", "# Contract\n\nv1\n", doc.ID, "project", ""); err != nil {
			return err
		}
		return lacks(peer.shell, "no-such-id", "task/docs/contract.md")
	})
	eventually(t, "the contract in the other task's project/docs", func() error {
		return hasFile(b.shell, "project/docs/contract.md", "# Contract\n\nv1\n", doc.ID, "project", "")
	})
	if entries, _ := os.ReadDir(filepath.Join(a.shell, "task", "comments")); len(entries) != 0 {
		t.Errorf("the move wrote %d comments", len(entries))
	}

	// Back to task/docs still works.
	e.shellOp(a, "mv", func() { moveShellFile(t, a, "project/docs/contract.md", "task/docs/contract.md") }, "project/docs/contract.md", "task/docs/contract.md")
	wantDocument(t, e.document(doc.ID), "Contract", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, "# Contract\n\nv1\n")
}

func TestAnAgentMovesNoDocumentOfAnotherTaskToProject(t *testing.T) {
	e := newShellE2E(t)
	taskA, taskB := e.task("Moves"), e.task("Elsewhere")
	a := e.start(taskA)
	e.start(taskB)
	other := e.createDocument("Theirs", "theirs", "text", "task")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskB, "document_id": other.ID}, nil)
	rel := "tasks/" + taskB + "/docs/theirs.txt"
	eventually(t, "the other task's document in A", func() error { return hasFile(a.shell, rel, "theirs", other.ID, "task", "") })

	e.agentsMoveDocuments(true)
	got, _ := e.shellCall(a, "check", "mv", rel, "project/docs/theirs.txt")
	if got.Allow || got.Code != "PROJECT_MOVE_DENIED" {
		t.Fatalf("check mv of another task's document = %+v", got)
	}
	wantDocument(t, e.document(other.ID), "Theirs", domain.DocumentFormatText, domain.DocumentScopeTask, "theirs")
}

func TestARefusedMoveNamesTheSettingAndATogglePicksUpOnTheNextMove(t *testing.T) {
	e := newShellE2E(t)
	taskA := e.task("Moves")
	a := e.start(taskA)
	doc := e.createDocument("Plan", "plan", "text", "task")
	e.post("link_document_to_ticket", map[string]string{"ticket_id": taskA, "document_id": doc.ID}, nil)
	eventually(t, "the plan in A", func() error { return hasFile(a.shell, "task/docs/plan.txt", "plan", doc.ID, "task", "") })

	// Unset: refused, the reason names the setting, the README says to ask.
	got, _ := e.shellCall(a, "check", "mv", "task/docs/plan.txt", "project/docs/plan.txt")
	if got.Allow || got.Code != "PROJECT_MOVE_DENIED" || !strings.Contains(got.Reason, "Agents move documents to project") {
		t.Fatalf("check with the setting unset = %+v", got)
	}
	if r := shellReadme(t, a); !strings.Contains(r, "Agents move documents to project") || !strings.Contains(r, "off") {
		t.Errorf("README does not say the setting is off:\n%s", r)
	}

	// Turned on while the session runs: the next move is allowed and the
	// README follows.
	e.agentsMoveDocuments(true)
	eventually(t, "the README to say moving is on", func() error {
		if r := shellReadme(t, a); !strings.Contains(r, "is on: mv task/docs/<file>") || strings.Contains(r, "is off") {
			return os.ErrInvalid
		}
		return nil
	})
	e.shellOp(a, "mv", func() { moveShellFile(t, a, "task/docs/plan.txt", "project/docs/plan.txt") }, "task/docs/plan.txt", "project/docs/plan.txt")
	wantDocument(t, e.document(doc.ID), "Plan", domain.DocumentFormatText, domain.DocumentScopeProject, "plan")

	// Turned off again: the next move is refused and the file put back.
	e.shellOp(a, "mv", func() { moveShellFile(t, a, "project/docs/plan.txt", "task/docs/plan.txt") }, "project/docs/plan.txt", "task/docs/plan.txt")
	e.agentsMoveDocuments(false)
	moveShellFile(t, a, "task/docs/plan.txt", "project/docs/plan.txt")
	got, _ = e.shellCall(a, "commit", "mv", "task/docs/plan.txt", "project/docs/plan.txt")
	if got.Allow || got.Code != "PROJECT_MOVE_DENIED" {
		t.Fatalf("commit after turning the setting off = %+v", got)
	}
	wantDocument(t, e.document(doc.ID), "Plan", domain.DocumentFormatText, domain.DocumentScopeTask, "plan")
	if err := hasFile(a.shell, "task/docs/plan.txt", "plan", doc.ID, "task", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the README to say moving is off", func() error {
		if !strings.Contains(shellReadme(t, a), "is off") {
			return os.ErrInvalid
		}
		return nil
	})
}
