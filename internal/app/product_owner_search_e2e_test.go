//go:build !windows && sqlite_fts5

package app

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// The search of a project's conversations (ADR 0112) is SQLite FTS5, which the
// driver builds only with the sqlite_fts5 tag; the untagged build is covered by
// product_owner_search_untagged_e2e_test.go.

type poMatch struct {
	ThreadID    string `json:"thread_id"`
	ThreadTitle string `json:"thread_title"`
	ItemID      string `json:"item_id"`
	At          string `json:"at"`
	Archived    bool   `json:"archived"`
	Snippet     string `json:"snippet"`
}

func (e *poE2E) search(project, q string) (matches []poMatch, status int, code string) {
	e.t.Helper()
	var out struct {
		Matches []poMatch `json:"matches"`
	}
	status, code = e.get("search_product_owner_conversations?project_id="+project+"&q="+url.QueryEscape(q), &out)
	return out.Matches, status, code
}

// checkpointedThread runs a thread past a checkpoint and then one more turn, so
// that it has an archived part and a live one.
func (e *poE2E) checkpointedThread(first, second string) string {
	e.t.Helper()
	e.setContext(contextPastCheckpoint)
	id, _ := e.startThread(first)
	e.waitTurn(id, "idle")
	e.setContext(contextBelowCheckpoint)
	if status, code := e.send(id, second); status != 200 {
		e.t.Fatalf("send = %d %s", status, code)
	}
	e.waitTurn(id, "idle")
	return id
}

func matchOf(ms []poMatch, threadID, word string) *poMatch {
	for i := range ms {
		if ms[i].ThreadID == threadID && strings.Contains(strings.ToLower(ms[i].Snippet), "«"+word) {
			return &ms[i]
		}
	}
	return nil
}

func TestASearchFindsWhatWasSaidInAnArchivedPartAndTheSummaryAndTheLivePartWithStemming(t *testing.T) {
	e := newPOE2E(t)
	id := e.checkpointedThread("we decided to export the tasks as csv", "now let us talk about webhooks")
	th := e.thread(id)

	// A word of the archived part, in another inflection: stemming.
	ms, status, code := e.search(e.project, "exports")
	if status != 200 {
		t.Fatalf("search = %d %s", status, code)
	}
	m := matchOf(ms, id, "export")
	if m == nil || !m.Archived || m.ThreadTitle != th.Thread.Title || m.ItemID == "" {
		t.Fatalf("matches = %+v, want an archived match of the thread %q with the word marked", ms, th.Thread.Title)
	}
	if _, err := time.Parse(time.RFC3339, m.At); err != nil {
		t.Errorf("at = %q: %v", m.At, err)
	}

	// A word that only the summary has finds the checkpoint item.
	var checkpoint poItem
	for _, it := range th.Items {
		if it.Kind == "checkpoint" {
			checkpoint = it
		}
	}
	ms, _, _ = e.search(e.project, "decisions")
	found := false
	for _, mm := range ms {
		found = found || (mm.ItemID == checkpoint.ItemID && mm.Archived)
	}
	if !found {
		t.Errorf("matches = %+v, want the checkpoint %s", ms, checkpoint.ItemID)
	}

	// A word of the part after the checkpoint is found, not archived.
	ms, _, _ = e.search(e.project, "webhook")
	if m := matchOf(ms, id, "webhook"); m == nil || m.Archived {
		t.Errorf("matches = %+v, want a live match", ms)
	}

	// OR joins phrasings; a word nobody said finds nothing.
	if ms, _, _ = e.search(e.project, "spreadsheet OR csv"); matchOf(ms, id, "csv") == nil {
		t.Errorf("OR query matches = %+v", ms)
	}
	ms, status, _ = e.search(e.project, "kubernetes")
	if status != 200 || len(ms) != 0 {
		t.Errorf("no word: status %d matches %+v, want an empty list", status, ms)
	}
}

func TestASearchNeverCrossesProjects(t *testing.T) {
	e := newPOE2E(t)
	mine := e.checkpointedThread("the export is slow", "and the export again")
	other, err := e.cat.Projects.CreateProject(e.ctx, "other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		ThreadID string `json:"thread_id"`
	}
	if status, code := e.post("start_product_owner_thread", map[string]any{"project_id": other.ID, "message": "the export of the other project"}, &out); status != 200 {
		t.Fatalf("start = %d %s", status, code)
	}
	e.waitTurn(out.ThreadID, "idle")

	ms, _, _ := e.search(e.project, "export")
	if matchOf(ms, mine, "export") == nil || matchOf(ms, out.ThreadID, "export") != nil {
		t.Errorf("project matches = %+v, want only its own thread", ms)
	}
	ms, _, _ = e.search(other.ID, "export")
	if matchOf(ms, out.ThreadID, "export") == nil || matchOf(ms, mine, "export") != nil {
		t.Errorf("other project matches = %+v, want only its own thread", ms)
	}
}

func TestASearchAnswersAtMostThirtyMatchesAndRefusesAnEmptyOrOverlongQuery(t *testing.T) {
	e := newPOE2E(t)
	id, _ := e.startThread("banana")
	e.waitTurn(id, "idle")
	for i := 0; i < 40; i++ {
		if _, err := e.store.AppendItem(e.ctx, domain.ProductOwnerItem{ThreadID: id, Kind: domain.ProductOwnerItemMessage, At: time.Now(),
			Author: domain.ProductOwnerAuthorPerson, Text: "banana bread"}); err != nil {
			t.Fatal(err)
		}
	}
	if ms, _, _ := e.search(e.project, "banana"); len(ms) != 30 {
		t.Errorf("%d matches, want 30", len(ms))
	}
	for _, q := range []string{"", "   ", strings.Repeat("a", 201)} {
		if _, status, code := e.search(e.project, q); status != 400 || code != "INVALID_INPUT" {
			t.Errorf("q of %d characters = %d %s, want 400 INVALID_INPUT", len(q), status, code)
		}
	}
	// A query of nothing but operators and punctuation is a query of no words.
	if ms, status, _ := e.search(e.project, `"" ( ) * OR`); status != 200 || len(ms) != 0 {
		t.Errorf("punctuation: status %d matches %+v", status, ms)
	}
}

func TestASearchAnswersTheSameAfterARestart(t *testing.T) {
	e := newPOE2E(t)
	id := e.checkpointedThread("we decided to export the tasks as csv", "and then webhooks")
	before, _, _ := e.search(e.project, "export")
	if matchOf(before, id, "export") == nil {
		t.Fatalf("matches = %+v", before)
	}
	e.restart()
	after, status, code := e.search(e.project, "export")
	if status != 200 || len(after) != len(before) || after[0].ItemID != before[0].ItemID || !after[0].Archived {
		t.Errorf("after the restart: %d %s %+v, want %+v", status, code, after, before)
	}
}
