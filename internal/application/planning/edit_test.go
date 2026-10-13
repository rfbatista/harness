package planning_test

import (
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const editPage = "<!doctype html>\n<html><head><meta charset=\"utf-8\"><title>Plan</title></head><body>\n<p>one</p>\n<p>two</p>\n<p>two</p>\n</body></html>\n"

func wantErrCode(t *testing.T, err error, code string) {
	t.Helper()
	if got := errs.Code(err); got != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestEditDocument_ReplacesExactlyAndChecksTheResult(t *testing.T) {
	svc, projectID := newService(t)
	d, err := svc.CreateDocument(projectID, "Plan", editPage, domain.DocumentFormatHTML, domain.DocumentScopeTask)
	if err != nil {
		t.Fatal(err)
	}

	edited, err := svc.EditDocument(d.ID, "<p>one</p>", "<p>uno</p>", false, "")
	if err != nil || !strings.Contains(edited.Content, "<p>uno</p>") || edited.Title != "Plan" || edited.Format != domain.DocumentFormatHTML {
		t.Fatalf("edit: %+v, %v", edited, err)
	}
	_, err = svc.EditDocument(d.ID, "<p>two</p>", "<p>dos</p>", false, "")
	wantErrCode(t, err, "EDIT_AMBIGUOUS")
	if !strings.HasPrefix(err.(*domain.StructuredError).Message, "2 matches") {
		t.Fatalf("ambiguity message: %v", err)
	}
	all, err := svc.EditDocument(d.ID, "<p>two</p>", "<p>dos</p>", true, "")
	if err != nil || strings.Count(all.Content, "<p>dos</p>") != 2 {
		t.Fatalf("replace_all: %v", err)
	}

	_, err = svc.EditDocument(d.ID, "<p>three</p>", "x", false, "")
	wantErrCode(t, err, "EDIT_NO_MATCH")
	_, err = svc.EditDocument(d.ID, "  <p>uno</P>", "x", false, "")
	wantErrCode(t, err, "EDIT_NO_MATCH") // exact only: no case or whitespace folding
	if !strings.Contains(err.Error(), `closest stored line: "<p>uno</p>"`) {
		t.Fatalf("no-match hint: %v", err)
	}
	_, err = svc.EditDocument(d.ID, "<body>", "<main>", false, "")
	wantErrCode(t, err, "DOCUMENT_NOT_HTML")
	for _, bad := range [][2]string{{"", "x"}, {"<p>uno</p>", "<p>uno</p>"}} {
		_, err = svc.EditDocument(d.ID, bad[0], bad[1], false, "")
		wantErrCode(t, err, "INVALID_INPUT")
	}
	_, err = svc.EditDocument("missing", "a", "b", false, "")
	wantErrCode(t, err, "DOCUMENT_NOT_FOUND")
	if got := svc.GetDocument(d.ID); got.Content != all.Content {
		t.Fatal("a refused edit changed the document")
	}
}

func TestEditDocument_RefusesMarkdownAndStaleVersions(t *testing.T) {
	svc, projectID := newService(t)
	md, _ := svc.CreateDocument(projectID, "Notes", "# notes\n", domain.DocumentFormatMarkdown, domain.DocumentScopeTask)
	_, err := svc.EditDocument(md.ID, "notes", "Notes", false, "")
	wantErrCode(t, err, "DOCUMENT_NOT_HTML")
	if !strings.Contains(err.Error(), "this document is Markdown; rewrite it once as HTML with update_*_document, then edit") {
		t.Fatalf("markdown refusal: %v", err)
	}

	d, _ := svc.CreateDocument(projectID, "Plan", editPage, domain.DocumentFormatHTML, domain.DocumentScopeTask)
	v := domain.DocumentVersion(d.Content)
	if _, err := svc.EditDocument(d.ID, "<p>one</p>", "<p>uno</p>", false, v); err != nil {
		t.Fatal(err)
	}
	_, err = svc.EditDocument(d.ID, "<p>uno</p>", "<p>eins</p>", false, v)
	wantErrCode(t, err, "DOCUMENT_CHANGED")
	now := domain.DocumentVersion(svc.GetDocument(d.ID).Content)
	if !strings.Contains(err.Error(), "now at "+now) {
		t.Fatalf("stale version message: %v", err)
	}
}

// racingDocuments lands another write between the edit's read and its
// compare-and-set, the first time only.
type racingDocuments struct {
	ports.DocumentRepository
	raced bool
}

func (r *racingDocuments) UpdateIfContent(id, expected, title, content string, format domain.DocumentFormat) (*domain.Document, error) {
	if !r.raced {
		r.raced = true
		cur := r.Get(id)
		if _, err := r.Update(id, cur.Title, strings.Replace(cur.Content, "<p>two</p>\n<p>two</p>", "<p>two</p>\n<p>deux</p>", 1), ""); err != nil {
			return nil, err
		}
	}
	return r.DocumentRepository.UpdateIfContent(id, expected, title, content, format)
}

func TestEditDocument_CompareAndSet(t *testing.T) {
	for _, withVersion := range []bool{false, true} {
		svc, docs, projectID := newServiceWithDocuments(t, func(inner ports.DocumentRepository) ports.DocumentRepository {
			return &racingDocuments{DocumentRepository: inner}
		})
		d, _ := svc.CreateDocument(projectID, "Plan", editPage, domain.DocumentFormatHTML, domain.DocumentScopeTask)
		version := ""
		if withVersion {
			version = domain.DocumentVersion(d.Content)
		}
		edited, err := svc.EditDocument(d.ID, "<p>one</p>", "<p>uno</p>", false, version)
		got := docs.Get(d.ID).Content
		if !strings.Contains(got, "<p>deux</p>") {
			t.Fatalf("the racing write was lost: %q", got)
		}
		if withVersion {
			// The caller pinned a version: the race is theirs to resolve.
			wantErrCode(t, err, "DOCUMENT_CHANGED")
			if strings.Contains(got, "<p>uno</p>") {
				t.Fatal("a lost race was written anyway")
			}
		} else if err != nil || !strings.Contains(edited.Content, "<p>uno</p>") || !strings.Contains(edited.Content, "<p>deux</p>") {
			// Without one, the edit is tried once more on the fresh content.
			t.Fatalf("retry: %v", err)
		}
	}
}

// newServiceWithDocuments is newService with the document repository wrapped.
func newServiceWithDocuments(t *testing.T, wrap func(ports.DocumentRepository) ports.DocumentRepository) (*planning.Service, ports.DocumentRepository, string) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, err := projects.Create("proj", "/tmp/proj")
	if err != nil {
		t.Fatal(err)
	}
	docs := wrap(sqlite.NewDocumentRepository(db))
	return planning.NewService(sqlite.NewTicketRepository(db), docs, projects), docs, p.ID
}
