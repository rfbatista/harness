//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/rfbatista/harnesskit/errs"
	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/mcpinterview"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/interviews"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// answeredText is llmkit's turn for the answers below to the interview() the
// tests ask, as the agent must read it, to the letter.
const answeredText = "[interview answers · iv-1]\n" +
	"These are the person's answers to the interview you asked. Continue from them.\n" +
	"\n1. Which store?\nAnswer: Postgres\nNote: for the joins\n" +
	"\n2. Why?\nAnswer: Because.\n"

const submitBody = `{"answers":[{"chosen":[1],"text":"","note":"for the joins","skipped":false},{"chosen":[],"text":"Because.","note":"","skipped":false}]}`

// post sends a JSON body and reads the JSON answer.
func (e *signalsE2E) post(path, body string) (int, map[string]any) {
	e.t.Helper()
	res, err := http.Post(e.srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return res.StatusCode, out
}

// stop is the session's Stop hook, as the CLI posts it: the turn claude is
// told to go on with, "" when it may stop.
func (e *signalsE2E) stop(session string) string {
	e.t.Helper()
	status, out := e.post(httpapi.InteractiveSessionHookPath+"?session_id="+session+"&event=Stop", `{"hook_event_name":"Stop","stop_hook_active":false}`)
	if status != http.StatusOK {
		e.t.Fatalf("Stop hook: %d", status)
	}
	if out == nil {
		return ""
	}
	if out["decision"] != "block" {
		e.t.Fatalf("Stop hook answer: %+v", out)
	}
	return out["reason"].(string)
}

// answered posts the answers and wants the interview back, 200.
func (e *signalsE2E) submit(id string) map[string]any {
	e.t.Helper()
	status, out := e.post("/api/interviews/"+id+"/answers", submitBody)
	if status != http.StatusOK {
		e.t.Fatalf("submit %s: %d %+v", id, status, out)
	}
	return out["interview"].(map[string]any)
}

func (e *signalsE2E) dismiss(id, body string) map[string]any {
	e.t.Helper()
	status, out := e.post("/api/interviews/"+id+"/dismiss", body)
	if status != http.StatusOK {
		e.t.Fatalf("dismiss %s: %d %+v", id, status, out)
	}
	return out["interview"].(map[string]any)
}

func (e *signalsE2E) stored(session, id string) *domain.Interview {
	e.t.Helper()
	return interviewOf(e.t, e.read(session), id)
}

func (e *signalsE2E) delivery(session, id string) (string, bool) {
	iv := e.stored(session, id)
	if iv.Delivery == nil {
		return "", iv.DeliveredAt != nil
	}
	return string(*iv.Delivery), iv.DeliveredAt != nil
}

// A submit on a session that is mid-turn is stored as sent and queued; the
// turn is llmkit's text, unedited, and goes in with the Stop hook's block,
// after which the interview reads delivered with a time.
func TestSubmitOnABusySessionIsQueuedThenDeliveredAtTheStopHook(t *testing.T) {
	e := newSignalsE2E(t)
	session := e.start(domain.SessionRunning)
	e.ask("iv-1", session)
	events, err := e.svc.FollowInterviews(e.ctx, session)
	if err != nil {
		t.Fatal(err)
	}

	got := e.submit("iv-1")
	if got["state"] != "sent" || got["delivery"] != "queued" || got["delivered_at"] != nil || got["ended_at"] == nil {
		t.Fatalf("a busy session's submit: %+v", got)
	}
	if as := got["answers"].([]any); len(as) != 2 || as[0].(map[string]any)["note"] != "for the joins" {
		t.Fatalf("answers stored: %+v", got["answers"])
	}
	if d, at := e.delivery(session, "iv-1"); d != "queued" || at {
		t.Fatalf("read back: %s %v", d, at)
	}

	if reason := e.stop(session); reason != answeredText {
		t.Fatalf("the Stop hook's turn:\n%q\nwant\n%q", reason, answeredText)
	}
	if d, at := e.delivery(session, "iv-1"); d != "delivered" || !at {
		t.Fatalf("after the Stop hook: %s %v", d, at)
	}
	if reason := e.stop(session); reason != "" {
		t.Fatalf("nothing left to deliver: %q", reason)
	}

	// Each change went out as interview_changed, with the whole interview.
	var seen []string
	for range 2 {
		select {
		case ev := <-events:
			if ev.Kind != domain.InterviewChanged {
				t.Fatalf("event: %+v", ev)
			}
			seen = append(seen, string(ev.Interview.State)+"/"+string(*ev.Interview.Delivery))
		case <-time.After(5 * time.Second):
			t.Fatalf("events so far: %v", seen)
		}
	}
	if strings.Join(seen, " ") != "sent/queued sent/delivered" {
		t.Fatalf("events: %v", seen)
	}
}

// A dismiss, with a note or without, closes the interview as dismissed and
// delivers llmkit's dismissed turn.
func TestDismissDeliversTheDismissedTurnWithOrWithoutANote(t *testing.T) {
	e := newSignalsE2E(t)
	session := e.start(domain.SessionRunning)
	e.ask("iv-1", session)
	e.ask("iv-2", session)

	got := e.dismiss("iv-1", `{"note":"not now"}`)
	if got["state"] != "dismissed" || got["delivery"] != "queued" || got["dismiss_note"] != "not now" || got["answers"] != nil {
		t.Fatalf("dismissed with a note: %+v", got)
	}
	want := "[interview dismissed · iv-1]\nThe person chose not to answer this interview. Do not wait for answers.\nNote: not now\n"
	if reason := e.stop(session); reason != want {
		t.Fatalf("turn with a note: %q", reason)
	}

	got = e.dismiss("iv-2", `{}`)
	if got["state"] != "dismissed" || got["dismiss_note"] != nil {
		t.Fatalf("dismissed without a note: %+v", got)
	}
	want = "[interview dismissed · iv-2]\nThe person chose not to answer this interview. Do not wait for answers.\n"
	if reason := e.stop(session); reason != want {
		t.Fatalf("turn without a note: %q", reason)
	}
	if d, at := e.delivery(session, "iv-2"); d != "delivered" || !at {
		t.Fatalf("delivery: %s %v", d, at)
	}
	// An empty body is a dismiss with no note too.
	e.ask("iv-3", session)
	if status, _ := e.post("/api/interviews/iv-3/dismiss", ""); status != http.StatusOK {
		t.Fatalf("dismiss with no body: %d", status)
	}
}

// Repeating the operation that already succeeded returns the stored interview
// and delivers nothing a second time.
func TestRepeatingASubmitOrADismissDeliversNothingTwice(t *testing.T) {
	e := newSignalsE2E(t)
	session := e.start(domain.SessionRunning)
	e.ask("iv-1", session)
	e.ask("iv-2", session)

	first := e.submit("iv-1")
	again := e.submit("iv-1")
	if first["state"] != "sent" || again["state"] != "sent" || again["ended_at"] != first["ended_at"] {
		t.Fatalf("a repeated submit: %+v then %+v", first, again)
	}
	// Even with different answers: the stored result stands.
	if status, out := e.post("/api/interviews/iv-1/answers", `{"answers":[]}`); status != http.StatusOK || out["interview"].(map[string]any)["state"] != "sent" {
		t.Fatalf("a repeat with other answers: %d %+v", status, out)
	}
	if reason := e.stop(session); reason != answeredText {
		t.Fatalf("one turn: %q", reason)
	}
	e.submit("iv-1")
	if reason := e.stop(session); reason != "" {
		t.Fatalf("a repeat after delivery delivers nothing: %q", reason)
	}

	e.dismiss("iv-2", `{"note":"n"}`)
	e.dismiss("iv-2", `{"note":"other"}`)
	if reason := e.stop(session); !strings.HasPrefix(reason, "[interview dismissed · iv-2]") || !strings.Contains(reason, "Note: n\n") {
		t.Fatalf("one dismissed turn: %q", reason)
	}
	if reason := e.stop(session); reason != "" {
		t.Fatalf("nothing twice: %q", reason)
	}
	if iv := e.stored(session, "iv-2"); iv.DismissNote == nil || *iv.DismissNote != "n" {
		t.Fatalf("the first note stands: %+v", iv)
	}
}

// refusal reads an error body: its status, code, message and carried interview.
func refusalOf(t *testing.T, status int, out map[string]any, wantStatus int, wantCode string) (message string, carried map[string]any) {
	t.Helper()
	if status != wantStatus || out["code"] != wantCode {
		t.Fatalf("want %d %s, got %d %+v", wantStatus, wantCode, status, out)
	}
	if d, ok := out["details"].(map[string]any); ok {
		carried, _ = d["interview"].(map[string]any)
	}
	return out["error"].(string), carried
}

func TestEveryCodedRefusalCarriesItsDocumentedBody(t *testing.T) {
	e := newSignalsE2E(t)
	session := e.start(domain.SessionRunning)

	status, out := e.post("/api/interviews/ghost/answers", submitBody)
	refusalOf(t, status, out, http.StatusNotFound, "INTERVIEW_NOT_FOUND")
	status, out = e.post("/api/interviews/ghost/dismiss", `{}`)
	refusalOf(t, status, out, http.StatusNotFound, "INTERVIEW_NOT_FOUND")

	// Answers that do not fit name the question; nothing is closed.
	e.ask("iv-1", session)
	status, out = e.post("/api/interviews/iv-1/answers", `{"answers":[{"chosen":[0],"text":"","note":"","skipped":false},{"chosen":[0],"text":"x","note":"","skipped":false}]}`)
	msg, carried := refusalOf(t, status, out, http.StatusBadRequest, "INTERVIEW_ANSWERS_INVALID")
	if !strings.Contains(msg, "answer 2") || carried != nil {
		t.Fatalf("invalid answers: %q %+v", msg, carried)
	}
	status, out = e.post("/api/interviews/iv-1/answers", `{"answers":[]}`)
	refusalOf(t, status, out, http.StatusBadRequest, "INTERVIEW_ANSWERS_INVALID")
	if iv := e.stored(session, "iv-1"); iv.State != domain.InterviewOpen || iv.Delivery != nil {
		t.Fatalf("a refused submit changes nothing: %+v", iv)
	}
	if reason := e.stop(session); reason != "" {
		t.Fatalf("nothing is delivered for it: %q", reason)
	}

	// Dismissed: a submit is refused; sent: a dismiss is.
	e.dismiss("iv-1", `{}`)
	status, out = e.post("/api/interviews/iv-1/answers", submitBody)
	_, carried = refusalOf(t, status, out, http.StatusConflict, "INTERVIEW_NOT_OPEN")
	if carried["id"] != "iv-1" || carried["state"] != "dismissed" {
		t.Fatalf("not open carries the interview: %+v", carried)
	}
	e.ask("iv-2", session)
	e.submit("iv-2")
	status, out = e.post("/api/interviews/iv-2/dismiss", `{}`)
	_, carried = refusalOf(t, status, out, http.StatusConflict, "INTERVIEW_NOT_OPEN")
	if carried["state"] != "sent" || carried["delivery"] == nil {
		t.Fatalf("not open carries the interview: %+v", carried)
	}
	e.stop(session)

	// A session that ended before the answer: the interview is expired now
	// and carried; afterwards it is NOT_OPEN for both.
	e.ask("iv-3", session)
	if err := e.sessions.UpdateStatus(session, domain.SessionDone); err != nil {
		t.Fatal(err)
	}
	status, out = e.post("/api/interviews/iv-3/answers", submitBody)
	_, carried = refusalOf(t, status, out, http.StatusConflict, "INTERVIEW_EXPIRED")
	if carried["id"] != "iv-3" || carried["state"] != "expired" || carried["ended_at"] == nil {
		t.Fatalf("expired carries the interview: %+v", carried)
	}
	if iv := e.stored(session, "iv-3"); iv.State != domain.InterviewExpired {
		t.Fatalf("it reads expired: %+v", iv)
	}
	status, out = e.post("/api/interviews/iv-3/dismiss", `{}`)
	_, carried = refusalOf(t, status, out, http.StatusConflict, "INTERVIEW_NOT_OPEN")
	if carried["state"] != "expired" {
		t.Fatalf("%+v", carried)
	}
	status, out = e.post("/api/interviews/iv-3/answers", submitBody)
	refusalOf(t, status, out, http.StatusConflict, "INTERVIEW_NOT_OPEN")
}

func TestExpiryOnSessionEndRefusesALaterSubmit(t *testing.T) {
	e := newSignalsE2E(t)
	session := e.start(domain.SessionRunning)
	e.ask("iv-1", session)
	events, _ := e.svc.FollowInterviews(e.ctx, session)
	if _, err := e.orch.EndInteractive(e.ctx, session, 0, true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("no expiry")
	}
	status, out := e.post("/api/interviews/iv-1/answers", submitBody)
	_, carried := refusalOf(t, status, out, http.StatusConflict, "INTERVIEW_NOT_OPEN")
	if carried["state"] != "expired" {
		t.Fatalf("%+v", carried)
	}
}

// After a send or a dismiss the session's counts, the board card and the
// feed drop.
func TestSendingOrDismissingLowersTheCountsAndTheNeed(t *testing.T) {
	e := newSignalsE2E(t)
	session := e.start(domain.SessionRunning)
	feed := e.projectFeed()
	e.ask("iv-1", session)
	e.ask("iv-2", session)
	feedSession(t, feed, session, 2, 4)

	e.submit("iv-1")
	feedSession(t, feed, session, 1, 2)
	if c := e.card(); !c.NeedsYou {
		t.Fatalf("one is still open: %+v", c)
	}
	e.dismiss("iv-2", `{}`)
	feedSession(t, feed, session, 0, 0)
	if c := e.card(); c.NeedsYou {
		t.Fatalf("none open: %+v", c)
	}
	if s := e.session(session); s.OpenInterviewCount != 0 || s.OpenQuestionCount != 0 {
		t.Fatalf("session head: %+v", s)
	}
	if s := e.listed(session); s.OpenInterviewCount != 0 {
		t.Fatalf("session row: %+v", s)
	}
}

// countingTurns counts the turns handed to the courier, and runs them as a
// busy session's Stop hook would: on release.
type countingTurns struct {
	mu    sync.Mutex
	texts []string
	done  []func(time.Time)
}

func (c *countingTurns) Deliver(_, _, text string, done func(time.Time)) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.texts = append(c.texts, text)
	c.done = append(c.done, done)
	return false, nil
}

var _ ports.TurnDelivery = (*countingTurns)(nil)

// Submits, a dismiss and an expiry racing for one interview: exactly one
// wins, one turn is handed to the courier, and the others get the stored
// interview or INTERVIEW_NOT_OPEN, never a second delivery.
func TestRacingCallersOfOneInterviewHaveExactlyOneWinner(t *testing.T) {
	var (
		db       *gorm.DB
		sessions ports.SessionRepository
		agents   ports.AgentRepository
	)
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: t.TempDir(), ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule, AgentRuntimeModule, TaskChannelModule, TextProcessingModule,
		fx.Populate(&db, &sessions, &agents),
		fx.NopLogger,
	)
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(ctx) })

	courier := &countingTurns{}
	svc := interviews.New(interviews.Deps{Repo: sqlite.NewInterviewRepository(db), Sessions: sessions, Agents: agents, Turns: mcpinterview.Turns{}, Delivery: courier})
	if _, err := sessions.Create(&domain.Session{ID: "s1", ProjectID: "p", Status: domain.SessionRunning, Interactive: true, RunsOn: domain.RunnerTUI}); err != nil {
		t.Fatal(err)
	}
	answers := []domain.InterviewAnswer{{Chosen: []int{1}}, {Text: "Because."}}

	for round := range 30 {
		id := "race-" + string(rune('a'+round))
		if _, err := svc.Ask(ctx, interview(id, "s1")); err != nil {
			t.Fatal(err)
		}
		before := len(courier.texts)
		type result struct {
			op  string
			iv  *domain.Interview
			err error
		}
		results := make(chan result, 8)
		start := make(chan struct{})
		var wg sync.WaitGroup
		run := func(op string, f func() (*domain.Interview, error)) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				iv, err := f()
				results <- result{op, iv, err}
			}()
		}
		for range 4 {
			run("submit", func() (*domain.Interview, error) { return svc.Submit(ctx, id, answers) })
		}
		for range 3 {
			run("dismiss", func() (*domain.Interview, error) { return svc.Dismiss(ctx, id, "no") })
		}
		run("expire", func() (*domain.Interview, error) { return nil, svc.Expire(ctx, "s1") })
		close(start)
		wg.Wait()
		close(results)

		final, err := sqlite.NewInterviewRepository(db).Get(id)
		if err != nil || final.State == domain.InterviewOpen {
			t.Fatalf("round %d: %+v %v", round, final, err)
		}
		delivered := len(courier.texts) - before
		wantTurns := 1
		if final.State == domain.InterviewExpired {
			wantTurns = 0
		}
		if delivered != wantTurns {
			t.Fatalf("round %d: %s, %d turns delivered, want %d", round, final.State, delivered, wantTurns)
		}
		for r := range results {
			switch r.op {
			case "submit", "dismiss":
				own := map[string]domain.InterviewState{"submit": domain.InterviewSent, "dismiss": domain.InterviewDismissed}[r.op]
				if final.State == own {
					if r.err != nil || r.iv.State != own {
						t.Fatalf("round %d: the winner's %s: %+v %v", round, r.op, r.iv, r.err)
					}
				} else if code := errs.Code(r.err); code != "INTERVIEW_NOT_OPEN" {
					t.Fatalf("round %d: a losing %s after %s: %v", round, r.op, final.State, r.err)
				}
			case "expire":
				if r.err != nil {
					t.Fatalf("round %d: expire: %v", round, r.err)
				}
			}
		}
	}
}

// A submit on an idle session on the server's terminal host is typed into its
// terminal at once, as llmkit rendered it, and reads delivered with a time.
func TestSubmitOnAnIdleSessionIsPastedIntoItsTerminal(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	gitInit(t, repoDir)
	fake := filepath.Join(dir, "claude")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var (
		orch *orchestration.Service
		plan *planning.Service
		svc  *interviews.Service
		cat  catalog.Catalog
	)
	ctx := context.Background()
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: ":memory:", Root: dir, ClaudeBin: fake, SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule, ExecutionModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, InterviewsModule,
		fx.Populate(&orch, &plan, &svc, &cat),
		fx.NopLogger,
	)
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(ctx) })
	if _, err := cat.Settings.UpdateSettings(map[string]string{domain.SettingWorkspacesRoot: filepath.Join(dir, "worktrees")}); err != nil {
		t.Fatal(err)
	}
	p, err := cat.Projects.CreateProject(ctx, "harness", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := cat.Projects.CreateRepository(ctx, p.ID, "harness", "", "https://example.com/harness.git", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := plan.CreateTicket(ctx, p.ID, "Ask", "", domain.TicketStatusTodo, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Sessions: orch, Interviews: svc})))
	t.Cleanup(srv.Close)
	post := func(path, body string) (int, map[string]any) {
		res, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		return res.StatusCode, out
	}

	sess, _, err := orch.StartInteractive(ctx, ports.InteractiveRequest{ProjectID: p.ID, RepositoryID: r.ID, TicketID: tk.ID, RunsOn: domain.RunnerServer, Size: ports.TermSize{Cols: 100, Rows: 30}})
	if err != nil {
		t.Fatal(err)
	}
	// Its first turn ends: the session is idle.
	post(httpapi.InteractiveSessionHookPath+"?session_id="+sess.ID+"&event=Stop", `{"hook_event_name":"Stop"}`)
	if _, err := svc.Ask(ctx, interview("iv-1", sess.ID)); err != nil {
		t.Fatal(err)
	}

	status, out := post("/api/interviews/iv-1/answers", submitBody)
	if status != http.StatusOK {
		t.Fatalf("submit: %d %+v", status, out)
	}
	got := out["interview"].(map[string]any)
	if got["state"] != "sent" || got["delivery"] != "delivered" || got["delivered_at"] == nil {
		t.Fatalf("an idle session takes it at once: %+v", got)
	}
	want := strings.Split(strings.TrimSpace(answeredText), "\n")
	deadline := time.Now().Add(5 * time.Second)
	for {
		term, err := orch.AttachTerminal(ctx, sess.ID)
		if err != nil {
			t.Fatal(err)
		}
		snap, sub := term.Subscribe()
		sub.Close()
		screen := ansi.Strip(snap.Screen)
		missing := ""
		for _, line := range want {
			if line != "" && !strings.Contains(screen, line) {
				missing = line
			}
		}
		if missing == "" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the terminal never showed %q: %q", missing, screen)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
