//go:build !windows

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/interviews"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// interviewsE2E is the server wired as it runs, on a database file so a test
// can restart it, with the HTTP API of the interviews on a test server.
// Sessions are recorded on the store the way the agent host records them; the
// interviews are asked through the service llmkit's Sink will call.
type interviewsE2E struct {
	t        *testing.T
	ctx      context.Context
	dir      string
	cat      catalog.Catalog
	sessions ports.SessionRepository
	svc      *interviews.Service
	app      *fx.App
	srv      *httptest.Server
	project  string
}

func newInterviewsE2E(t *testing.T) *interviewsE2E {
	t.Helper()
	e := &interviewsE2E{t: t, ctx: context.Background(), dir: t.TempDir()}
	e.boot()
	p, err := e.cat.Projects.CreateProject(e.ctx, "harness", e.dir)
	if err != nil {
		t.Fatal(err)
	}
	e.project = p.ID
	return e
}

// boot starts the server on the database file; restart stops it and boots it
// again on the same file.
func (e *interviewsE2E) boot() {
	e.t.Helper()
	e.app = fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(e.dir, "data", "harness.db"), Root: e.dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, InterviewsModule,
		fx.Populate(&e.cat, &e.sessions, &e.svc),
		fx.NopLogger,
	)
	if err := e.app.Start(e.ctx); err != nil {
		e.t.Fatal(err)
	}
	e.srv = httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{Interviews: e.svc})))
}

func (e *interviewsE2E) stop() {
	e.srv.CloseClientConnections()
	e.srv.Close()
	if err := e.app.Stop(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

func (e *interviewsE2E) restart() {
	e.t.Helper()
	e.stop()
	e.boot()
}

func (e *interviewsE2E) session(id string, status domain.SessionStatus, agent string) {
	e.t.Helper()
	if _, err := e.sessions.Create(&domain.Session{ID: id, ProjectID: e.project, AgentID: agent, Status: status, Interactive: true, RunsOn: domain.RunnerTUI}); err != nil {
		e.t.Fatal(err)
	}
}

func interviewOf(t *testing.T, list domain.InterviewList, id string) *domain.Interview {
	t.Helper()
	for _, iv := range list.Interviews {
		if iv.ID == id {
			return iv
		}
	}
	t.Fatalf("no interview %s in %+v", id, list)
	return nil
}

func recommendedOption(i int) *domain.Recommended { return &domain.Recommended{Option: &i} }

// interview is an interview of two questions as llmkit hands one over: it has
// no state, time or agent name yet.
func interview(id, session string) domain.Interview {
	text := "Because it is simple."
	return domain.Interview{
		ID: id, SessionID: session, Title: "Storage",
		Questions: []domain.InterviewQuestion{
			{Title: "Which store?", Body: "Pick **one**.", Kind: domain.QuestionSingle,
				Options:     []domain.InterviewOption{{Label: "SQLite", Description: "One file."}, {Label: "Postgres"}},
				Recommended: recommendedOption(0)},
			{Title: "Why?", Body: "", Kind: domain.QuestionText, Options: []domain.InterviewOption{}, Recommended: &domain.Recommended{Text: &text}},
		},
	}
}

func (e *interviewsE2E) ask(i domain.Interview) *domain.Interview {
	e.t.Helper()
	got, err := e.svc.Ask(e.ctx, i)
	if err != nil {
		e.t.Fatalf("ask %s: %v", i.ID, err)
	}
	return got
}

func (e *interviewsE2E) read(session string) domain.InterviewList {
	e.t.Helper()
	res, err := http.Get(e.srv.URL + "/api/sessions/" + session + "/interviews")
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		e.t.Fatalf("GET interviews of %s: %d", session, res.StatusCode)
	}
	var list domain.InterviewList
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		e.t.Fatal(err)
	}
	return list
}

// pageStream is one tab's GET /api/stream with a feed followed on it.
type pageStream struct {
	t     *testing.T
	lines chan map[string]any
	id    string
}

func (e *interviewsE2E) openStream() *pageStream {
	e.t.Helper()
	ctx, cancel := context.WithCancel(e.ctx)
	e.t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/stream", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	s := &pageStream{t: e.t, lines: make(chan map[string]any, 64)}
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var m map[string]any
			if json.Unmarshal([]byte(data), &m) == nil {
				s.lines <- m
			}
		}
	}()
	s.id, _ = s.next()["stream"].(string)
	if s.id == "" {
		e.t.Fatal("no stream id")
	}
	return s
}

func (s *pageStream) next() map[string]any {
	s.t.Helper()
	select {
	case m := <-s.lines:
		return m
	case <-time.After(5 * time.Second):
		s.t.Fatal("no message on the page stream")
		return nil
	}
}

// quiet fails when the stream says anything within a moment.
func (s *pageStream) quiet() {
	s.t.Helper()
	select {
	case m := <-s.lines:
		s.t.Fatalf("the stream said %v, want nothing", m)
	case <-time.After(150 * time.Millisecond):
	}
}

func (e *interviewsE2E) follow(s *pageStream, session string) {
	e.t.Helper()
	body, _ := json.Marshal(map[string]string{"stream": s.id, "path": "/sessions/" + session + "/interviews/events"})
	res, err := http.Post(e.srv.URL+"/api/stream/follow", "application/json", bytes.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		e.t.Fatalf("follow: %d", res.StatusCode)
	}
}

// An interview asked on a live session is read back with every field of the
// contract's shape, and is still there after the server restarts.
func TestAnAskedInterviewIsReadWithEveryContractFieldAndSurvivesARestart(t *testing.T) {
	e := newInterviewsE2E(t)
	defer func() { e.stop() }()
	e.session("s1", domain.SessionRunning, "")
	before := time.Now().Add(-time.Second)
	e.ask(interview("iv-1", "s1"))

	res, err := http.Get(e.srv.URL + "/api/sessions/s1/interviews")
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Seq        int64             `json:"seq"`
		Interviews []json.RawMessage `json:"interviews"`
	}
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if raw.Seq != 1 || len(raw.Interviews) != 1 {
		t.Fatalf("body = %+v, want seq 1 and one interview", raw)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw.Interviews[0], &fields); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"id", "session_id", "agent_name", "title", "asked_at", "state", "ended_at", "delivery", "delivered_at", "questions", "answers", "dismiss_note"} {
		if _, ok := fields[f]; !ok {
			t.Errorf("the interview has no %q field: %s", f, raw.Interviews[0])
		}
	}
	for _, f := range []string{"ended_at", "delivery", "delivered_at", "answers", "dismiss_note"} {
		if string(fields[f]) != "null" {
			t.Errorf("%s = %s on an open interview, want null", f, fields[f])
		}
	}
	var qs []map[string]json.RawMessage
	if err := json.Unmarshal(fields["questions"], &qs); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"title", "body", "kind", "options", "recommended"} {
		if _, ok := qs[0][f]; !ok {
			t.Errorf("a question has no %q field: %s", f, fields["questions"])
		}
	}

	check := func(when string) {
		t.Helper()
		list := e.read("s1")
		if list.Seq != 1 || len(list.Interviews) != 1 {
			t.Fatalf("%s: list = %+v", when, list)
		}
		got := list.Interviews[0]
		if got.ID != "iv-1" || got.SessionID != "s1" || got.AgentName != "claude" || got.Title != "Storage" || got.State != domain.InterviewOpen ||
			got.EndedAt != nil || got.Delivery != nil || got.DeliveredAt != nil || got.Answers != nil || got.DismissNote != nil {
			t.Errorf("%s: interview = %+v", when, got)
		}
		if got.AskedAt.Before(before) || got.AskedAt.After(time.Now()) {
			t.Errorf("%s: asked_at = %v", when, got.AskedAt)
		}
		if len(got.Questions) != 2 {
			t.Fatalf("%s: questions = %+v", when, got.Questions)
		}
		q, w := got.Questions[0], interview("", "").Questions
		if q.Title != "Which store?" || q.Body != "Pick **one**." || q.Kind != domain.QuestionSingle || len(q.Options) != 2 ||
			q.Options[0] != w[0].Options[0] || q.Options[1].Label != "Postgres" || q.Options[1].Description != "" ||
			q.Recommended == nil || q.Recommended.Option == nil || *q.Recommended.Option != 0 || q.Recommended.Text != nil {
			t.Errorf("%s: question 1 = %+v", when, q)
		}
		q = got.Questions[1]
		if q.Kind != domain.QuestionText || q.Options == nil || len(q.Options) != 0 || q.Recommended == nil || q.Recommended.Text == nil || *q.Recommended.Text != "Because it is simple." || q.Recommended.Option != nil {
			t.Errorf("%s: question 2 = %+v", when, q)
		}
	}
	check("before the restart")
	e.restart()
	check("after the restart")
}

// The agent's name is the catalog's name of the session's agent; a session
// with none, or whose agent the catalog no longer has, is "claude".
func TestAnInterviewNamesTheAskingSessionsAgent(t *testing.T) {
	e := newInterviewsE2E(t)
	defer func() { e.stop() }()
	agent, err := e.cat.Agents.CreateAgent(e.ctx, "reviewer", "Reviews.", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e.session("plain", domain.SessionRunning, "")
	e.session("named", domain.SessionRunning, agent.ID)
	e.session("gone", domain.SessionRunning, "deleted-agent")
	for session, want := range map[string]string{"plain": "claude", "named": "reviewer", "gone": "claude"} {
		if got := e.ask(interview("iv-"+session, session)).AgentName; got != want {
			t.Errorf("%s: agent_name = %q, want %q", session, got, want)
		}
	}
}

// A client following a session's interview feed on the page stream gets the
// asked interview with a seq above the seed's, and numbers count up by one;
// another session's interviews are in neither the feed nor the seed.
func TestTheInterviewFeedCarriesAskedInterviewsAndLeaksNoOtherSessions(t *testing.T) {
	e := newInterviewsE2E(t)
	defer func() { e.stop() }()
	e.session("s1", domain.SessionRunning, "")
	e.session("s2", domain.SessionRunning, "")
	e.ask(interview("old", "s1"))
	seed := e.read("s1")
	if seed.Seq != 1 {
		t.Fatalf("seed seq = %d", seed.Seq)
	}

	s := e.openStream()
	e.follow(s, "s1")
	e.ask(interview("other", "s2"))
	s.quiet()
	e.ask(interview("new", "s1"))

	m := s.next()
	if m["path"] != "/sessions/s1/interviews/events" {
		t.Fatalf("message = %v", m)
	}
	data := m["data"].(map[string]any)
	if data["kind"] != "interview_asked" || data["seq"] != float64(seed.Seq+1) {
		t.Fatalf("event = %v, want interview_asked with seq %d", data, seed.Seq+1)
	}
	if iv := data["interview"].(map[string]any); iv["id"] != "new" || iv["session_id"] != "s1" || iv["state"] != "open" {
		t.Fatalf("interview = %v", iv)
	}

	// A change of an interview is carried whole, with the next seq.
	changed := *interviewOf(t, e.read("s1"), "old")
	changed.State, changed.DismissNote = domain.InterviewDismissed, new(string)
	if _, err := e.svc.Change(e.ctx, changed); err != nil {
		t.Fatal(err)
	}
	data = s.next()["data"].(map[string]any)
	if data["kind"] != "interview_changed" || data["seq"] != float64(seed.Seq+2) {
		t.Fatalf("event = %v, want interview_changed with seq %d", data, seed.Seq+2)
	}

	for _, iv := range e.read("s1").Interviews {
		if iv.SessionID != "s1" {
			t.Errorf("the seed of s1 holds %+v", iv)
		}
	}
	if list := e.read("s2"); list.Seq != 1 || len(list.Interviews) != 1 || list.Interviews[0].ID != "other" {
		t.Errorf("s2 = %+v", list)
	}
}

// The list puts open interviews first, oldest first, then the closed ones,
// the last to end first, and holds only the 50 most recent closed ones.
func TestClosedInterviewsAreCappedAtFiftyAndOrdered(t *testing.T) {
	e := newInterviewsE2E(t)
	defer func() { e.stop() }()
	e.session("s1", domain.SessionRunning, "")
	const total = 53
	for i := range total {
		e.ask(interview(fmt.Sprintf("iv-%02d", i), "s1"))
	}
	// Close them in id order, except that the last one asked stays open, and
	// so does the first.
	for i := 1; i < total-1; i++ {
		iv := *interviewOf(t, e.read("s1"), fmt.Sprintf("iv-%02d", i))
		end := time.Now().Add(time.Duration(i) * time.Minute)
		iv.State, iv.EndedAt = domain.InterviewExpired, &end
		if _, err := e.svc.Change(e.ctx, iv); err != nil {
			t.Fatal(err)
		}
	}

	check := func(when string) {
		t.Helper()
		list := e.read("s1")
		if len(list.Interviews) != 2+domain.MaxClosedInterviews {
			t.Fatalf("%s: %d interviews, want 2 open and %d closed", when, len(list.Interviews), domain.MaxClosedInterviews)
		}
		if list.Interviews[0].ID != "iv-00" || list.Interviews[1].ID != "iv-52" {
			t.Errorf("%s: open ones = %s, %s; want the oldest first", when, list.Interviews[0].ID, list.Interviews[1].ID)
		}
		for i, iv := range list.Interviews[2:] {
			if want := fmt.Sprintf("iv-%02d", 51-i); iv.ID != want {
				t.Fatalf("%s: closed #%d = %s, want %s (the last to end first)", when, i, iv.ID, want)
			}
		}
		if list.Interviews[len(list.Interviews)-1].ID != "iv-02" {
			t.Errorf("%s: the oldest closed one kept is %s, want iv-02", when, list.Interviews[len(list.Interviews)-1].ID)
		}
	}
	check("live")
	e.restart()
	check("after a restart")
}

// Asking for a session that is not live is refused, and nothing is stored.
func TestAskingForASessionThatIsNotLiveIsRefusedAndStoresNothing(t *testing.T) {
	e := newInterviewsE2E(t)
	defer func() { e.stop() }()
	e.session("done", domain.SessionDone, "")
	e.session("failed", domain.SessionFailed, "")
	e.session("stopped", domain.SessionStopped, "")
	for _, id := range []string{"done", "failed", "stopped", "nobody"} {
		_, err := e.svc.Ask(e.ctx, interview("iv-"+id, id))
		var coded *domain.StructuredError
		if !errors.As(err, &coded) || coded.Code != "INTERVIEW_UNAVAILABLE" {
			t.Errorf("ask for %s = %v, want INTERVIEW_UNAVAILABLE", id, err)
		}
		if list := e.read(id); list.Seq != 0 || len(list.Interviews) != 0 {
			t.Errorf("%s stored %+v", id, list)
		}
	}
	if list := e.read("done"); list.Interviews == nil {
		t.Error("an empty list must be [], never null")
	}
}
