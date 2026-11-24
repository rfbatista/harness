//go:build !windows

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/in/mcpsession"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/agentboard"
	"operators-mcp/internal/application/artifacts"
	"operators-mcp/internal/application/notifications"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/planning"
	"operators-mcp/internal/application/productowner"
	"operators-mcp/internal/application/taskchannel"
	"operators-mcp/internal/application/tooling"
	"operators-mcp/internal/application/usage"
	"operators-mcp/internal/application/vfs"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
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
	store   ports.ProductOwnerStore
	shells  *vfs.Service
	board   *agentboard.Service
	usage   *usage.Service
	notes   *notifications.Service
	channel *taskchannel.Service
	art     *artifacts.Service
	project string
	repo    string
	// fakeDir holds the stand-in claude, its launch log and its gates.
	fakeDir string

	feed    *poFeed
	sandbox *portstest.RecordingSandboxDriver
}

// fakeHeadlessClaude is the stand-in claude. It speaks stream-json on stdin
// and stdout like the real CLI: for each user message it streams the answer
// in two deltas, then the whole assistant message and the turn's result.
// While the file "hold" exists beside it, it stops after the first delta, so a
// test can act in the middle of a turn. A line "slow" in "mode" makes it hold
// every turn.
const fakeHeadlessClaude = `#!/bin/sh
dir=$(dirname "$0")
echo "$*" > "$dir/launch.$$"
echo "$ANTHROPIC_CUSTOM_HEADERS" >> "$dir/headers"
prev=""
for a in "$@"; do
  if [ "$prev" = "--mcp-config" ]; then cat "$a" >> "$dir/mcpconfig" 2>/dev/null; echo >> "$dir/mcpconfig"; fi
  prev="$a"
done
printf '%s\n' '{"type":"system","subtype":"init","session_id":"fake"}'
n=0
while IFS= read -r line; do
  n=$((n+1))
  text=$(printf '%s' "$line" | sed -n 's/.*"text":"\([^"]*\)".*/\1/p')
  echo "$text" >> "$dir/received"
  inp=$(cat "$dir/usage" 2>/dev/null || echo 5)
  case "$text" in Checkpoint:*) while [ -f "$dir/summaryhold" ]; do sleep 0.05; done; [ -f "$dir/summaryfail" ] && exit 1;; esac
  while [ -f "$dir/prehold" ]; do sleep 0.05; done
  case "$text" in read:*) printf '%s\n' '{"type":"assistant","session_id":"fake","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"project/docs/prd.md"}}]}}';; esac
  printf '%s\n' '{"type":"stream_event","session_id":"fake","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"answer to: "}}}'
  while [ -f "$dir/hold" ]; do sleep 0.05; done
  printf '%s\n' '{"type":"stream_event","session_id":"fake","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"'"$text"'"}}}'
  printf '%s\n' '{"type":"assistant","session_id":"fake","message":{"role":"assistant","content":[{"type":"text","text":"answer to: '"$text"'"}]}}'
  printf '%s\n' '{"type":"result","subtype":"success","session_id":"fake","result":"ok","total_cost_usd":0.02,"usage":{"input_tokens":'"$inp"',"output_tokens":9}}'
done
`

// newPOE2E starts the server over a database file, with a project of one
// repository.
func newPOE2E(t *testing.T) *poE2E { return newPOE2EWith(t, nil) }

// newPOE2EWith is newPOE2E with tweak applied to the server's configuration
// before it boots.
func newPOE2EWith(t *testing.T, tweak func(*Config)) *poE2E {
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
	if tweak != nil {
		tweak(&e.cfg)
	}
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
	// The host's sandbox cannot nest in the one the tests run under: a driver
	// that records each policy and confines nothing stands in. It is put in
	// before the boot's recovery can start a session.
	e.sandbox = &portstest.RecordingSandboxDriver{}
	e.app = fx.New(
		fx.Supply(e.cfg),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, NotificationsModule, AgentBoardModule, UsageModule,
		fx.Invoke(func(orch *orchestration.Service) {
			if orch.Sandbox != nil {
				// The gate holds a start only while the file "slowstart" exists.
				orch.Sandbox.Driver = gatedSandbox{SandboxDriver: e.sandbox, gate: filepath.Join(e.fakeDir, "slowstart")}
			}
		}),
		ProductOwnerModule,
		fx.Populate(&e.orch, &e.plan, &e.cat, &e.po, &e.sess, &e.store, &e.shells, &e.channel, &e.art, &e.board, &e.usage, &e.notes),
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
		Sessions: e.orch, Planning: e.plan, Projects: e.cat.Projects, Settings: e.cat.Settings, ProductOwner: e.po, AgentBoard: e.board, Usage: e.usage,
	})))
	mux.Handle(mcpsession.PathPrefix, sessionTaskHandler(sessionTaskTools(e.cat, e.orch, e.plan, e.sess, e.art, e.channel, nil, nil), e.plan, e.sess))
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

// changesOf lists, as text, the changes under key seen so far.
func (f *poFeed) changesOf(key string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.changes {
		if raw, ok := c[key]; ok {
			out = append(out, string(raw))
		}
	}
	return out
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

type poItem struct {
	ItemID      string `json:"item_id"`
	Kind        string `json:"kind"`
	Author      string `json:"author"`
	Text        string `json:"text"`
	Streaming   *bool  `json:"streaming"`
	Stopped     bool   `json:"stopped"`
	Attachments []any  `json:"attachments"`
	Summary     string `json:"summary"`
}

type poThread struct {
	Thread struct {
		ThreadID  string `json:"thread_id"`
		ProjectID string `json:"project_id"`
		Title     string `json:"title"`
		Turn      string `json:"turn"`
		SessionID string `json:"session_id"`
	} `json:"thread"`
	Items      []poItem `json:"items"`
	HasEarlier bool     `json:"has_earlier"`
}

func (e *poE2E) startThread(message string) (threadID, itemID string) {
	e.t.Helper()
	var out struct {
		ThreadID string `json:"thread_id"`
		ItemID   string `json:"item_id"`
	}
	if status, code := e.post("start_product_owner_thread", map[string]any{"project_id": e.project, "message": message}, &out); status != 200 {
		e.t.Fatalf("start = %d %s", status, code)
	}
	return out.ThreadID, out.ItemID
}

func (e *poE2E) send(threadID, message string) (status int, code string) {
	e.t.Helper()
	return e.post("send_product_owner_message", map[string]any{"thread_id": threadID, "message": message}, nil)
}

func (e *poE2E) thread(id string) poThread {
	e.t.Helper()
	var out poThread
	if status, code := e.get("product_owner_thread?thread_id="+id, &out); status != 200 {
		e.t.Fatalf("GET thread = %d %s", status, code)
	}
	return out
}

// waitTurn waits until the thread's turn is want.
func (e *poE2E) waitTurn(id, want string) poThread {
	e.t.Helper()
	var got poThread
	eventuallyPO(e.t, "the turn to be "+want, func() string {
		got = e.thread(id)
		if got.Thread.Turn != want {
			return "turn is " + got.Thread.Turn
		}
		return ""
	})
	return got
}

// launches are the command lines the stand-in claude was started with, one
// entry per start or resume, oldest first. Each process writes a file of its
// own: two started together would interleave in one file.
func (e *poE2E) launches() []string {
	e.t.Helper()
	files, _ := filepath.Glob(filepath.Join(e.fakeDir, "launch.*"))
	type launch struct {
		at   time.Time
		name string
		text string
	}
	var all []launch
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		b, _ := os.ReadFile(f)
		all = append(all, launch{info.ModTime(), f, strings.TrimRight(string(b), "\n")})
	}
	slices.SortFunc(all, func(x, y launch) int {
		if c := x.at.Compare(y.at); c != 0 {
			return c
		}
		return strings.Compare(x.name, y.name)
	})
	out := make([]string, len(all))
	for i, l := range all {
		out[i] = l.text
	}
	return out
}

func TestAThreadStartsWithAMessageAndStreamsTheProductOwnersAnswer(t *testing.T) {
	e := newPOE2E(t)

	threadID, itemID := e.startThread("What was done this week?")
	if threadID == "" || itemID == "" {
		t.Fatalf("start answered thread %q item %q", threadID, itemID)
	}
	got := e.waitTurn(threadID, "idle")
	if got.Thread.ProjectID != e.project || got.Thread.Title != "What was done this week?" || got.Thread.SessionID == "" || got.HasEarlier {
		t.Errorf("thread = %+v", got.Thread)
	}
	var messages []poItem
	for _, it := range got.Items {
		if it.Kind == "message" {
			messages = append(messages, it)
		}
	}
	if len(messages) != 2 {
		t.Fatalf("items = %+v, want the person's message then the answer", got.Items)
	}
	person, answer := messages[0], messages[1]
	if person.ItemID != itemID || person.Author != "person" || person.Text != "What was done this week?" || person.Attachments == nil {
		t.Errorf("first message = %+v", person)
	}
	if answer.Author != "product_owner" || answer.Text != "answer to: What was done this week?" || answer.Streaming == nil || *answer.Streaming {
		t.Errorf("answer = %+v, want the whole text and streaming false", answer)
	}

	// The feed carried the deltas, then the final item, and the turn's
	// changes.
	eventuallyPO(t, "the stream of the answer", func() string {
		var deltas string
		for _, d := range e.feed.of("product_owner_delta") {
			if d["thread_id"] == threadID && d["item_id"] == answer.ItemID {
				deltas += d["text"].(string)
			}
		}
		if deltas != "answer to: What was done this week?" {
			return "deltas = " + deltas
		}
		final := false
		for _, c := range e.feed.of("product_owner_thread_item") {
			it, _ := c["item"].(map[string]any)
			if it["item_id"] == answer.ItemID && it["streaming"] == false && it["text"] == answer.Text {
				final = true
			}
		}
		if !final {
			return "no final thread item"
		}
		var turns []string
		for _, c := range e.feed.of("product_owner_thread") {
			th, _ := c["thread"].(map[string]any)
			if th["thread_id"] == threadID {
				turns = append(turns, th["turn"].(string))
			}
		}
		if len(turns) < 2 || turns[0] != "running" || turns[len(turns)-1] != "idle" {
			return "turns = " + strings.Join(turns, ",")
		}
		return ""
	})
}

func TestAThreadsSessionRunsInProductOwnerModeOnItsProjectWithTheBriefItsModelAndNoTask(t *testing.T) {
	e := newPOE2E(t)
	threadID, _ := e.startThread("hello")
	th := e.waitTurn(threadID, "idle")

	sess := e.sess.Get(th.Thread.SessionID)
	if sess == nil {
		t.Fatal("the thread has no session")
	}
	if sess.Mode != domain.SessionModeProductOwner || sess.ProjectID != e.project || sess.TicketID != "" || sess.Interactive {
		t.Errorf("session = mode %q project %q task %q interactive %v", sess.Mode, sess.ProjectID, sess.TicketID, sess.Interactive)
	}
	launches := e.launches()
	if len(launches) != 1 {
		t.Fatalf("launches = %q", launches)
	}
	args := launches[0]
	if !strings.Contains(args, "--model sonnet") || !strings.Contains(args, "--session-id "+sess.ID) {
		t.Errorf("launch = %.300s, want the built-in model sonnet and the session id", args)
	}
	for _, want := range []string{
		"product owner of project harness", "Say where you looked", "Say so when you cannot know", "has none and offer to shape its first feature",
		"cannot change anything yet", "read another session's transcript or worktree, even when asked",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("the brief lacks %q", want)
		}
	}
	// The sandbox lets it read every repository of the project and write none.
	calls := e.sandbox.Calls()
	if len(calls) == 0 {
		t.Fatal("the session was not put under the sandbox")
	}
	policy := calls[len(calls)-1].Policy
	repoDir := filepath.Join(filepath.Dir(e.cfg.DBPath), "repo")
	inAny := func(list []string) bool {
		for _, p := range list {
			if strings.HasSuffix(p, "/repo") || p == repoDir {
				return true
			}
		}
		return false
	}
	if !inAny(policy.Readable) || inAny(policy.Writable) {
		t.Errorf("sandbox readable %v writable %v: want the repository read-only", policy.Readable, policy.Writable)
	}
	if strings.Contains(args, "create the feature task") {
		t.Error("the brief is the product session's")
	}

	// A model set for the mode is the model of every new thread.
	if _, err := e.cat.Settings.UpdateSettings(map[string]string{domain.ModeModelKey(domain.SessionModeProductOwner): "haiku"}); err != nil {
		t.Fatal(err)
	}
	e.startThread("again")
	eventuallyPO(t, "the second launch", func() string {
		if l := e.launches(); len(l) != 2 || !strings.Contains(l[1], "--model haiku") {
			return "launches = " + strings.Join(l, "|")
		}
		return ""
	})
}

func TestAStartIsRefusedForABadMessageAnUnknownProjectAndASwitchedOffProductOwner(t *testing.T) {
	e := newPOE2E(t)
	five := []string{"a", "b", "c", "d", "e", "f"}
	for _, c := range []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"unknown project", map[string]any{"project_id": "nope", "message": "hi"}, 404, "PROJECT_NOT_FOUND"},
		{"empty message", map[string]any{"project_id": e.project, "message": "  "}, 400, "INVALID_INPUT"},
		{"message too long", map[string]any{"project_id": e.project, "message": strings.Repeat("x", 20001)}, 400, "INVALID_INPUT"},
		{"too many attachments", map[string]any{"project_id": e.project, "message": "hi", "attachment_ids": five}, 400, "INVALID_INPUT"},
		{"an attachment of no project", map[string]any{"project_id": e.project, "message": "hi", "attachment_ids": []string{"a"}}, 400, "INVALID_INPUT"},
	} {
		if status, code := e.post("start_product_owner_thread", c.body, nil); status != c.status || code != c.code {
			t.Errorf("%s = %d %s, want %d %s", c.name, status, code, c.status, c.code)
		}
	}
	if status, _ := e.post("update_product_owner", map[string]any{"project_id": e.project, "enabled": false}, nil); status != 200 {
		t.Fatal("switch off failed")
	}
	if status, code := e.post("start_product_owner_thread", map[string]any{"project_id": e.project, "message": "hi"}, nil); status != 409 || code != "PRODUCT_OWNER_OFF" {
		t.Errorf("off = %d %s, want 409 PRODUCT_OWNER_OFF", status, code)
	}
	var list struct {
		Threads []any `json:"threads"`
	}
	e.get("product_owner_threads?project_id="+e.project, &list)
	if len(list.Threads) != 0 || len(e.launches()) != 0 {
		t.Errorf("a refused start left %d threads and %d launches", len(list.Threads), len(e.launches()))
	}
	if status, code := e.get("product_owner_thread?thread_id=nope", nil); status != 404 || code != "THREAD_NOT_FOUND" {
		t.Errorf("unknown thread = %d %s, want 404 THREAD_NOT_FOUND", status, code)
	}
}

func TestAStartWhenClaudeCannotRunIsProductOwnerUnavailableAndNamesWhereToFixIt(t *testing.T) {
	e := newPOE2EWith(t, func(c *Config) { c.ClaudeBin = "claude-is-not-installed-here" })
	b, _ := json.Marshal(map[string]any{"project_id": e.project, "message": "hi"})
	res, err := http.Post(e.srv.URL+"/api/start_product_owner_thread", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Code   string `json:"code"`
		FixURL string `json:"fix_url"`
	}
	_ = json.NewDecoder(res.Body).Decode(&body)
	if res.StatusCode != 503 || body.Code != "PRODUCT_OWNER_UNAVAILABLE" || body.FixURL == "" {
		t.Errorf("answer = %d %+v, want 503 PRODUCT_OWNER_UNAVAILABLE with a fix_url", res.StatusCode, body)
	}
}

// seedThread stores a thread of n messages directly, the way the service
// would have: nothing runs for it.
func (e *poE2E) seedThread(title string, activity time.Time, n int) string {
	e.t.Helper()
	id := fmt.Sprintf("seed-%s", title)
	err := e.store.CreateThread(e.ctx, domain.ProductOwnerThread{ID: id, ProjectID: e.project, SessionID: "none-" + id, Title: title,
		Turn: domain.ProductOwnerTurnIdle, StartedAt: activity, LastActivityAt: activity})
	if err != nil {
		e.t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		// The last message is the product owner's, so a restart finds nothing
		// unanswered to resume.
		author := domain.ProductOwnerAuthorPerson
		if i == n {
			author = domain.ProductOwnerAuthorProductOwner
		}
		if _, err := e.store.AppendItem(e.ctx, domain.ProductOwnerItem{ThreadID: id, Kind: domain.ProductOwnerItemMessage, At: activity,
			Author: author, Text: fmt.Sprintf("message %d", i)}); err != nil {
			e.t.Fatal(err)
		}
	}
	return id
}

func TestThreadsListTheMostRecentlyActiveFirstAndKeepTheirTitlesAcrossARestart(t *testing.T) {
	e := newPOE2E(t)
	now := time.Now()
	e.seedThread("oldest", now.Add(-3*time.Hour), 1)
	e.seedThread("newest", now.Add(-1*time.Minute), 1)
	e.seedThread("middle", now.Add(-1*time.Hour), 1)

	read := func() []string {
		var out struct {
			Threads []struct {
				ThreadID           string `json:"thread_id"`
				Title              string `json:"title"`
				StartedAt          string `json:"started_at"`
				LastActivityAt     string `json:"last_activity_at"`
				Turn               string `json:"turn"`
				PendingHeldActions *int   `json:"pending_held_actions"`
			} `json:"threads"`
		}
		if status, code := e.get("product_owner_threads?project_id="+e.project, &out); status != 200 {
			t.Fatalf("list = %d %s", status, code)
		}
		var titles []string
		for _, th := range out.Threads {
			if th.Turn != "idle" || th.StartedAt == "" || th.LastActivityAt == "" || th.PendingHeldActions == nil || *th.PendingHeldActions != 0 {
				t.Errorf("row = %+v", th)
			}
			titles = append(titles, th.Title)
		}
		return titles
	}
	want := "newest,middle,oldest"
	if got := strings.Join(read(), ","); got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
	e.restart()
	if got := strings.Join(read(), ","); got != want {
		t.Errorf("order after the restart = %s, want %s", got, want)
	}
	if status, code := e.get("product_owner_threads?project_id=nope", nil); status != 404 || code != "PROJECT_NOT_FOUND" {
		t.Errorf("unknown project = %d %s", status, code)
	}
}

func TestAThreadOfTwoHundredFiftyItemsIsReadInPagesWithNoGapAndNoRepeat(t *testing.T) {
	e := newPOE2E(t)
	id := e.seedThread("long", time.Now(), 250)

	var texts []string
	before := ""
	for pages := 0; ; pages++ {
		var page poThread
		path := "product_owner_thread?thread_id=" + id
		if before != "" {
			path += "&before=" + before
		}
		if status, code := e.get(path, &page); status != 200 {
			t.Fatalf("read = %d %s", status, code)
		}
		if len(page.Items) > 100 || len(page.Items) == 0 {
			t.Fatalf("page %d has %d items", pages, len(page.Items))
		}
		if pages == 0 && (!page.HasEarlier || page.Items[len(page.Items)-1].Text != "message 250") {
			t.Fatalf("the first page = %d items, has_earlier %v, last %q; want the newest 100", len(page.Items), page.HasEarlier, page.Items[len(page.Items)-1].Text)
		}
		var texts2 []string
		for _, it := range page.Items {
			texts2 = append(texts2, it.Text)
		}
		texts = append(texts2, texts...)
		before = page.Items[0].ItemID
		if !page.HasEarlier {
			break
		}
		if pages > 5 {
			t.Fatal("paging does not end")
		}
		e.restartIf(pages == 0)
	}
	if len(texts) != 250 {
		t.Fatalf("read %d items in all", len(texts))
	}
	for i, text := range texts {
		if want := fmt.Sprintf("message %d", i+1); text != want {
			t.Fatalf("item %d = %q, want %q", i, text, want)
		}
	}
	if status, code := e.get("product_owner_thread?thread_id="+id+"&before=nope", nil); status != 400 || code != "INVALID_INPUT" {
		t.Errorf("a before of no item = %d %s, want 400 INVALID_INPUT", status, code)
	}
}

// restartIf restarts the server when cond holds, so a read is taken up again
// over the stored thread.
func (e *poE2E) restartIf(cond bool) {
	e.t.Helper()
	if cond {
		e.restart()
	}
}

func TestWhatTheProductOwnerReadsLeavesAnActivityLineBeforeItsAnswer(t *testing.T) {
	e := newPOE2E(t)
	id, _ := e.startThread("read: the PRD")
	got := e.waitTurn(id, "idle")
	var kinds []string
	for _, it := range got.Items {
		kinds = append(kinds, it.Kind)
		if it.Kind == "activity" && it.Text != "Reading project/docs/prd.md" {
			t.Errorf("activity = %q", it.Text)
		}
	}
	if strings.Join(kinds, ",") != "message,activity,message" {
		t.Errorf("kinds = %v, want the person's message, the activity, the answer", kinds)
	}
}

// hold keeps the stand-in claude in the middle of each turn, after its first
// delta, until release.
func (e *poE2E) hold() {
	e.t.Helper()
	if err := os.WriteFile(filepath.Join(e.fakeDir, "hold"), nil, 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *poE2E) release() { _ = os.Remove(filepath.Join(e.fakeDir, "hold")) }

func (e *poE2E) messages(id string) []poItem {
	var out []poItem
	for _, it := range e.thread(id).Items {
		if it.Kind == "message" {
			out = append(out, it)
		}
	}
	return out
}

func TestAMessageAfterTheProcessEndedResumesTheSameSessionAndKeepsTheConversation(t *testing.T) {
	e := newPOE2E(t)
	e.po.SetIdle(300 * time.Millisecond)

	id, _ := e.startThread("first")
	th := e.waitTurn(id, "idle")
	eventuallyPO(t, "the idle process to end", func() string {
		if e.orch.ProductOwnerRunning(th.Thread.SessionID) {
			return "the process still runs"
		}
		return ""
	})

	if status, code := e.send(id, "second"); status != 200 {
		t.Fatalf("send = %d %s", status, code)
	}
	e.waitTurn(id, "idle")
	msgs := e.messages(id)
	var texts []string
	for _, m := range msgs {
		texts = append(texts, m.Text)
	}
	want := "first|answer to: first|second|answer to: second"
	if strings.Join(texts, "|") != want {
		t.Errorf("messages = %q, want %q", strings.Join(texts, "|"), want)
	}
	launches := e.launches()
	if len(launches) != 2 || !strings.Contains(launches[1], "--resume "+th.Thread.SessionID) || strings.Contains(launches[1], "--session-id") {
		t.Errorf("launches = %q, want a second one resuming session %s", launches, th.Thread.SessionID)
	}
	if got := e.thread(id).Thread.SessionID; got != th.Thread.SessionID {
		t.Errorf("the thread moved to session %s", got)
	}
}

func TestAMessageDuringATurnIsRefusedAndAStopKeepsThePartialAnswer(t *testing.T) {
	e := newPOE2E(t)
	e.hold()
	id, _ := e.startThread("slow question")
	eventuallyPO(t, "a partial answer", func() string {
		for _, m := range e.messages(id) {
			if m.Author == "product_owner" && m.Text == "answer to: " && m.Streaming != nil && *m.Streaming {
				return ""
			}
		}
		return "no streaming answer yet"
	})
	if got := e.thread(id).Thread.Turn; got != "running" {
		t.Fatalf("turn = %s, want running", got)
	}
	if status, code := e.send(id, "and another"); status != 409 || code != "THREAD_BUSY" {
		t.Errorf("a message during the turn = %d %s, want 409 THREAD_BUSY", status, code)
	}
	if n := len(e.messages(id)); n != 2 {
		t.Errorf("the refused message was stored: %d messages", n)
	}

	// The stand-in ends once it is let go of; a real claude ends on its stdin closing.
	time.AfterFunc(200*time.Millisecond, e.release)
	if status, code := e.post("stop_product_owner_turn", map[string]any{"thread_id": id}, nil); status != 200 {
		t.Fatalf("stop = %d %s", status, code)
	}
	got := e.waitTurn(id, "idle")
	answer := e.messages(id)[1]
	if answer.Text != "answer to: " || answer.Streaming == nil || *answer.Streaming || !answer.Stopped {
		t.Errorf("the stopped answer = %+v, want its partial text, streaming false, stopped", answer)
	}
	_ = got

	// The thread takes a new message; the process is resumed.
	if status, code := e.send(id, "after the stop"); status != 200 {
		t.Fatalf("send after the stop = %d %s", status, code)
	}
	e.waitTurn(id, "idle")
	if n := len(e.messages(id)); n != 4 {
		t.Errorf("messages = %d, want 4", n)
	}
	if status, code := e.post("stop_product_owner_turn", map[string]any{"thread_id": "nope"}, nil); status != 404 || code != "THREAD_NOT_FOUND" {
		t.Errorf("stop of an unknown thread = %d %s", status, code)
	}
}

func TestSwitchingTheProductOwnerOffRefusesMessagesAndLetsARunningTurnFinish(t *testing.T) {
	e := newPOE2E(t)
	e.hold()
	id, _ := e.startThread("question")
	eventuallyPO(t, "the turn to run", func() string {
		if e.thread(id).Thread.Turn != "running" {
			return "not running"
		}
		return ""
	})
	if status, _ := e.post("update_product_owner", map[string]any{"project_id": e.project, "enabled": false}, nil); status != 200 {
		t.Fatal("switch off failed")
	}
	if status, code := e.send(id, "more"); status != 409 || code != "PRODUCT_OWNER_OFF" {
		t.Errorf("send while off = %d %s, want 409 PRODUCT_OWNER_OFF", status, code)
	}
	e.release()
	e.waitTurn(id, "idle")
	if m := e.messages(id); len(m) != 2 || m[1].Text != "answer to: question" {
		t.Errorf("the running turn did not finish: %+v", m)
	}

	if status, _ := e.post("update_product_owner", map[string]any{"project_id": e.project, "enabled": true}, nil); status != 200 {
		t.Fatal("switch on failed")
	}
	if status, code := e.send(id, "back on"); status != 200 {
		t.Errorf("send after switching on = %d %s", status, code)
	}
	e.waitTurn(id, "idle")
}

func TestAMessageIsRefusedWhenItIsEmptyTooLongOrCarriesTooManyAttachments(t *testing.T) {
	e := newPOE2E(t)
	id, _ := e.startThread("q")
	e.waitTurn(id, "idle")
	for name, body := range map[string]map[string]any{
		"empty":       {"thread_id": id, "message": ""},
		"too long":    {"thread_id": id, "message": strings.Repeat("x", 20001)},
		"attachments": {"thread_id": id, "message": "hi", "attachment_ids": []string{"a", "b", "c", "d", "e", "f"}},
	} {
		if status, code := e.post("send_product_owner_message", body, nil); status != 400 || code != "INVALID_INPUT" {
			t.Errorf("%s = %d %s, want 400 INVALID_INPUT", name, status, code)
		}
	}
	if status, code := e.send("nope", "hi"); status != 404 || code != "THREAD_NOT_FOUND" {
		t.Errorf("unknown thread = %d %s", status, code)
	}
}

func TestTwoTabsFollowingAThreadSeeTheSameItemsInTheSameOrder(t *testing.T) {
	e := newPOE2E(t)
	second := followPOFeed(t, e.srv.URL, e.project)
	id, _ := e.startThread("both of you")
	e.waitTurn(id, "idle")

	items := func(f *poFeed) string {
		var seen []string
		for _, c := range f.of("product_owner_thread_item") {
			it, _ := c["item"].(map[string]any)
			seen = append(seen, fmt.Sprintf("%v:%v:%v", it["item_id"], it["streaming"], it["text"]))
		}
		return strings.Join(seen, "\n")
	}
	eventuallyPO(t, "both tabs to have the items", func() string {
		a, b := items(e.feed), items(second)
		if a == "" || a != b {
			return "first tab:\n" + a + "\nsecond tab:\n" + b
		}
		return ""
	})
}

func (e *poE2E) streamingPartial(id string) bool {
	for _, m := range e.messages(id) {
		if m.Author == "product_owner" && m.Streaming != nil && *m.Streaming && m.Text != "" {
			return true
		}
	}
	return false
}

func countItems(items []poItem, kind, text string) int {
	n := 0
	for _, it := range items {
		if it.Kind == kind && (text == "" || strings.Contains(it.Text, text)) {
			n++
		}
	}
	return n
}

func TestARestartInTheMiddleOfATurnResumesItWithANoticeAndLosesNoMessage(t *testing.T) {
	e := newPOE2E(t)
	e.hold()
	id, _ := e.startThread("what is in progress?")
	eventuallyPO(t, "a partial answer", func() string {
		if !e.streamingPartial(id) {
			return "no partial answer"
		}
		return ""
	})

	e.restart()
	e.release()
	got := e.waitTurn(id, "idle")

	if n := countItems(got.Items, "message", "what is in progress?"); n < 1 {
		t.Fatalf("the person's message is gone: %+v", got.Items)
	}
	var persons int
	for _, it := range got.Items {
		if it.Kind == "message" && it.Author == "person" {
			persons++
		}
	}
	if persons != 1 {
		t.Errorf("%d person messages, want the one that was sent", persons)
	}
	if n := countItems(got.Items, "notice", "Continued after a restart"); n != 1 {
		t.Errorf("%d notices, want 1: %+v", n, got.Items)
	}
	var answers []poItem
	for _, it := range got.Items {
		if it.Kind == "message" && it.Author == "product_owner" {
			answers = append(answers, it)
		}
		if it.Streaming != nil && *it.Streaming {
			t.Errorf("an item is still streaming: %+v", it)
		}
	}
	if len(answers) < 2 || answers[0].Text != "answer to: " || answers[0].Stopped || answers[len(answers)-1].Stopped || !strings.Contains(answers[len(answers)-1].Text, "what is in progress?") {
		t.Errorf("answers = %+v, want the cut one kept as it arrived and the continued one whole, answering the message", answers)
	}
	if l := e.launches(); len(l) != 2 || !strings.Contains(l[1], "--resume "+got.Thread.SessionID) {
		t.Errorf("launches = %q, want the session resumed once", l)
	}
	eventuallyPO(t, "the turn change on the feed", func() string {
		for _, c := range e.feed.of("product_owner_thread") {
			if th, _ := c["thread"].(map[string]any); th["thread_id"] == id && th["turn"] == "idle" {
				return ""
			}
		}
		return "no idle turn event after the restart"
	})
}

func TestAnIdleThreadStaysIdleThroughARestartWithNoNoticeAndNoProcess(t *testing.T) {
	e := newPOE2E(t)
	id, _ := e.startThread("hello")
	before := e.waitTurn(id, "idle")
	e.restart()
	time.Sleep(500 * time.Millisecond)
	after := e.thread(id)
	if after.Thread.Turn != "idle" || len(after.Items) != len(before.Items) || countItems(after.Items, "notice", "") != 0 {
		t.Errorf("after the restart = %+v, want the thread as it was", after)
	}
	if len(e.launches()) != 1 || e.orch.ProductOwnerRunning(before.Thread.SessionID) {
		t.Errorf("a restart started claude for an idle thread: %q", e.launches())
	}
}

func TestAThreadWhoseResumeFailsAtBootGetsAFailureItemAndTakesAMessageNeverStuckBusy(t *testing.T) {
	e := newPOE2E(t)
	e.hold()
	id, _ := e.startThread("cut off")
	eventuallyPO(t, "a partial answer", func() string {
		if !e.streamingPartial(id) {
			return "no partial answer"
		}
		return ""
	})

	good := e.cfg.ClaudeBin
	e.cfg.ClaudeBin = "claude-is-not-installed-here"
	e.restart()
	got := e.waitTurn(id, "idle")
	var failure map[string]any
	var generic struct {
		Items []map[string]any `json:"items"`
	}
	e.get("product_owner_thread?thread_id="+id, &generic)
	for _, it := range generic.Items {
		if it["kind"] == "failure" {
			failure = it
		}
	}
	if failure == nil || failure["code"] != "SESSION_RESUME_FAILED" || failure["retry_item_id"] == "" || countItems(got.Items, "notice", "") != 0 {
		t.Fatalf("items = %v, want a SESSION_RESUME_FAILED failure to retry from and no notice", generic.Items)
	}

	// Claude can run again: the thread takes the message, never THREAD_BUSY.
	e.cfg.ClaudeBin = good
	e.restart()
	e.release()
	if status, code := e.send(id, "again"); status != 200 {
		t.Fatalf("send = %d %s, want it taken", status, code)
	}
	e.waitTurn(id, "idle")
}

func TestARestartDuringResumingOrBetweenStoringAMessageAndItsTurnIsRecoveredAndAnswered(t *testing.T) {
	e := newPOE2E(t)
	id, _ := e.startThread("first")
	th := e.waitTurn(id, "idle")

	// Cut off while the session was waking up.
	cutOff, err := e.store.Thread(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	cutOff.Turn = domain.ProductOwnerTurnResuming
	if err := e.store.SaveThread(e.ctx, *cutOff); err != nil {
		t.Fatal(err)
	}
	e.restart()
	e.waitTurn(id, "idle")
	if n := countItems(e.thread(id).Items, "notice", "Continued after a restart"); n != 1 {
		t.Errorf("%d notices after a restart during resuming, want 1", n)
	}
	before := len(e.messages(id))

	// A message stored, and the crash before its turn ever started: the thread
	// is idle with the person's message last.
	if _, err := e.store.AppendItem(e.ctx, domain.ProductOwnerItem{ThreadID: id, Kind: domain.ProductOwnerItemMessage, At: time.Now(),
		Author: domain.ProductOwnerAuthorPerson, Text: "stored but never delivered"}); err != nil {
		t.Fatal(err)
	}
	e.restart()
	eventuallyPO(t, "the unanswered message to be answered", func() string {
		m := e.messages(id)
		if e.thread(id).Thread.Turn != "idle" || len(m) < before+2 || !strings.Contains(m[len(m)-1].Text, "stored but never delivered") {
			return "not answered yet"
		}
		return ""
	})
	_ = th
}

func TestAMessageAtTheIdleBoundaryIsDeliveredByAFreshResumeNeverFailed(t *testing.T) {
	e := newPOE2E(t)
	e.po.SetIdle(150 * time.Millisecond)
	id, _ := e.startThread("first")
	e.waitTurn(id, "idle")
	for i, delay := range []time.Duration{100, 120, 135, 145, 150, 155, 165, 190} {
		time.Sleep(delay * time.Millisecond)
		if status, code := e.send(id, fmt.Sprintf("m%d", i)); status != 200 {
			t.Fatalf("send %d after %dms = %d %s", i, delay, status, code)
		}
		e.waitTurn(id, "idle")
	}
	items := e.thread(id).Items
	if n := countItems(items, "failure", ""); n != 0 {
		t.Errorf("%d failure items: %+v", n, items)
	}
	for i := 0; i < 8; i++ {
		if countItems(items, "message", fmt.Sprintf("answer to: m%d", i)) != 1 {
			t.Errorf("message m%d has no answer: %+v", i, items)
		}
	}
}

func TestARenameDuringATurnIsNotLostWhenTheTurnEnds(t *testing.T) {
	e := newPOE2E(t)
	e.hold()
	id, _ := e.startThread("a title the product owner would keep")
	eventuallyPO(t, "the turn to run", func() string {
		if e.thread(id).Thread.Turn != "running" {
			return "not running"
		}
		return ""
	})
	if status, code := e.post("rename_product_owner_thread", map[string]any{"thread_id": id, "title": "Mine"}, nil); status != 200 {
		t.Fatalf("rename = %d %s", status, code)
	}
	e.release()
	got := e.waitTurn(id, "idle")
	if got.Thread.Title != "Mine" {
		t.Errorf("title = %q after the turn, want the person's", got.Thread.Title)
	}
	var list struct {
		Threads []struct {
			TitleSetByPerson bool   `json:"title_set_by_person"`
			Excerpt          string `json:"last_message_excerpt"`
		} `json:"threads"`
	}
	e.get("product_owner_threads?project_id="+e.project, &list)
	if len(list.Threads) != 1 || !list.Threads[0].TitleSetByPerson || list.Threads[0].Excerpt != "answer to: a title the product owner would keep" {
		t.Errorf("list = %+v, want the rename and the answer's excerpt both kept", list.Threads)
	}
}

func TestAStreamingMessageItemAlwaysCarriesItsTextKey(t *testing.T) {
	e := newPOE2E(t)
	e.hold()
	id, _ := e.startThread("q")
	eventuallyPO(t, "a streaming item on the feed", func() string {
		for _, c := range e.feed.changesOf("product_owner_thread_item") {
			if strings.Contains(c, `"streaming":true`) {
				if !strings.Contains(c, `"text":`) {
					return "a streaming item has no text key: " + c
				}
				return ""
			}
		}
		return "none yet"
	})
	e.release()
	e.waitTurn(id, "idle")
}

func TestTheProductOwnerIsNeverGivenTheServicesServerEvenWhenTheProjectAddsIt(t *testing.T) {
	e := newPOE2E(t)
	if e.orch.ServicesServerURL == nil {
		t.Fatal("the host serves no services server: the test would prove nothing")
	}
	if _, err := e.cat.Projects.SetProjectMCPServers(e.ctx, e.project, []string{"services"}); err != nil {
		t.Fatal(err)
	}
	id, _ := e.startThread("hello")
	e.waitTurn(id, "idle")
	cfg, _ := os.ReadFile(filepath.Join(e.fakeDir, "mcpconfig"))
	if !strings.Contains(string(cfg), "task") {
		t.Fatalf("the MCP config has no task server: %q", cfg)
	}
	if strings.Contains(string(cfg), "services") {
		t.Errorf("the product owner was given the services server: %s", cfg)
	}
	if l := e.launches(); len(l) != 1 || strings.Contains(l[0], "mcp__services__") {
		t.Errorf("the services tools are allowed: %q", l)
	}
}

func TestATurnRunningWhenTheProductOwnerIsSwitchedOffStillResumesAfterARestart(t *testing.T) {
	e := newPOE2E(t)
	e.hold()
	id, _ := e.startThread("question")
	eventuallyPO(t, "a partial answer", func() string {
		if !e.streamingPartial(id) {
			return "no partial answer"
		}
		return ""
	})
	if status, _ := e.post("update_product_owner", map[string]any{"project_id": e.project, "enabled": false}, nil); status != 200 {
		t.Fatal("switch off failed")
	}
	e.restart()
	e.release()
	got := e.waitTurn(id, "idle")
	if last := e.messages(id); !strings.Contains(last[len(last)-1].Text, "question") || countItems(got.Items, "notice", "") != 1 {
		t.Errorf("the turn did not finish after the restart: %+v", got.Items)
	}
	if status, code := e.send(id, "more"); status != 409 || code != "PRODUCT_OWNER_OFF" {
		t.Errorf("a new message = %d %s, want 409 PRODUCT_OWNER_OFF", status, code)
	}
}

// dialSession is an MCP client on a session's own task endpoint, as its claude
// would be.
func (e *poE2E) dialSession(sessionID string) *client.Client {
	e.t.Helper()
	trans, err := transport.NewStreamableHTTP(e.srv.URL + mcpsession.PathPrefix + sessionID)
	if err != nil {
		e.t.Fatal(err)
	}
	c := client.NewClient(trans)
	e.t.Cleanup(func() { _ = trans.Close() })
	var req mcplib.InitializeRequest
	req.Params.ProtocolVersion = mcplib.LATEST_PROTOCOL_VERSION
	req.Params.ClientInfo = mcplib.Implementation{Name: "test", Version: "0.0.1"}
	if _, err := c.Initialize(e.ctx, req); err != nil {
		e.t.Fatal(err)
	}
	return c
}

func (e *poE2E) toolNames(c *client.Client) []string {
	e.t.Helper()
	listed, err := c.ListTools(e.ctx, mcplib.ListToolsRequest{})
	if err != nil {
		e.t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

// call runs a tool and answers its text and whether it was refused: an
// unknown tool is an error of the call, a refused one a tool error.
func (e *poE2E) call(c *client.Client, name string, args map[string]any) (text string, refused bool) {
	e.t.Helper()
	var req mcplib.CallToolRequest
	req.Params.Name, req.Params.Arguments = name, args
	res, err := c.CallTool(e.ctx, req)
	if err != nil {
		return err.Error(), true
	}
	for _, c := range res.Content {
		if tc, ok := c.(mcplib.TextContent); ok {
			text += tc.Text
		}
	}
	return text, res.IsError
}

func TestAProductOwnerSessionIsServedItsReadToolsAndNoWriteStartOrMergeTool(t *testing.T) {
	e := newPOE2E(t)
	id, _ := e.startThread("hello")
	e.waitTurn(id, "idle")
	owner := e.dialSession(e.thread(id).Thread.SessionID)

	want := append([]string(nil), tooling.ProductOwnerToolNames...)
	slices.Sort(want)
	got := e.toolNames(owner)
	if !slices.Equal(got, want) {
		t.Fatalf("tools = %v, want exactly %v", got, want)
	}
	for _, banned := range []string{"start_task_session", "create_task", "update_task_status", "merge_session_branch", "push_branch",
		"publish_artifact", "attach_artifact_to_task", "message_architect", "reply_to_session", "request_user_review", "notify",
		"propose_instruction_files", "set_status_check"} {
		if slices.Contains(got, banned) {
			t.Errorf("the product owner is served %s", banned)
		}
		// A call is refused whatever the prompt says.
		if text, refused := e.call(owner, banned, map[string]any{"title": "x", "prompt": "x", "status": "done"}); !refused {
			t.Errorf("a call to %s was answered: %s", banned, text)
		}
	}
	if tasks, _ := e.plan.ListTickets(e.ctx, e.project, ports.TicketFilter{}); len(tasks) != 0 {
		t.Error("a refused create_task made a task")
	}

	// The brief names the tools it has.
	if l := e.launches(); len(l) != 1 {
		t.Fatalf("launches = %q", l)
	} else {
		for _, name := range tooling.ProductOwnerToolNames {
			if !strings.Contains(l[0], name) {
				t.Errorf("the opening brief does not name the tool %s", name)
			}
		}
	}

	// Every other session keeps its tools: the line is the mode's.
	if _, err := e.sess.Create(&domain.Session{ID: "plain", ProjectID: e.project, Task: "a task session", Status: domain.SessionRunning, Mode: domain.SessionModeImplement}); err != nil {
		t.Fatal(err)
	}
	if general := e.toolNames(e.dialSession("plain")); !slices.Contains(general, "start_task_session") || len(general) <= len(want) {
		t.Errorf("a task session is served %v", general)
	} else {
		// No other mode is served the product owner's own reads.
		for _, own := range []string{"get_project_task", "list_project_sessions", "list_project_branches"} {
			if slices.Contains(general, own) {
				t.Errorf("a task session is served %s", own)
			}
		}
	}
}

func TestTheProductOwnerReadsTasksWithTheirHistoryAndOnlyTheStatusOutcomeAndSummaryOfOtherSessions(t *testing.T) {
	e := newPOE2E(t)
	feature, err := e.plan.CreateTicket(e.ctx, e.project, "Product owner", "talk to your project", domain.TicketStatusInProgress, "")
	if err != nil {
		t.Fatal(err)
	}
	child, err := e.plan.CreateTicket(e.ctx, e.project, "Mode and settings", "", domain.TicketStatusReview, feature.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.plan.StatusHistory.Append(&domain.TaskStatusChange{TaskID: child.ID, Status: domain.TicketStatusReview, Reason: "ready for review", By: domain.ChangedByArchitect, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	merged := &domain.MergeResult{At: time.Now(), Merged: true, Into: "main", Method: "merge"}
	if _, err := e.sess.Create(&domain.Session{ID: "other", ProjectID: e.project, TicketID: child.ID, Task: "implement the mode", WorkingDir: "/secret/worktrees/other",
		WorkspaceID: "ws-secret", Branch: "agent/mode-1", Status: domain.SessionDone, Mode: domain.SessionModeImplement}); err != nil {
		t.Fatal(err)
	}
	if err := e.sess.UpdateMerge("other", *merged); err != nil {
		t.Fatal(err)
	}

	id, _ := e.startThread("what is in progress?")
	owner := e.dialSession(e.thread(id).Thread.SessionID)

	text, refused := e.call(owner, "get_project_task", map[string]any{"task_id": feature.ID})
	if refused || !strings.Contains(text, "Mode and settings") || !strings.Contains(text, `"children"`) {
		t.Errorf("get_project_task(feature) = %s (refused %v), want its children", text, refused)
	}
	text, refused = e.call(owner, "get_project_task", map[string]any{"task_id": child.ID})
	if refused || !strings.Contains(text, "ready for review") || !strings.Contains(text, "architect") {
		t.Errorf("get_project_task(child) = %s (refused %v), want its status history", text, refused)
	}
	if text, refused := e.call(owner, "get_project_task", map[string]any{"task_id": "nope"}); !refused || !strings.Contains(text, "TASK_NOT_FOUND") {
		t.Errorf("an unknown task = %s", text)
	}

	text, _ = e.call(owner, "list_project_sessions", map[string]any{"include_ended": true})
	for _, want := range []string{"implement the mode", `"done"`, `"merge"`, "other"} {
		if !strings.Contains(text, want) {
			t.Errorf("list_project_sessions lacks %q: %s", want, text)
		}
	}
	for _, secret := range []string{"/secret", "ws-secret", "working_dir", "workspace", "transcript", "worktree", "last_action"} {
		if strings.Contains(text, secret) {
			t.Errorf("list_project_sessions shows %q: %s", secret, text)
		}
	}
	if strings.Contains(text, e.thread(id).Thread.SessionID) {
		t.Error("the product owner's own thread is listed among the sessions")
	}
	text, _ = e.call(owner, "list_project_branches", nil)
	if !strings.Contains(text, "agent/mode-1") || !strings.Contains(text, `"merged":true`) || strings.Contains(text, "/secret") {
		t.Errorf("list_project_branches = %s", text)
	}
}

func TestTheProductOwnersSandboxReachesNoOtherSessionsWorktreeAndItsShellRefusesEveryWrite(t *testing.T) {
	e := newPOE2E(t)
	id, _ := e.startThread("hello")
	th := e.waitTurn(id, "idle")

	calls := e.sandbox.Calls()
	policy := calls[len(calls)-1].Policy
	worktrees := filepath.Join(filepath.Dir(e.cfg.DBPath), "worktrees")
	for _, p := range append(append([]string(nil), policy.Readable...), policy.Writable...) {
		if strings.HasPrefix(p, worktrees) {
			t.Errorf("the sandbox grants %s, a path of the session worktrees", p)
		}
	}

	dir, ok := e.shells.RootDir(th.Thread.SessionID)
	if !ok {
		t.Fatal("the product owner has no virtual shell")
	}
	for _, edit := range []ports.ShellEdit{
		{Op: "write", Paths: []string{filepath.Join(dir, "project/docs/new.md")}, Tool: "Write"},
		{Op: "rm", Paths: []string{filepath.Join(dir, "project/docs/prd.md")}},
		{Op: "mkdir", Paths: []string{filepath.Join(dir, "tasks/x")}},
	} {
		if ans := e.shells.CheckShellEdit(e.ctx, th.Thread.SessionID, edit); ans.Allow || ans.Code != "PRODUCT_OWNER_READ_ONLY" {
			t.Errorf("check %+v = %+v, want PRODUCT_OWNER_READ_ONLY", edit, ans)
		}
		if ans := e.shells.CommitShellEdit(e.ctx, th.Thread.SessionID, edit); ans.Allow || ans.Code != "PRODUCT_OWNER_READ_ONLY" {
			t.Errorf("commit %+v = %+v, want PRODUCT_OWNER_READ_ONLY", edit, ans)
		}
	}
	// What is outside its shell is no concern of the proxy.
	if ans := e.shells.CheckShellEdit(e.ctx, th.Thread.SessionID, ports.ShellEdit{Op: "write", Paths: []string{"/tmp/elsewhere"}}); !ans.Allow {
		t.Errorf("a path outside the shell = %+v", ans)
	}
}

func TestAThreadsSessionIsInNoSessionListTheBoardTheFeedOrTheInbox(t *testing.T) {
	e := newPOE2E(t)
	// Another session, to show the surfaces still list what they list.
	if _, err := e.sess.Create(&domain.Session{ID: "task-session", ProjectID: e.project, Task: "a task session", Status: domain.SessionRunning, Mode: domain.SessionModeImplement}); err != nil {
		t.Fatal(err)
	}
	id, _ := e.startThread("hello")
	th := e.waitTurn(id, "idle")
	sid := th.Thread.SessionID

	var list struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	if status, code := e.get("sessions?project_id="+e.project, &list); status != 200 {
		t.Fatalf("list sessions = %d %s", status, code)
	}
	var ids []string
	for _, s := range list.Sessions {
		ids = append(ids, s.ID)
	}
	if slices.Contains(ids, sid) || !slices.Contains(ids, "task-session") {
		t.Errorf("the project's sessions = %v, want the task session and not the thread's %s", ids, sid)
	}
	if live := e.sess.List(e.ctx, ports.SessionFilter{ProjectID: e.project, Statuses: domain.LiveSessionStatuses}); len(live) != 1 || live[0].ID != "task-session" {
		t.Errorf("live sessions = %+v", live)
	}
	if act, _ := e.sess.ActivityByProject(); act[e.project].LiveCount != 1 {
		t.Errorf("the project counts %d live sessions, want only the task session", act[e.project].LiveCount)
	}

	res, err := http.Get(e.srv.URL + "/api/agent_board")
	if err != nil {
		t.Fatal(err)
	}
	board, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Contains(string(board), sid) {
		t.Errorf("the agent board shows the thread's session")
	}
	for _, c := range e.feed.changesOf("session") {
		if strings.Contains(c, sid) {
			t.Errorf("the project feed announced the thread's session: %s", c)
		}
	}
	notes, err := e.notes.ListNotifications(e.ctx, ports.NotificationQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(notes); strings.Contains(string(b), sid) {
		t.Errorf("the inbox mentions the thread's session: %s", b)
	}
}

func TestAThreadsCallsAreAttributedToTheProductOwnerAndItsProject(t *testing.T) {
	e := newPOE2E(t)
	id, _ := e.startThread("hello")
	e.waitTurn(id, "idle")

	b, _ := os.ReadFile(filepath.Join(e.fakeDir, "headers"))
	want := "harness:product-owner:" + e.project
	if !strings.Contains(string(b), want) {
		t.Fatalf("claude was given the headers %q, want the attribution %s", b, want)
	}
	// The proxy records each call under that attribution.
	now := time.Now()
	e.usage.Record(e.ctx, ports.UsageRequest{ID: "call-1", Attribution: want, Method: "POST", Path: "/v1/messages", Model: "sonnet", Status: 200, StartedAt: now, EndedAt: now, InputTokens: 10, OutputTokens: 5})
	recs, err := e.usage.ListRecentUsage(e.ctx, ports.UsageQuery{ProjectID: e.project})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs.Records) != 1 || recs.Records[0].ProjectID != e.project || recs.Records[0].Attribution != "harness:product-owner" || recs.Records[0].SessionID != "" {
		t.Errorf("records = %+v, want one for the product owner in the project", recs.Records)
	}
	sum, err := e.usage.UsageSummary(e.ctx, e.project, ports.UsageRange("30d"))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Totals.Requests != 1 {
		t.Errorf("the project's usage counts %d requests, want the product owner's one", sum.Totals.Requests)
	}
}

func TestTheThreadListCarriesTheLastMessageExcerptAndWhetherThePersonNamedIt(t *testing.T) {
	e := newPOE2E(t)
	id, _ := e.startThread("What was done this week?")
	e.waitTurn(id, "idle")

	type row struct {
		ThreadID         string `json:"thread_id"`
		Title            string `json:"title"`
		Excerpt          string `json:"last_message_excerpt"`
		TitleSetByPerson *bool  `json:"title_set_by_person"`
	}
	read := func() row {
		var out struct {
			Threads []row `json:"threads"`
		}
		e.get("product_owner_threads?project_id="+e.project, &out)
		if len(out.Threads) != 1 {
			t.Fatalf("threads = %+v", out.Threads)
		}
		return out.Threads[0]
	}
	got := read()
	if got.Excerpt != "answer to: What was done this week?" || got.TitleSetByPerson == nil || *got.TitleSetByPerson {
		t.Errorf("row = %+v, want the answer's first line as the excerpt and the title not set by the person", got)
	}
	long := strings.Repeat("y", 200) + "\nsecond line"
	if status, code := e.send(id, long); status != 200 {
		t.Fatalf("send = %d %s", status, code)
	}
	e.waitTurn(id, "idle")
	if ex := read().Excerpt; len([]rune(ex)) > 160 || !strings.HasPrefix(ex, "answer to: yyy") {
		t.Errorf("excerpt = %q, want one line of at most 160 characters", ex)
	}
}

func TestThePersonRenamesAThreadAndTheNewTitleIsKeptAndAnnounced(t *testing.T) {
	e := newPOE2E(t)
	id, _ := e.startThread("first words")
	e.waitTurn(id, "idle")

	var renamed struct {
		ThreadID         string `json:"thread_id"`
		Title            string `json:"title"`
		TitleSetByPerson bool   `json:"title_set_by_person"`
	}
	if status, code := e.post("rename_product_owner_thread", map[string]any{"thread_id": id, "title": "  Weekly review  "}, &renamed); status != 200 {
		t.Fatalf("rename = %d %s", status, code)
	}
	if renamed.ThreadID != id || renamed.Title != "Weekly review" || !renamed.TitleSetByPerson {
		t.Errorf("answer = %+v", renamed)
	}
	for name, title := range map[string]string{"empty": "   ", "too long": strings.Repeat("x", 201)} {
		if status, code := e.post("rename_product_owner_thread", map[string]any{"thread_id": id, "title": title}, nil); status != 400 || code != "INVALID_INPUT" {
			t.Errorf("%s title = %d %s, want 400 INVALID_INPUT", name, status, code)
		}
	}
	if status, code := e.post("rename_product_owner_thread", map[string]any{"thread_id": "nope", "title": "x"}, nil); status != 404 || code != "THREAD_NOT_FOUND" {
		t.Errorf("unknown thread = %d %s", status, code)
	}

	eventuallyPO(t, "the rename on the feed", func() string {
		for _, c := range e.feed.of("product_owner_thread") {
			if th, _ := c["thread"].(map[string]any); th["title"] == "Weekly review" && th["title_set_by_person"] == true {
				return ""
			}
		}
		return "no product_owner_thread event with the new title"
	})

	// Later turns do not replace it, and the restart keeps it.
	e.send(id, "more")
	e.waitTurn(id, "idle")
	e.restart()
	if got := e.thread(id).Thread.Title; got != "Weekly review" {
		t.Errorf("title = %q after more turns and a restart", got)
	}
}

func TestAResumeThatFailsKeepsThePersonsMessageAndLeavesAFailureItemToRetryFrom(t *testing.T) {
	e := newPOE2E(t)
	e.po.SetIdle(200 * time.Millisecond)
	id, _ := e.startThread("first")
	th := e.waitTurn(id, "idle")
	eventuallyPO(t, "the idle process to end", func() string {
		if e.orch.ProductOwnerRunning(th.Thread.SessionID) {
			return "still running"
		}
		return ""
	})

	// claude can no longer be started: the next message cannot wake the session.
	e.cfg.ClaudeBin = "claude-is-not-installed-here"
	e.restart()
	time.Sleep(200 * time.Millisecond)
	var kept struct {
		Code string `json:"code"`
	}
	b, _ := json.Marshal(map[string]any{"thread_id": id, "message": "second"})
	res, err := http.Post(e.srv.URL+"/api/send_product_owner_message", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewDecoder(res.Body).Decode(&kept)
	res.Body.Close()
	if res.StatusCode != 503 || kept.Code != "PRODUCT_OWNER_UNAVAILABLE" {
		t.Fatalf("send = %d %s, want 503 PRODUCT_OWNER_UNAVAILABLE", res.StatusCode, kept.Code)
	}

	got := e.thread(id)
	if got.Thread.Turn != "idle" {
		t.Errorf("turn = %s, want idle", got.Thread.Turn)
	}
	var second, failure *poItem
	for i, it := range got.Items {
		if it.Kind == "message" && it.Text == "second" {
			second = &got.Items[i]
		}
		if it.Kind == "failure" {
			failure = &got.Items[i]
		}
	}
	if second == nil || failure == nil {
		t.Fatalf("items = %+v, want the person's message and a failure item", got.Items)
	}
	raw, _ := json.Marshal(failure)
	_ = raw
	var generic struct {
		Items []map[string]any `json:"items"`
	}
	e.get("product_owner_thread?thread_id="+id, &generic)
	for _, it := range generic.Items {
		if it["kind"] == "failure" && (it["code"] != "SESSION_RESUME_FAILED" || it["retry_item_id"] != second.ItemID || it["text"] == "") {
			t.Errorf("failure = %+v, want code SESSION_RESUME_FAILED, a text and retry_item_id %s", it, second.ItemID)
		}
	}

	// Once claude can run again, sending the same text again works.
	e.cfg.ClaudeBin = filepath.Join(e.fakeDir, "claude")
	e.restart()
	if status, code := e.send(id, "second"); status != 200 {
		t.Fatalf("retry = %d %s", status, code)
	}
	e.waitTurn(id, "idle")
}

func TestAThreadReadsResumingWhileItsSessionWakesBeforeTheTurnRuns(t *testing.T) {
	e := newPOE2E(t)
	e.po.SetIdle(200 * time.Millisecond)
	id, _ := e.startThread("first")
	th := e.waitTurn(id, "idle")
	eventuallyPO(t, "the idle process to end", func() string {
		if e.orch.ProductOwnerRunning(th.Thread.SessionID) {
			return "still running"
		}
		return ""
	})
	if status, code := e.send(id, "second"); status != 200 {
		t.Fatalf("send = %d %s", status, code)
	}
	e.waitTurn(id, "idle")
	var turns []string
	for _, c := range e.feed.of("product_owner_thread") {
		if th, _ := c["thread"].(map[string]any); th["thread_id"] == id {
			turns = append(turns, th["turn"].(string))
		}
	}
	got := strings.Join(turns, ",")
	if !strings.Contains(got, "idle,resuming,running,idle") {
		t.Errorf("turns = %s, want the second turn to go resuming, running, idle", got)
	}
}

func TestASecondMessageDuringAWakeUpIsRefusedAtOnceNotAfterTheWakeUp(t *testing.T) {
	e := newPOE2E(t)
	e.po.SetIdle(200 * time.Millisecond)
	id, _ := e.startThread("first")
	th := e.waitTurn(id, "idle")
	eventuallyPO(t, "the idle process to end", func() string {
		if e.orch.ProductOwnerRunning(th.Thread.SessionID) {
			return "still running"
		}
		return ""
	})

	// The wake-up is held for good until released: only a refusal that does not
	// wait for it can come back while it lasts.
	e.gateStart()
	e.slow(true)
	woken := make(chan int, 1)
	go func() {
		status, _ := e.send(id, "second")
		woken <- status
	}()
	eventuallyPO(t, "the thread to read resuming", func() string {
		if got := e.thread(id).Thread.Turn; got != "resuming" {
			return "turn is " + got
		}
		return ""
	})
	began := time.Now()
	refused := make(chan [2]any, 1)
	go func() {
		status, code := e.send(id, "third")
		refused <- [2]any{status, code}
	}()
	select {
	case r := <-refused:
		if r[0] != 409 || r[1] != "THREAD_BUSY" {
			t.Errorf("a message during the wake-up = %v, want 409 THREAD_BUSY", r)
		}
		if took := time.Since(began); took > time.Second {
			t.Errorf("the refusal took %s", took)
		}
	case <-time.After(2 * time.Second):
		t.Error("the message was not refused within 2s: it waits on the wake-up")
		e.slow(false)
		<-refused
	}
	select {
	case <-woken:
		t.Error("the wake-up ended while it was held")
	default:
	}
	e.slow(false)
	if status := <-woken; status != 200 {
		t.Errorf("the message that woke the thread = %d", status)
	}
	e.waitTurn(id, "idle")
}

func TestAStopBeforeTheFirstWordIsAnsweredByAStoppedMessageAndNoBootResumesIt(t *testing.T) {
	e := newPOE2E(t)
	prehold := filepath.Join(e.fakeDir, "prehold")
	if err := os.WriteFile(prehold, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	id, _ := e.startThread("stopped before any word")
	eventuallyPO(t, "the process to hold the message", func() string {
		if b, _ := os.ReadFile(filepath.Join(e.fakeDir, "received")); !strings.Contains(string(b), "stopped before any word") {
			return "not received yet"
		}
		return ""
	})
	time.AfterFunc(200*time.Millisecond, func() { _ = os.Remove(prehold) })
	if status, code := e.post("stop_product_owner_turn", map[string]any{"thread_id": id}, nil); status != 200 {
		t.Fatalf("stop = %d %s", status, code)
	}
	got := e.waitTurn(id, "idle")
	last := got.Items[len(got.Items)-1]
	if last.Kind != "message" || last.Author != "product_owner" || !last.Stopped || last.Text != "" {
		t.Fatalf("last item = %+v, want an empty product-owner message with stopped set", last)
	}

	e.restart()
	time.Sleep(500 * time.Millisecond)
	after := e.thread(id)
	if after.Thread.Turn != "idle" || len(after.Items) != len(got.Items) || len(e.launches()) != 1 {
		t.Errorf("a boot answered a stopped turn: turn %s, %d items (was %d), %d launches", after.Thread.Turn, len(after.Items), len(got.Items), len(e.launches()))
	}
}

func TestALiveSendDuringBootRecoveryIsResumedOnceWithNoFailureAndARecoveredThreadIsBusy(t *testing.T) {
	e := newPOE2E(t)
	e.po.SetIdle(150 * time.Millisecond)
	a, _ := e.startThread("thread a")
	b, _ := e.startThread("thread b")
	e.waitTurn(a, "idle")
	thB := e.waitTurn(b, "idle")
	eventuallyPO(t, "both processes to end", func() string {
		if e.orch.ProductOwnerRunning(thB.Thread.SessionID) || e.orch.ProductOwnerRunning(e.thread(a).Thread.SessionID) {
			return "still running"
		}
		return ""
	})
	// Thread a was cut off with its message stored and never answered.
	if _, err := e.store.AppendItem(e.ctx, domain.ProductOwnerItem{ThreadID: a, Kind: domain.ProductOwnerItemMessage, At: time.Now(),
		Author: domain.ProductOwnerAuthorPerson, Text: "unanswered at the crash"}); err != nil {
		t.Fatal(err)
	}

	e.slow(true) // the boot's resume of thread a is held at its start
	launched := len(e.launches())
	e.restart()
	// Thread a is decided at boot, before any request: it is busy while recovered.
	if got := e.thread(a).Thread.Turn; got != "interrupted" {
		t.Fatalf("thread a reads %s, want interrupted while its recovery is held", got)
	}
	if status, code := e.send(a, "live to a"); status != 409 || code != "THREAD_BUSY" {
		t.Errorf("a send to the thread being recovered = %d %s, want 409 THREAD_BUSY", status, code)
	}
	// Thread b was not cut off: a live send is its own resume, once, and the
	// recovery never touches it.
	sent := make(chan int, 1)
	go func() { status, _ := e.send(b, "live to b"); sent <- status }()
	time.Sleep(300 * time.Millisecond)
	e.slow(false)
	if status := <-sent; status != 200 {
		t.Fatalf("the live send to b = %d", status)
	}
	e.waitTurn(a, "idle")
	e.waitTurn(b, "idle")
	for _, id := range []string{a, b} {
		if n := countItems(e.thread(id).Items, "failure", ""); n != 0 {
			t.Errorf("thread %s has %d failure items", id, n)
		}
	}
	if n := countItems(e.thread(b).Items, "notice", ""); n != 0 {
		t.Errorf("thread b was recovered too: %d notices", n)
	}
	if n := countItems(e.thread(a).Items, "notice", "Continued after a restart"); n != 1 {
		t.Errorf("thread a has %d notices, want 1", n)
	}
	if got := len(e.launches()) - launched; got != 2 {
		t.Errorf("%d launches since the restart, want one resume for each thread", got)
	}
}

func (e *poE2E) lastItem(id string) poItem {
	items := e.thread(id).Items
	return items[len(items)-1]
}

func (e *poE2E) assertStoppedLast(id string) {
	e.t.Helper()
	if last := e.lastItem(id); last.Kind != "message" || last.Author != "product_owner" || !last.Stopped || last.Text != "" {
		e.t.Errorf("thread %s: last item = %+v, want the empty stopped product-owner message", id, last)
	}
}

func TestAStopDuringAWakeUpIsAnsweredByAStoppedMessageAndNoBootResumesIt(t *testing.T) {
	e := newPOE2E(t)
	e.po.SetIdle(150 * time.Millisecond)
	id, _ := e.startThread("first")
	th := e.waitTurn(id, "idle")
	eventuallyPO(t, "the idle process to end", func() string {
		if e.orch.ProductOwnerRunning(th.Thread.SessionID) {
			return "still running"
		}
		return ""
	})

	prehold := filepath.Join(e.fakeDir, "prehold")
	if err := os.WriteFile(prehold, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	e.slow(true)
	woken := make(chan int, 1)
	go func() { status, _ := e.send(id, "woken then stopped"); woken <- status }()
	eventuallyPO(t, "the thread to read resuming", func() string {
		if e.thread(id).Thread.Turn != "resuming" {
			return "not resuming"
		}
		return ""
	})
	stopped := make(chan int, 1)
	go func() {
		status, _ := e.post("stop_product_owner_turn", map[string]any{"thread_id": id}, nil)
		stopped <- status
	}()
	time.Sleep(300 * time.Millisecond)
	e.slow(false)
	time.AfterFunc(300*time.Millisecond, func() { _ = os.Remove(prehold) })
	if status := <-woken; status != 200 {
		t.Fatalf("the send = %d", status)
	}
	if status := <-stopped; status != 200 {
		t.Fatalf("the stop = %d", status)
	}
	e.waitTurn(id, "idle")
	e.assertStoppedLast(id)

	launched := len(e.launches())
	e.restart()
	time.Sleep(500 * time.Millisecond)
	if after := e.thread(id); after.Thread.Turn != "idle" || len(e.launches()) != launched || countItems(after.Items, "notice", "") != 0 {
		t.Errorf("a boot resumed a stopped turn: turn %s, %d launches (was %d)", after.Thread.Turn, len(e.launches()), launched)
	}
	e.assertStoppedLast(id)
}

func TestAStopOnAThreadQueuedForRecoveryIsAnsweredAndTheRecoverySkipsIt(t *testing.T) {
	e := newPOE2E(t)
	e.po.SetIdle(150 * time.Millisecond)
	ids := []string{}
	for _, title := range []string{"queued a", "queued b"} {
		id, _ := e.startThread(title)
		e.waitTurn(id, "idle")
		ids = append(ids, id)
	}
	eventuallyPO(t, "both processes to end", func() string {
		for _, id := range ids {
			if e.orch.ProductOwnerRunning(e.thread(id).Thread.SessionID) {
				return "still running"
			}
		}
		return ""
	})
	for _, id := range ids {
		if _, err := e.store.AppendItem(e.ctx, domain.ProductOwnerItem{ThreadID: id, Kind: domain.ProductOwnerItemMessage, At: time.Now(),
			Author: domain.ProductOwnerAuthorPerson, Text: "unanswered at the crash"}); err != nil {
			t.Fatal(err)
		}
	}
	prehold := filepath.Join(e.fakeDir, "prehold")
	if err := os.WriteFile(prehold, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	e.slow(true) // the first recovery is held at its start; the second waits behind it
	e.restart()
	for _, id := range ids {
		if got := e.thread(id).Thread.Turn; got != "interrupted" {
			t.Fatalf("thread %s reads %s, want interrupted", id, got)
		}
	}
	done := make(chan int, 2)
	for _, id := range ids {
		go func() {
			status, _ := e.post("stop_product_owner_turn", map[string]any{"thread_id": id}, nil)
			done <- status
		}()
	}
	time.Sleep(400 * time.Millisecond)
	e.slow(false)
	time.AfterFunc(300*time.Millisecond, func() { _ = os.Remove(prehold) })
	for range ids {
		if status := <-done; status != 200 {
			t.Fatalf("a stop = %d", status)
		}
	}
	for _, id := range ids {
		e.waitTurn(id, "idle")
		e.assertStoppedLast(id)
	}

	launched := len(e.launches())
	e.restart()
	time.Sleep(500 * time.Millisecond)
	for _, id := range ids {
		if e.thread(id).Thread.Turn != "idle" {
			t.Errorf("thread %s was not left idle by the boot", id)
		}
		e.assertStoppedLast(id)
	}
	if len(e.launches()) != launched {
		t.Errorf("a boot resumed a stopped turn: %d launches, was %d", len(e.launches()), launched)
	}
}

func TestAThreadsSessionHasAContextEntryAtItsStartAndANewOneAtEachResume(t *testing.T) {
	e := newPOE2E(t)
	if _, err := e.cat.Projects.SetProjectMCPServers(e.ctx, e.project, []string{"services"}); err != nil {
		t.Fatal(err)
	}
	e.po.SetIdle(150 * time.Millisecond)
	id, _ := e.startThread("first")
	th := e.waitTurn(id, "idle")
	sid := th.Thread.SessionID

	view, err := e.orch.SessionContext(e.ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) != 1 || view.Entries[0].Kind != "start" {
		t.Fatalf("after the start: %d entries %+v, want the start entry", len(view.Entries), view.Entries)
	}
	eventuallyPO(t, "the idle process to end", func() string {
		if e.orch.ProductOwnerRunning(sid) {
			return "still running"
		}
		return ""
	})
	if status, code := e.send(id, "second"); status != 200 {
		t.Fatalf("send = %d %s", status, code)
	}
	e.waitTurn(id, "idle")

	view, err = e.orch.SessionContext(e.ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Entries) != 2 || view.Entries[0].Kind != "start" || view.Entries[1].Kind != "resume" {
		t.Fatalf("after the resume: entries %+v, want start then resume", view.Entries)
	}
	for _, entry := range view.Entries {
		if entry.Facts.Mode != "productowner" || entry.Facts.Model != "sonnet" || !entry.Facts.Headless || entry.Facts.RepositoryVisibility == "" {
			t.Errorf("%s facts = %+v", entry.Kind, entry.Facts)
		}
		var brief bool
		for _, p := range entry.Instructions {
			if p.Title == "Product owner brief" && strings.Contains(p.Text, "cannot change anything yet") {
				brief = true
			}
		}
		if !brief {
			t.Errorf("%s entry has no product owner brief piece: %+v", entry.Kind, entry.Instructions)
		}
		var names []string
		for _, tool := range entry.Tools {
			names = append(names, tool.Name)
			if strings.Contains(tool.Name, "services") || strings.Contains(tool.Server, "services") {
				t.Errorf("%s entry lists a services tool: %+v", entry.Kind, tool)
			}
		}
		for _, want := range tooling.ProductOwnerToolNames {
			if !slices.Contains(names, "mcp__task__"+want) {
				t.Errorf("%s entry lacks the tool %s: %v", entry.Kind, want, names)
			}
		}
		for _, server := range entry.MCPServers {
			if server.Name == "services" {
				t.Errorf("%s entry lists the services server", entry.Kind)
			}
		}
	}
	// What the entries hold is names and texts, never an environment's values.
	raw, _ := json.Marshal(view)
	for _, secret := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "sk-ant-"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the context holds %q", secret)
		}
	}
}
