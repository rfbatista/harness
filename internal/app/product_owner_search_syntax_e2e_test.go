//go:build !windows && sqlite_fts5

package app

import (
	"testing"
)

// The person's words are never FTS5 syntax: quotes, NEAR, prefix stars, column
// filters, the initial-token caret and the NOT minus are all read as plain
// words, so a query never fails with an SQL error, and a column filter on the
// project never reaches another project's conversations.
func TestASearchReadsFTS5SyntaxAsPlainWordsAndNeverLeavesItsProject(t *testing.T) {
	e := newPOE2E(t)
	id, _ := e.startThread("banana bread")
	e.waitTurn(id, "idle")
	other := e.otherProjectWithThread(t, "banana secret")

	for _, q := range []string{
		`banana"`,
		`"banana`,
		`banana" OR "secret`,
		`NEAR(banana bread)`,
		`NEAR(banana bread, 1)`,
		`banan*`,
		`body:banana`,
		`project_id:` + other,
		`{body project_id}: banana`,
		`^banana`,
		`banana -bread`,
		`banana NOT bread`,
		`banana AND`,
		`(banana`,
		`banana) OR (secret`,
		`'banana'`,
		`banana;DROP TABLE product_owner_search`,
	} {
		ms, status, code := e.search(e.project, q)
		if status != 200 {
			t.Errorf("q %q = %d %s, want 200", q, status, code)
			continue
		}
		for _, m := range ms {
			if m.ThreadID != id {
				t.Errorf("q %q found thread %s, not of this project", q, m.ThreadID)
			}
		}
	}
	// An operator is a plain word too: NEAR(banana bread) asks for the word near,
	// which no message has.
	if ms, _, _ := e.search(e.project, `NEAR(banana bread)`); len(ms) != 0 {
		t.Errorf("NEAR(banana bread) = %+v, want nothing: near is a word no message has", ms)
	}
	// And the search table is still there.
	if ms, status, _ := e.search(e.project, "banana"); status != 200 || len(ms) == 0 {
		t.Errorf("banana after the queries: %d %+v", status, ms)
	}
}

// otherProjectWithThread makes a second project with a thread its person
// started with text, and answers the project's id.
func (e *poE2E) otherProjectWithThread(t *testing.T, text string) string {
	t.Helper()
	p, err := e.cat.Projects.CreateProject(e.ctx, "other-search-project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		ThreadID string `json:"thread_id"`
	}
	if status, code := e.post("start_product_owner_thread", map[string]any{"project_id": p.ID, "message": text}, &out); status != 200 {
		t.Fatalf("start = %d %s", status, code)
	}
	e.waitTurn(out.ThreadID, "idle")
	return p.ID
}
