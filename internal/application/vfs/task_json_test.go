package vfs_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"operators-mcp/internal/application/vfs"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type oneTicket struct {
	ports.TicketReader
	tk *domain.Ticket
}

func (o oneTicket) GetTicket(context.Context, string) (*domain.Ticket, error) { return o.tk, nil }
func (o oneTicket) ListTickets(context.Context, string, ports.TicketFilter) ([]*domain.Ticket, error) {
	return nil, nil
}

type taskDocuments struct{ fakeDocuments }

func (taskDocuments) ListTicketDocuments(context.Context, string) []*domain.Document { return nil }

type noComments struct{ ports.SessionTaskComments }

func (noComments) ListTicketComments(context.Context, string, time.Time) ([]*domain.TicketComment, error) {
	return nil, nil
}

type noResources struct{ ports.TaskResourceFiles }

func (noResources) ListTicketResources(context.Context, string) ([]*domain.TicketResource, error) {
	return nil, nil
}

type fixedStructure struct{ s *domain.TaskStructure }

func (f fixedStructure) TaskStructure(context.Context, string) (*domain.TaskStructure, error) {
	return f.s, nil
}

// taskJSONOf opens a task's shell and reads task/task.json.
func taskJSONOf(t *testing.T, structure ports.TaskStructureReader) map[string]any {
	t.Helper()
	tk := &domain.Ticket{ID: "t1", ProjectID: "p1", Title: "CSV export", Status: domain.TicketStatusTodo}
	svc := vfs.New(vfs.Sources{Tickets: oneTicket{tk: tk}, Structure: structure, Documents: taskDocuments{}, Comments: noComments{}, Resources: noResources{}})
	dir, err := svc.Open(context.Background(), "s1", ports.ShellScope{TicketID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	raw, err := os.ReadFile(filepath.Join(dir, "task", "task.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTaskJSONOfAPlainTaskHasNoParentAndNoChildren(t *testing.T) {
	task := taskJSONOf(t, fixedStructure{&domain.TaskStructure{Children: []domain.TaskChild{}}})
	if parent, ok := task["parent"]; !ok || parent != nil {
		t.Errorf("parent = %v (present %v), want null", parent, ok)
	}
	if children, ok := task["children"].([]any); !ok || len(children) != 0 {
		t.Errorf("children = %v, want []", task["children"])
	}
	if task["id"] != "t1" || task["title"] != "CSV export" || task["status"] != "todo" {
		t.Errorf("the existing fields changed: %v", task)
	}
}

func TestTaskJSONOfAChildNamesItsFeature(t *testing.T) {
	task := taskJSONOf(t, fixedStructure{&domain.TaskStructure{
		Parent:   &domain.TaskRef{ID: "f1", Title: "Exports", Status: domain.TicketStatusInProgress},
		Children: []domain.TaskChild{},
	}})
	parent, _ := task["parent"].(map[string]any)
	if parent["id"] != "f1" || parent["title"] != "Exports" || parent["status"] != "in_progress" {
		t.Errorf("parent = %v", task["parent"])
	}
}

func TestTaskJSONOfAFeatureListsItsChildrenWithTheirArchivedFlag(t *testing.T) {
	task := taskJSONOf(t, fixedStructure{&domain.TaskStructure{Children: []domain.TaskChild{
		{TaskRef: domain.TaskRef{ID: "c1", Title: "CSV export", Status: domain.TicketStatusTodo}},
		{TaskRef: domain.TaskRef{ID: "c2", Title: "PDF export", Status: domain.TicketStatusDone}, Archived: true},
	}}})
	children, _ := task["children"].([]any)
	if len(children) != 2 {
		t.Fatalf("children = %v", task["children"])
	}
	first, second := children[0].(map[string]any), children[1].(map[string]any)
	if first["id"] != "c1" || first["status"] != "todo" || first["archived"] != false || second["id"] != "c2" || second["archived"] != true {
		t.Errorf("children = %v", children)
	}
}
