//go:build !windows && !sqlite_fts5

package app

import (
	"net/url"
	"testing"
)

// A server built without the sqlite_fts5 tag boots, keeps checkpointing, and
// answers its search SEARCH_UNAVAILABLE (503).
func TestAServerBuiltWithoutFTS5BootsCheckpointsAndAnswersSearchUnavailable(t *testing.T) {
	e := newPOE2E(t)
	e.setContext(contextPastCheckpoint)
	id, _ := e.startThread("we decided to export the tasks")
	th := e.waitTurn(id, "idle")
	if len(checkpointsOf(th.Items)) != 1 {
		t.Errorf("items = %v, want the checkpoint to work without search", kinds(th.Items))
	}
	status, code := e.get("search_product_owner_conversations?project_id="+e.project+"&q="+url.QueryEscape("export"), nil)
	if status != 503 || code != "SEARCH_UNAVAILABLE" {
		t.Errorf("search = %d %s, want 503 SEARCH_UNAVAILABLE", status, code)
	}
}
