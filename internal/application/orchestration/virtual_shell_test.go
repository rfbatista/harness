//go:build !windows

package orchestration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/blobstore"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/vshell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// shellArtifacts serves a fixed set of artifacts and their bytes, filtered
// the way the artifacts service filters them.
type shellArtifacts struct {
	list  []*domain.Artifact
	bytes map[string]string
}

func (f shellArtifacts) ListArtifacts(_ context.Context, flt ports.ArtifactFilter) ([]*domain.Artifact, error) {
	var out []*domain.Artifact
	for _, a := range f.list {
		switch {
		case flt.TicketID != "" && a.TicketID != flt.TicketID && !a.AttachedTo(flt.TicketID):
		case flt.ProjectID != "" && a.ProjectID != flt.ProjectID:
		case flt.Scope != "" && a.Scope != flt.Scope:
		default:
			out = append(out, a)
		}
	}
	return out, nil
}

func (f shellArtifacts) GetArtifact(context.Context, string) (*domain.Artifact, error) {
	return nil, nil
}
func (shellArtifacts) DeleteArtifact(context.Context, string) error { return nil }
func (f shellArtifacts) OpenArtifactFile(_ context.Context, id, relpath string) (*ports.ArtifactFile, error) {
	b, ok := f.bytes[id]
	if !ok || relpath != "" {
		return nil, &domain.StructuredError{Code: "ARTIFACT_NOT_FOUND", Message: "artifact not found"}
	}
	return &ports.ArtifactFile{Content: nopCloser{strings.NewReader(b)}, Size: int64(len(b))}, nil
}

type nopCloser struct{ io.ReadSeeker }

func (nopCloser) Close() error { return nil }

// shellWorld is a task with something of every kind for its shell to hold,
// in a project that has more.
type shellWorld struct {
	plan                    *planning.Service
	task                    *domain.Ticket
	planDoc, notesA, notesB *domain.Document
	glossary, architecture  *domain.Document
	byPerson, byOther       *domain.TicketComment
}

const planPage = "<!doctype html><html><head><title>Plan</title></head><body>Roll out</body></html>"

// withShells gives svc virtual shells over a fresh planning world, and
// answers that world. The orchestration's own ticket lookup serves the task
// in the test catalog's project.
func withShells(t *testing.T, svc *Service) *shellWorld {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	projects := sqlite.NewProjectRepository(db)
	p, err := projects.Create("proj", "/tmp/proj")
	if err != nil {
		t.Fatal(err)
	}
	plan := planning.NewService(sqlite.NewTicketRepository(db), sqlite.NewDocumentRepository(db), projects)
	plan.Resources, plan.Comments = sqlite.NewTicketResourceRepository(db), sqlite.NewTicketCommentRepository(db)
	plan.Blobs = blobstore.New(t.TempDir())
	plan.ContentURL = func(id string) string { return "/api/ticket_resources/" + id + "/content" }

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	w := &shellWorld{plan: plan}
	w.task, err = plan.CreateTicket(ctx, p.ID, "Ship the thing", "with care", domain.TicketStatusInProgress)
	must(err)
	other, err := plan.CreateTicket(ctx, p.ID, "Elsewhere", "", "")
	must(err)

	doc := func(title, content string, format domain.DocumentFormat, scope domain.DocumentScope, link string) *domain.Document {
		t.Helper()
		d, err := plan.CreateDocument(p.ID, title, content, format, scope)
		must(err)
		if link != "" {
			must(plan.LinkDocument(link, d.ID))
		}
		return d
	}
	w.planDoc = doc("Plan: rollout", planPage, domain.DocumentFormatHTML, domain.DocumentScopeTask, w.task.ID)
	w.notesA = doc("Notes", "# Notes A\n", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, w.task.ID)
	w.notesB = doc("Notes", "# Notes B\n", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, w.task.ID)
	w.glossary = doc("Glossary", "# Terms\n", domain.DocumentFormatMarkdown, domain.DocumentScopeProject, w.task.ID)
	w.architecture = doc("Architecture", "# Layers\n", domain.DocumentFormatMarkdown, domain.DocumentScopeProject, "")
	doc("Other task's notes", "# Not ours\n", domain.DocumentFormatMarkdown, domain.DocumentScopeTask, other.ID)

	w.byPerson, err = plan.AddTicketComment(ctx, w.task.ID, "Please hurry")
	must(err)
	w.byOther, err = plan.AddSessionComment(ctx, w.task.ID, domain.CommentWriter{SessionID: "s-other", AgentName: "Go Developer"}, "On it")
	must(err)
	_, err = plan.UploadTicketResource(ctx, ports.ResourceUpload{TicketID: w.task.ID, FileName: "spec.pdf", Mime: "application/pdf", Content: strings.NewReader("%PDF-1.4 spec")})
	must(err)
	_, err = plan.AddTicketLink(ctx, w.task.ID, "https://example.com/board", "Figma board")
	must(err)

	arts := shellArtifacts{
		list: []*domain.Artifact{
			{ID: "a-home", TicketID: w.task.ID, ProjectID: p.ID, Kind: domain.ArtifactPage, Title: "Home", Path: "design/home.html", Revision: 2, Scope: domain.ArtifactScopeTask},
			{ID: "a-logo", TicketID: other.ID, ProjectID: p.ID, Kind: domain.ArtifactImage, Title: "Logo", Path: "brand/logo.svg", Revision: 1, Scope: domain.ArtifactScopeProject, AttachedTicketIDs: []string{w.task.ID}},
			{ID: "a-palette", TicketID: other.ID, ProjectID: p.ID, Kind: domain.ArtifactFile, Title: "Palette", Path: "brand/palette.css", Revision: 1, Scope: domain.ArtifactScopeProject},
			{ID: "a-dev", TicketID: other.ID, ProjectID: p.ID, Kind: domain.ArtifactURL, Title: "Dev server", URL: "http://localhost:5173/", Revision: 1, Scope: domain.ArtifactScopeProject},
		},
		bytes: map[string]string{"a-home": "<h1>home</h1>", "a-logo": "<svg/>", "a-palette": ":root{}"},
	}
	svc.Shells = vshell.New(vshell.Sources{Tickets: plan, Documents: plan, Comments: plan, Resources: plan, Artifacts: arts})

	inCatalog := *w.task
	inCatalog.ProjectID = "p1"
	svc.tickets = fakeTickets{w.task.ID: &inCatalog}
	return w
}

type shellFile struct {
	mode    fs.FileMode
	content string
}

// readShell is every file under dir, by slash path relative to it.
func readShell(t *testing.T, dir string) map[string]shellFile {
	t.Helper()
	out := map[string]shellFile{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out[filepath.ToSlash(rel)] = shellFile{info.Mode().Perm(), string(b)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func commentName(c *domain.TicketComment, author string) string {
	return "task/comments/" + c.CreatedAt.UTC().Format("20060102-150405") + "-" + author + "-" + c.ID[:8] + ".md"
}

// checkShell checks dir holds w's task and project in the contract's layout,
// names and modes, for session sessionID.
func checkShell(t *testing.T, dir string, w *shellWorld) {
	t.Helper()
	if !strings.HasPrefix(filepath.Base(dir), "harness-shell-") || !filepath.IsAbs(dir) {
		t.Fatalf("shell %q is not an absolute harness-shell-* temp dir", dir)
	}
	got := readShell(t, dir)

	// Of two documents with one title, one keeps the plain name.
	plainNotes, otherNotes := w.notesA, w.notesB
	if got["task/docs/notes.md"].content == w.notesB.Content {
		plainNotes, otherNotes = w.notesB, w.notesA
	}
	want := map[string]shellFile{
		"task/docs/plan-rollout.html":                  {0o644, planPage},
		"task/docs/notes.md":                           {0o644, plainNotes.Content},
		"task/docs/notes-" + otherNotes.ID[:8] + ".md": {0o644, otherNotes.Content},
		"project/docs/glossary.md":                     {0o644, "# Terms\n"},
		"project/docs/architecture.md":                 {0o644, "# Layers\n"},
		commentName(w.byPerson, "person"):              {0o444, "Please hurry"},
		commentName(w.byOther, "go-developer"):         {0o444, "On it"},
		"task/resources/spec.pdf":                      {0o444, "%PDF-1.4 spec"},
		"task/designs/design/home.html":                {0o644, "<h1>home</h1>"},
		"project/designs/brand/logo.svg":               {0o644, "<svg/>"},
		"project/designs/brand/palette.css":            {0o644, ":root{}"},
	}
	for path, wf := range want {
		gf, ok := got[path]
		if !ok {
			t.Errorf("%s missing; shell has %v", path, keys(got))
			continue
		}
		if gf != wf {
			t.Errorf("%s = %o %q, want %o %q", path, gf.mode, gf.content, wf.mode, wf.content)
		}
	}
	generated := map[string]string{
		"task/resources/links.md":  "- [Figma board](https://example.com/board)",
		"task/designs/attached.md": "Logo → project/designs/brand/logo.svg",
		"project/designs/urls.md":  "- [Dev server](http://localhost:5173/)",
		"README.md":                "task/docs/",
		".index.json":              "",
		"task/task.json":           "",
	}
	for path, line := range generated {
		gf, ok := got[path]
		if !ok {
			t.Errorf("%s missing", path)
			continue
		}
		if gf.mode != 0o444 || !strings.Contains(gf.content, line) {
			t.Errorf("%s = %o %q, want read-only with %q", path, gf.mode, gf.content, line)
		}
	}
	if extra := len(got) - len(want) - len(generated); extra != 0 {
		t.Errorf("shell has %d unexpected files: %v", extra, keys(got))
	}

	var task map[string]any
	if err := json.Unmarshal([]byte(got["task/task.json"].content), &task); err != nil {
		t.Fatal(err)
	}
	wantTask := map[string]any{"id": w.task.ID, "title": "Ship the thing", "description": "with care", "status": "in_progress", "architect_session_id": nil}
	if len(task) != len(wantTask) {
		t.Errorf("task.json = %v, want %v", task, wantTask)
	}
	for k, v := range wantTask {
		if task[k] != v {
			t.Errorf("task.json %s = %v, want %v", k, task[k], v)
		}
	}

	var index []ports.ShellFile
	if err := json.Unmarshal([]byte(got[".index.json"].content), &index); err != nil {
		t.Fatal(err)
	}
	var indexed []string
	for _, row := range index {
		indexed = append(indexed, row.Path)
	}
	slices.Sort(indexed)
	files := slices.DeleteFunc(keys(got), func(p string) bool { return p == ".index.json" })
	if !slices.Equal(indexed, files) {
		t.Errorf("index paths %v, want every file %v", indexed, files)
	}
	rows := map[string]ports.ShellFile{}
	for _, row := range index {
		rows[row.Path] = row
	}
	wantRows := []ports.ShellFile{
		{Path: "task/docs/plan-rollout.html", Kind: "document", ID: w.planDoc.ID, Scope: "task", Version: domain.DocumentVersion(planPage), Title: "Plan: rollout", UpdatedAt: w.planDoc.UpdatedAt},
		{Path: "project/docs/glossary.md", Kind: "document", ID: w.glossary.ID, Scope: "project", Version: domain.DocumentVersion("# Terms\n"), Title: "Glossary", UpdatedAt: w.glossary.UpdatedAt},
		{Path: "task/designs/design/home.html", Kind: "artifact", ID: "a-home", Scope: "task", Version: "2", Title: "Home"},
		{Path: "project/designs/brand/logo.svg", Kind: "artifact", ID: "a-logo", Scope: "project", Version: "1", Title: "Logo"},
	}
	for _, wr := range wantRows {
		r := rows[wr.Path]
		if r.Path != wr.Path || r.Kind != wr.Kind || r.ID != wr.ID || r.Scope != wr.Scope || r.Version != wr.Version || r.Title != wr.Title || !r.UpdatedAt.Equal(wr.UpdatedAt) {
			t.Errorf("index row %+v, want %+v", r, wr)
		}
	}
	if r := rows[commentName(w.byPerson, "person")]; r.Kind != "comment" || r.ID != w.byPerson.ID || r.Scope != "task" {
		t.Errorf("comment row %+v", r)
	}
}

func keys(m map[string]shellFile) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func envValue(env []string, name string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, name+"="); ok {
			return v
		}
	}
	return ""
}

func waitGone(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("shell %s outlived its session", dir)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitFile waits for name to appear and answers its content.
func waitFile(t *testing.T, name string) []byte {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if b, err := os.ReadFile(name); err == nil {
			return b
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never appeared", name)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// checkLaunch checks the CLI gets the shell with --add-dir and a brief that
// names it.
func checkLaunch(t *testing.T, args []string, shell string) {
	t.Helper()
	found := false
	for i, a := range args {
		if a == "--add-dir" && i+1 < len(args) && args[i+1] == shell {
			found = true
		}
	}
	if !found {
		t.Errorf("args %q lack --add-dir %s", args, shell)
	}
	if system, _ := flag(args, "--append-system-prompt"); !strings.Contains(system, shell) {
		t.Errorf("the brief does not name the shell %s:\n%s", shell, system)
	}
}

func TestAnInteractiveTaskSessionFindsItsTaskInItsVirtualShell(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `printf '%s' "$HARNESS_SHELL" > shell-path; printf 'ready\r\n'; cat`})
	w := withShells(t, svc)
	sess, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID,
		RunsOn: domain.RunnerServer, Size: ports.TermSize{Cols: 80, Rows: 24}})

	shell := string(waitFile(t, filepath.Join(sess.WorkingDir, "shell-path")))
	if shell == "" || envValue(launch.Spec.Env, "HARNESS_SHELL") != shell {
		t.Fatalf("the CLI saw HARNESS_SHELL %q, spec env %v", shell, launch.Spec.Env)
	}
	checkLaunch(t, launch.Args, shell)
	checkShell(t, shell, w)

	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	waitGone(t, shell)
}

func TestAHeadlessTaskSessionFindsItsTaskInItsVirtualShell(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.DefaultEnv = append(svc.DefaultEnv, "CLAUDE_FAKE_RECORD=1")
	w := withShells(t, svc)
	sess, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID, Task: "go"})
	if err != nil {
		t.Fatal(err)
	}
	var launch fakeLaunch
	if err := json.Unmarshal(waitFile(t, filepath.Join(sess.WorkingDir, fakeLaunchFile)), &launch); err != nil {
		t.Fatal(err)
	}
	if launch.Shell == "" {
		t.Fatal("the CLI got no HARNESS_SHELL")
	}
	checkLaunch(t, launch.Args, launch.Shell)
	checkShell(t, launch.Shell, w)

	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	waitGone(t, launch.Shell)
}

func TestAVirtualShellGoesWhenItsSessionFails(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `printf '%s' "$HARNESS_SHELL" > shell-path; exit 3`})
	w := withShells(t, svc)
	sess, _ := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID,
		RunsOn: domain.RunnerServer, Size: ports.TermSize{Cols: 80, Rows: 24}})
	shell := string(waitFile(t, filepath.Join(sess.WorkingDir, "shell-path")))
	if shell == "" {
		t.Fatal("the CLI got no HARNESS_SHELL")
	}
	waitStatus(t, svc, sess.ID, domain.SessionFailed)
	waitGone(t, shell)
}

// A session that ends loses its shell; resumed, it gets a fresh one, where
// its own comments are its to edit.
func TestAResumedSessionGetsAFreshVirtualShell(t *testing.T) {
	svc, _ := newInteractiveService(t)
	w := withShells(t, svc)
	ctx := context.Background()
	sess, launch := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID})
	first := envValue(launch.Spec.Env, "HARNESS_SHELL")
	checkLaunch(t, launch.Args, first)
	checkShell(t, first, w)

	own, err := w.plan.AddSessionComment(ctx, w.task.ID, domain.CommentWriter{SessionID: sess.ID}, "Done with the plan")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	waitGone(t, first)

	_, spec, err := svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	second := envValue(spec.Env, "HARNESS_SHELL")
	if second == "" || second == first {
		t.Fatalf("resumed with shell %q, want a fresh one (was %q)", second, first)
	}
	checkLaunch(t, launchOf(t, spec).Args, second)
	got := readShell(t, second)
	if f := got[commentName(own, "claude")]; f.mode != 0o644 || f.content != "Done with the plan" {
		t.Errorf("own comment = %o %q, want writable; shell has %v", f.mode, f.content, keys(got))
	}
	if f := got[commentName(w.byPerson, "person")]; f.mode != 0o444 {
		t.Errorf("the person's comment is %o, want read-only", f.mode)
	}

	if _, err := svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	waitGone(t, second)
}

func TestASessionWithoutATaskGetsNoVirtualShell(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.DefaultEnv = append(svc.DefaultEnv, "CLAUDE_FAKE_RECORD=1")
	withShells(t, svc)
	sess, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", Task: "go"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), sess.ID) })
	var launch fakeLaunch
	if err := json.Unmarshal(waitFile(t, filepath.Join(sess.WorkingDir, fakeLaunchFile)), &launch); err != nil {
		t.Fatal(err)
	}
	if launch.Shell != "" || bytes.Contains([]byte(strings.Join(launch.Args, " ")), []byte("harness-shell-")) {
		t.Fatalf("a session with no task got a shell: %q %q", launch.Shell, launch.Args)
	}
}

// An interactive task session runs with its shim dir first on PATH: the
// seven commands, each a link to the harness's shim binary, with the
// session's shell endpoint in HARNESS_SHELL_URL. The dir goes with the
// session.
func TestAnInteractiveTaskSessionRunsWithItsShimsFirstOnPath(t *testing.T) {
	svc := newHostedService(t, scriptAgent{script: `printf '%s' "$PATH" > path; printf '%s' "$HARNESS_SHELL_URL" > shell-url; printf 'ready\r\n'; cat`})
	w := withShells(t, svc)
	svc.ShimBinary = "/opt/harness/server"
	svc.ShellURL = func(id string) string { return "http://127.0.0.1:9/api/sessions/" + id + "/shell" }
	sess, _ := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID,
		RunsOn: domain.RunnerServer, Size: ports.TermSize{Cols: 80, Rows: 24}})

	path := string(waitFile(t, filepath.Join(sess.WorkingDir, "path")))
	shims, rest, _ := strings.Cut(path, string(os.PathListSeparator))
	if rest != os.Getenv("PATH") {
		t.Errorf("PATH after the shims = %q, want the server's %q", rest, os.Getenv("PATH"))
	}
	entries, err := os.ReadDir(shims)
	if err != nil {
		t.Fatalf("first PATH entry %q: %v", shims, err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		if target, err := os.Readlink(filepath.Join(shims, e.Name())); err != nil || target != "/opt/harness/server" {
			t.Errorf("%s links to %q (%v), want the shim binary", e.Name(), target, err)
		}
	}
	if want := []string{"cp", "mkdir", "mv", "rm", "sed", "tee", "touch"}; !slices.Equal(names, want) {
		t.Errorf("shim dir holds %v, want %v", names, want)
	}
	if got, want := string(waitFile(t, filepath.Join(sess.WorkingDir, "shell-url"))), "http://127.0.0.1:9/api/sessions/"+sess.ID+"/shell"; got != want {
		t.Errorf("HARNESS_SHELL_URL = %q, want %q", got, want)
	}

	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	waitGone(t, shims)
}
