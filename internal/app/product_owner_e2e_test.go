//go:build !windows

package app

import (
	"bufio"
	"bytes"
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

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/productowner"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// poE2E is the server wired as it runs, with the product owner's HTTP API on a
// test server and a shell script standing in for the headless claude. The
// script answers each message with "answer N to: <text>" streamed in two
// deltas, and records how it was launched, so the tests see what the server
// asked of claude. restart stops the server and starts it over the same data.
type poE2E struct {
	t       *testing.T
	ctx     context.Context
	cfg     Config
	app     *fx.App
	srv     *httptest.Server
	orch    *orchestration.Service
	plan    *planning.Service
	cat     catalog.Catalog
	po      *productowner.Service
	sess    ports.SessionRepository
	project string
	repo    string
	// fakeDir holds the stand-in claude, its launch log and its gates.
	fakeDir string

	feed *poFeed
}

// fakeHeadlessClaude is the stand-in claude. It speaks stream-json on stdin
// and stdout like the real CLI: for each user message it streams the answer
// in two deltas, then the whole assistant message and the turn's result.
// While the file "hold" exists beside it, it stops after the first delta, so a
// test can act in the middle of a turn. A line "slow" in "mode" makes it hold
// every turn.
const fakeHeadlessClaude = `#!/bin/sh
dir=$(dirname "$0")
echo "$*" >> "$dir/launches"
printf '%s\n' '{"type":"system","subtype":"init","session_id":"fake"}'
n=0
while IFS= read -r line; do
  n=$((n+1))
  text=$(printf '%s' "$line" | sed -n 's/.*"text":"\([^"]*\)".*/\1/p')
  echo "$text" >> "$dir/received"
  printf '%s\n' '{"type":"stream_event","session_id":"fake","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"answer to: "}}}'
  while [ -f "$dir/hold" ]; do sleep 0.05; done
  printf '%s\n' '{"type":"stream_event","session_id":"fake","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"'"$text"'"}}}'
  printf '%s\n' '{"type":"assistant","session_id":"fake","message":{"role":"assistant","content":[{"type":"text","text":"answer to: '"$text"'"}]}}'
  printf '%s\n' '{"type":"result","subtype":"success","session_id":"fake","result":"ok","total_cost_usd":0.02,"usage":{"input_tokens":5,"output_tokens":9}}'
done
`

// newPOE2E starts the server over a database file, with a project of one
// repository.
func newPOE2E(t *testing.T) *poE2E {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	gitInit(t, repoDir)
	fakeDir := filepath.Join(dir, "fake")
	if err := os.MkdirAll(fakeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(fakeDir, "claude")
	if err := os.WriteFile(fake, []byte(fakeHeadlessClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	e := &poE2E{t: t, ctx: context.Background(), fakeDir: fakeDir}
	e.cfg = Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "po.db"), DataDir: filepath.Join(dir, "data"), Root: dir, ClaudeBin: fake, SessionShell: "direct"}
	e.boot()
	t.Cleanup(func() { _ = e.app.Stop(context.Background()) })
	if _, err := e.cat.Settings.UpdateSettings(map[string]string{domain.SettingWorkspacesRoot: filepath.Join(dir, "worktrees")}); err != nil {
		t.Fatal(err)
	}
	p, err := e.cat.Projects.CreateProject(e.ctx, "harness", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.cat.Projects.CreateRepository(e.ctx, p.ID, "harness", "", "https://example.com/harness.git", repoDir)
	if err != nil {
		t.Fatal(err)
	}
	e.project, e.repo = p.ID, r.ID
	e.serve()
	return e
}

func (e *poE2E) boot() {
	e.t.Helper()
	e.app = fx.New(
		fx.Supply(e.cfg),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, ProductOwnerModule,
		fx.Populate(&e.orch, &e.plan, &e.cat, &e.po, &e.sess),
		fx.NopLogger,
	)
	if err := e.app.Start(e.ctx); err != nil {
		e.t.Fatal(err)
	}
}

func (e *poE2E) serve() {
	e.t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Sessions: e.orch, Planning: e.plan, Projects: e.cat.Projects, Settings: e.cat.Settings, ProductOwner: e.po,
	})))
	e.srv = httptest.NewServer(mux)
	e.t.Cleanup(e.srv.Close)
	e.feed = followPOFeed(e.t, e.srv.URL, e.project)
}

// restart stops the server and starts it again over the same data. The test
// server comes back on a new address, so followers reconnect.
func (e *poE2E) restart() {
	e.t.Helper()
	e.feed.stop() // an open feed would keep the test server from closing
	e.srv.Close()
	if err := e.app.Stop(e.ctx); err != nil {
		e.t.Fatal(err)
	}
	e.boot()
	e.serve()
}

// get reads a route of the API and decodes its answer into out; it answers the
// status and, for an error, the code.
func (e *poE2E) get(path string, out any) (status int, code string) {
	e.t.Helper()
	res, err := http.Get(e.srv.URL + "/api/" + path)
	if err != nil {
		e.t.Fatal(err)
	}
	return decodeAnswer(e.t, res, out)
}

func (e *poE2E) post(path string, body any, out any) (status int, code string) {
	e.t.Helper()
	b, _ := json.Marshal(body)
	res, err := http.Post(e.srv.URL+"/api/"+path, "application/json", bytes.NewReader(b))
	if err != nil {
		e.t.Fatal(err)
	}
	return decodeAnswer(e.t, res, out)
}

func decodeAnswer(t *testing.T, res *http.Response, out any) (int, string) {
	t.Helper()
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode/100 != 2 {
		var e struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(b, &e)
		return res.StatusCode, e.Code
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("answer %s: %v", b, err)
		}
	}
	return res.StatusCode, ""
}

// poFeed follows a project's feed, the page stream's source, for the whole test.
type poFeed struct {
	stop    context.CancelFunc
	mu      sync.Mutex
	changes []map[string]json.RawMessage
}

func followPOFeed(t *testing.T, base, project string) *poFeed {
	t.Helper()
	f := &poFeed{}
	ctx, cancel := context.WithCancel(context.Background())
	f.stop = cancel
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/api/events?project_id="+project, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			data, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var c map[string]json.RawMessage
			if json.Unmarshal([]byte(data), &c) == nil {
				f.mu.Lock()
				f.changes = append(f.changes, c)
				f.mu.Unlock()
			}
		}
	}()
	return f
}

// of lists the changes under key seen so far, each decoded as a generic object.
func (f *poFeed) of(key string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, c := range f.changes {
		if raw, ok := c[key]; ok {
			var v map[string]any
			if json.Unmarshal(raw, &v) == nil {
				out = append(out, v)
			}
		}
	}
	return out
}

// eventually polls cond until it holds, failing with what it last said.
func eventuallyPO(t *testing.T, what string, cond func() string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		if last = cond(); last == "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s: %s", what, last)
}

type poSettings struct {
	ProjectID string `json:"project_id"`
	Enabled   bool   `json:"enabled"`
	Agent     *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"agent"`
	Prompt     string  `json:"prompt"`
	DigestTime *string `json:"digest_time"`
	TimeZone   string  `json:"time_zone"`
}

func (e *poE2E) settings() poSettings {
	e.t.Helper()
	var s poSettings
	if status, code := e.get("product_owner?project_id="+e.project, &s); status != 200 {
		e.t.Fatalf("GET product_owner = %d %s", status, code)
	}
	return s
}

func TestAProjectsProductOwnerIsOnWithNothingElseSetUntilThePersonChangesIt(t *testing.T) {
	e := newPOE2E(t)

	s := e.settings()
	if s.ProjectID != e.project || !s.Enabled || s.Agent != nil || s.Prompt != "" || s.DigestTime != nil || s.TimeZone == "" {
		t.Fatalf("fresh settings = %+v, want on, no agent, no prompt, no digest time, a time zone", s)
	}
	if status, code := e.get("product_owner?project_id=nope", nil); status != 404 || code != "PROJECT_NOT_FOUND" {
		t.Errorf("unknown project = %d %s, want 404 PROJECT_NOT_FOUND", status, code)
	}
}

func TestAnUpdateChangesOnlyTheFieldsSentAndAnswersTheSettings(t *testing.T) {
	e := newPOE2E(t)
	agent, err := e.cat.Agents.CreateAgent(e.ctx, "product-manager", "shapes features", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	var got poSettings
	if status, code := e.post("update_product_owner", map[string]any{"project_id": e.project, "agent_id": agent.ID, "prompt": "Answer in Portuguese.", "digest_time": "08:30"}, &got); status != 200 {
		t.Fatalf("update = %d %s", status, code)
	}
	if got.Agent == nil || got.Agent.ID != agent.ID || got.Agent.Name != "product-manager" || got.Prompt != "Answer in Portuguese." || got.DigestTime == nil || *got.DigestTime != "08:30" || !got.Enabled {
		t.Fatalf("answer = %+v", got)
	}
	if read := e.settings(); read.Prompt != got.Prompt || read.Agent == nil || read.Agent.ID != agent.ID {
		t.Errorf("GET after update = %+v, want the answer %+v", read, got)
	}

	// Switching it off leaves the other fields as they were.
	if status, code := e.post("update_product_owner", map[string]any{"project_id": e.project, "enabled": false}, &got); status != 200 {
		t.Fatalf("switch off = %d %s", status, code)
	}
	if got.Enabled || got.Prompt != "Answer in Portuguese." || got.Agent == nil || got.DigestTime == nil {
		t.Errorf("after switching off = %+v, want only enabled changed", got)
	}

	// An empty agent id and an empty time clear them.
	if status, code := e.post("update_product_owner", map[string]any{"project_id": e.project, "agent_id": "", "digest_time": ""}, &got); status != 200 {
		t.Fatalf("clear = %d %s", status, code)
	}
	if got.Agent != nil || got.DigestTime != nil || got.Prompt != "Answer in Portuguese." {
		t.Errorf("after clearing = %+v, want no agent and no time, prompt kept", got)
	}
}

func TestAnUpdateIsRefusedForAnUnknownAgentAMalformedTimeAndAnOverlongPrompt(t *testing.T) {
	e := newPOE2E(t)
	for _, c := range []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"unknown agent", map[string]any{"project_id": e.project, "agent_id": "nope"}, 404, "AGENT_NOT_FOUND"},
		{"malformed time", map[string]any{"project_id": e.project, "digest_time": "8:30"}, 400, "INVALID_INPUT"},
		{"time out of range", map[string]any{"project_id": e.project, "digest_time": "24:00"}, 400, "INVALID_INPUT"},
		{"prompt too long", map[string]any{"project_id": e.project, "prompt": strings.Repeat("x", 20001)}, 400, "INVALID_INPUT"},
		{"unknown project", map[string]any{"project_id": "nope", "enabled": false}, 404, "PROJECT_NOT_FOUND"},
	} {
		if status, code := e.post("update_product_owner", c.body, nil); status != c.status || code != c.code {
			t.Errorf("%s = %d %s, want %d %s", c.name, status, code, c.status, c.code)
		}
	}
	if s := e.settings(); !s.Enabled || s.Prompt != "" || s.DigestTime != nil {
		t.Errorf("a refused update changed the settings: %+v", s)
	}
}

func TestTheSettingsSurviveARestartAndEveryUpdateReachesTheProjectsFeed(t *testing.T) {
	e := newPOE2E(t)
	if status, code := e.post("update_product_owner", map[string]any{"project_id": e.project, "enabled": false, "prompt": "Be brief."}, nil); status != 200 {
		t.Fatalf("update = %d %s", status, code)
	}
	eventuallyPO(t, "the settings event", func() string {
		for _, c := range e.feed.of("product_owner_settings") {
			s, _ := c["settings"].(map[string]any)
			if c["project_id"] == e.project && s["enabled"] == false && s["prompt"] == "Be brief." {
				return ""
			}
		}
		return "no product_owner_settings event with the new settings"
	})

	e.restart()
	if s := e.settings(); s.Enabled || s.Prompt != "Be brief." {
		t.Errorf("after the restart = %+v, want off with its prompt", s)
	}

	// Another project's follower hears nothing of this project.
	other, err := e.cat.Projects.CreateProject(e.ctx, "other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	otherFeed := followPOFeed(t, e.srv.URL, other.ID)
	if status, _ := e.post("update_product_owner", map[string]any{"project_id": e.project, "prompt": "Again."}, nil); status != 200 {
		t.Fatal("update failed")
	}
	eventuallyPO(t, "the event after the restart", func() string {
		if len(e.feed.of("product_owner_settings")) == 0 {
			return "no event on the project's own feed"
		}
		return ""
	})
	time.Sleep(100 * time.Millisecond)
	if got := otherFeed.of("product_owner_settings"); len(got) != 0 {
		t.Errorf("another project's feed got %v", got)
	}
}
