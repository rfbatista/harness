package code_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/in/web/code"
	"operators-mcp/internal/application/workspaces/workspacestest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports/portstest"
)

// A commit that changed more files than one diff page seeds the first page
// only, with the cursor for the next, and the page says how many files the
// whole diff has.
func TestTheFirstPaintSeedsTheFirstDiffPageOnly(t *testing.T) {
	f := portstest.NewCodeFixture(t)
	cb, at := workspacestest.NewCode(t, f)

	seed := code.LoadCommit(context.Background(), cb, f.ProjectID, at.RepositoryID, f.ManyTip)
	if seed.Error != nil || seed.Diff == nil {
		t.Fatalf("seed = %+v", seed.Error)
	}
	d := seed.Diff
	if len(d.Files) != domain.DiffPageFiles || d.Next == nil || *d.Next != d.Files[len(d.Files)-1].Path {
		t.Fatalf("seeded %d files, next %v; want one page with its next", len(d.Files), d.Next)
	}
	if d.FileCount <= len(d.Files) {
		t.Errorf("file_count = %d, want the whole diff's", d.FileCount)
	}

	body, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "many/f119.txt") || !strings.Contains(string(body), `"next":"many/f049.txt"`) {
		t.Errorf("the seed carries files past the page or lacks its next: %.300s", body)
	}

	paint := code.Build(seed)
	if paint.Totals != "120 files" {
		t.Errorf("the heading counts %q, want the whole diff's 120 files", paint.Totals)
	}
}
