package productowner

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

type projects map[string]*domain.Project

func (p projects) GetProject(_ context.Context, id string) (*domain.Project, error) {
	if pr, ok := p[id]; ok {
		return pr, nil
	}
	return nil, errors.New("no such project")
}

func (p projects) ListProjects(context.Context) ([]*domain.Project, error) { return nil, nil }

// serve runs the page handler for `path` and returns what it rendered and the frame it asked for.
func serve(t *testing.T, path, project, thread string) (body string, frame shell.Frame, err error) {
	t.Helper()
	h := Handler{
		Projects: projects{"p1": {ID: "p1", Name: "harness"}},
		Layout: func(_ context.Context, title, projectID, _ string) (shell.Frame, error) {
			return shell.Frame{Title: title, Top: shell.TopBar{Current: projectID}}, nil
		},
		Render: func(w http.ResponseWriter, r *http.Request, status int, page templ.Component) error {
			w.WriteHeader(status)
			return page.Render(r.Context(), w)
		},
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("project", project)
	if thread != "" {
		req.SetPathValue("thread", thread)
	}
	rec := httptest.NewRecorder()
	h.Layout = func(_ context.Context, title, projectID, _ string) (shell.Frame, error) {
		frame = shell.Frame{Title: title, Top: shell.TopBar{Current: projectID}}
		return frame, nil
	}
	err = h.Page(rec, req)
	return rec.Body.String(), frame, err
}

func TestThePageCarriesTheIslandMountAndItsProps(t *testing.T) {
	body, _, err := serve(t, "/projects/p1/product-owner", "p1", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<main id="main" class="[ owner-page ]">`,
		`<div data-island="owner-page" data-island-rendered="false"></div>`,
		`<script type="application/json" data-island-props="owner-page">{"project_id":"p1","project_name":"harness","thread_id":""}</script>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
}

func TestAThreadsAddressNamesItInTheProps(t *testing.T) {
	body, _, err := serve(t, "/projects/p1/product-owner/threads/t-7", "p1", "t-7")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"thread_id":"t-7"`) {
		t.Errorf("props do not name the thread:\n%s", body)
	}
}

func TestThePageMarksTheRailsProductOwnerRowCurrent(t *testing.T) {
	body, _, err := serve(t, "/projects/p1/product-owner", "p1", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `<div id="frame-state" hidden data-page="`+shell.PageProductOwner+`"`) {
		t.Errorf("the frame does not say the product owner page is open:\n%s", body)
	}
}

func TestAnUnknownProjectHasNoPage(t *testing.T) {
	if _, _, err := serve(t, "/projects/nope/product-owner", "nope", ""); err == nil {
		t.Error("the page of an unknown project was served")
	}
}

func TestThePropsEscapeWhatCouldCloseTheScript(t *testing.T) {
	got := Props{ProjectID: "p1", ProjectName: `</script><b>x`, ThreadID: ""}.JSON()
	if strings.Contains(got, "</script>") || strings.Contains(got, "<b>") {
		t.Errorf("props are not escaped: %s", got)
	}
}

func TestEveryThreadHasItsOwnAddress(t *testing.T) {
	if got := ThreadHref("p 1", "t/1"); got != "/projects/p%201/product-owner/threads/t%2F1" {
		t.Errorf("thread href = %q", got)
	}
}
