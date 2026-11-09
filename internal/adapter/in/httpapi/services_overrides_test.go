package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/application/services"
)

// Run overrides (ADR 0077): an agent starts or restarts a service with `env`
// for that run only. The tests run a real `sh -c` that prints the variables it
// received, and call the tools as a session over MCP.

const (
	secretDotenv  = "dotenv-secret-value"
	secretService = "service-secret-value"
	secretBoth    = "hidden-service-value"
)

// overridesFixture is a repository with a root .env (A, B, C), a service
// that exports it and sets B and S, and a session with its own worktree.
type overridesFixture struct {
	*mcpFixture
	id string
}

func newOverridesFixture(t *testing.T) *overridesFixture {
	t.Helper()
	f := newMCPFixture(t, 5*time.Second)
	if _, err := f.cat.Projects.SaveEnvFile(context.Background(), f.repoID, ".env", "A=a-dotenv\nB=b-dotenv\nC="+secretDotenv+"\n"); err != nil {
		t.Fatal(err)
	}
	f.session("sess-1", f.projectID, f.repoID, true)
	in := commandInput("web")
	in["repository_id"], in["export_repository_env"] = f.repoID, true
	in["env"] = "B=b-service\nS=" + secretService + "\nT=" + secretBoth
	in["command"] = map[string]any{"line": `echo "got A=$A B=$B C=$C D=$D S=$S"; sleep 30`}
	return &overridesFixture{mcpFixture: f, id: f.createID(in)}
}

func viewRunOf(out map[string]any) map[string]any {
	return out["service"].(map[string]any)["run"].(map[string]any)
}

func TestServicesOverrides_ACommandRunReceivesThemAndTheyBeatTheServiceAndTheDotenv(t *testing.T) {
	f := newOverridesFixture(t)
	agent := f.as("sess-1")

	out := agent.ok("start_service", map[string]any{"name": "web", "env": map[string]any{"B": "b-override", "D": "d-override"}})
	run := viewRunOf(out)
	if out["already_running"] != false || run["overrides"].(map[string]any)["B"] != "b-override" || run["overrides"].(map[string]any)["D"] != "d-override" ||
		run["override_by"] != "session sess-1 (dev)" {
		t.Fatalf("run = %v", run)
	}
	f.awaitLog(f.id, "got A=a-dotenv B=b-override C="+secretDotenv+" D=d-override S="+secretService+"\n")

	// A second start with other variables changes nothing.
	again := agent.ok("start_service", map[string]any{"name": "web", "env": map[string]any{"B": "other"}})
	if again["already_running"] != true || viewRunOf(again)["overrides"].(map[string]any)["B"] != "b-override" {
		t.Fatalf("a start on a running service changes nothing: %v", again)
	}
}

func TestServicesOverrides_ServiceViewHasBranchOverridesAndOverrideByAndEmptyWhenNone(t *testing.T) {
	f := newOverridesFixture(t)
	agent := f.as("sess-1")

	plain := viewRunOf(agent.ok("start_service", map[string]any{"name": "web", "worktree": "own"}))
	ov, ok := plain["overrides"].(map[string]any)
	if !ok || len(ov) != 0 || plain["override_by"] != "" || plain["branch"] != "feat/sess-1" {
		t.Fatalf("no overrides shows {} and \"\", with the branch: %v", plain)
	}
	if _, has := agent.ok("service_status", named("web"))["service"].(map[string]any)["run"].(map[string]any)["overrides"]; !has {
		t.Fatal("service_status holds overrides")
	}

	with := viewRunOf(agent.ok("restart_service", map[string]any{"name": "web", "worktree": "own", "env": map[string]any{"LOG_LEVEL": "debug"}}))
	if with["overrides"].(map[string]any)["LOG_LEVEL"] != "debug" || with["override_by"] != "session sess-1 (dev)" || with["branch"] != "feat/sess-1" {
		t.Fatalf("run = %v", with)
	}
}

func TestServicesOverrides_NoServiceOrDotenvValueIsInAnyAgentAnswer(t *testing.T) {
	f := newOverridesFixture(t)
	agent := f.as("sess-1")
	agent.ok("start_service", map[string]any{"name": "web", "env": map[string]any{"D": "d-override", "T": "t-override"}})
	f.awaitLog(f.id, "got A=a-dotenv")
	agent.ok("list_services", nil)
	agent.ok("service_status", named("web"))
	agent.ok("restart_service", named("web"))
	f.awaitLog(f.id, "got A=a-dotenv")
	agent.ok("stop_service", named("web"))

	if len(f.answers) == 0 {
		t.Fatal("no answers")
	}
	for _, text := range f.answers {
		for _, secret := range []string{secretDotenv, secretService, secretBoth, "b-dotenv"} {
			if strings.Contains(text, secret) {
				t.Fatalf("an answer holds %q: %s", secret, text)
			}
		}
	}
	// Names only.
	if view := agent.ok("service_status", named("web"))["service"].(map[string]any); strings.Join(anyStrings(view["env_names"]), ",") != "B,S,T" {
		t.Fatalf("env_names = %v", view["env_names"])
	}
}

func TestServicesOverrides_ARestartRepeatsReplacesOrClearsThem(t *testing.T) {
	f := newOverridesFixture(t)
	agent := f.as("sess-1")
	agent.ok("start_service", map[string]any{"name": "web", "worktree": "own", "env": map[string]any{"D": "first"}})
	f.awaitLog(f.id, "D=first")

	// No arguments: the same worktree, the same overrides.
	same := viewRunOf(agent.ok("restart_service", named("web")))
	if same["where"] != "your worktree" || same["overrides"].(map[string]any)["D"] != "first" || same["override_by"] != "session sess-1 (dev)" {
		t.Fatalf("a bare restart repeats the run: %v", same)
	}
	f.awaitLog(f.id, "D=first")

	// The web's restart repeats them too, and keeps who set them.
	if code, out := f.operate("restart_service", f.id); code != 200 {
		t.Fatalf("restart = %d %v", code, out)
	}
	web := f.awaitStatus(f.id, "running")["run"].(map[string]any)
	if names := web["overrides"].([]any); len(names) != 1 || names[0].(map[string]any)["name"] != "D" {
		t.Fatalf("the web restart repeats the overrides: %v", web)
	}
	f.awaitLog(f.id, "D=first")

	// A new env replaces them.
	replaced := viewRunOf(agent.ok("restart_service", map[string]any{"name": "web", "worktree": "own", "env": map[string]any{"D": "second"}}))
	if replaced["overrides"].(map[string]any)["D"] != "second" {
		t.Fatalf("replaced = %v", replaced)
	}
	f.awaitLog(f.id, "D=second")

	// env {} clears them.
	cleared := viewRunOf(agent.ok("restart_service", map[string]any{"name": "web", "worktree": "own", "env": map[string]any{}}))
	if len(cleared["overrides"].(map[string]any)) != 0 || cleared["override_by"] != "" {
		t.Fatalf("cleared = %v", cleared)
	}
	if got := f.awaitLog(f.id, "D= S="); strings.Contains(got, "D=second") {
		t.Fatalf("the cleared run has no D: %s", got)
	}
}

func TestServicesOverrides_ThePersonsStartBeginsFromTheDefinitionWithNoOverrides(t *testing.T) {
	f := newOverridesFixture(t)
	agent := f.as("sess-1")
	agent.ok("start_service", map[string]any{"name": "web", "env": map[string]any{"D": "agent-value"}})
	f.awaitLog(f.id, "D=agent-value")
	f.operate("stop_service", f.id)

	if code, out := f.operate("start_service", f.id); code != 200 {
		t.Fatalf("start = %d %v", code, out)
	}
	run := f.awaitStatus(f.id, "running")["run"].(map[string]any)
	if names := run["overrides"].([]any); len(names) != 0 {
		t.Fatalf("a person's start has no overrides: %v", run)
	}
	if got := f.awaitLog(f.id, "got A=a-dotenv"); strings.Contains(got, "agent-value") {
		t.Fatalf("the person's run got an override: %s", got)
	}
}

func TestServicesOverrides_TheRunShapeListsNamesWithByAndNeverValues(t *testing.T) {
	f := newOverridesFixture(t)
	events, err := f.svc.FollowServices(context.Background(), f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	f.as("sess-1").ok("start_service", map[string]any{"name": "web", "env": map[string]any{"LOG_LEVEL": "uniquely-debug-value"}})
	f.awaitLog(f.id, "got A=a-dotenv")

	run := f.service(f.id)["run"].(map[string]any)
	list := run["overrides"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["name"] != "LOG_LEVEL" || list[0].(map[string]any)["by"] != "dev · session sess-1" {
		t.Fatalf("overrides = %v", list)
	}

	// Neither the HTTP answers nor the feed hold a value; nor does the log.
	_, listed := f.call("GET", "/api/list_services?project_id="+f.projectID, nil)
	b, _ := json.Marshal([]any{run, listed})
	for e := range drain(events) {
		eb, _ := json.Marshal(e)
		b = append(b, eb...)
	}
	b = append(b, f.logText(f.id)...)
	if strings.Contains(string(b), "uniquely-debug-value") {
		t.Fatalf("an override value reached the feed, the Run shape or the log: %s", b)
	}
	if !strings.Contains(f.logText(f.id), "LOG_LEVEL") {
		t.Fatalf("the log names the override: %s", f.logText(f.id))
	}
}

// drain yields what the channel holds now.
func drain[T any](ch <-chan T) <-chan T {
	out := make(chan T, 256)
	for {
		select {
		case v, ok := <-ch:
			if !ok {
				close(out)
				return out
			}
			out <- v
		default:
			close(out)
			return out
		}
	}
}

func TestServicesOverrides_BadEntriesAreRefusedNamingTheKeyAndStartNothing(t *testing.T) {
	f := newOverridesFixture(t)
	agent := f.as("sess-1")

	many := map[string]any{}
	for i := 0; i < 101; i++ {
		many["K"+string(rune('A'+i/26))+string(rune('a'+i%26))] = "v"
	}
	cases := []struct {
		name string
		env  map[string]any
		want string // a substring of the refusal
	}{
		{"name", map[string]any{"1BAD": "x"}, `"1BAD"`},
		{"dash", map[string]any{"A-B": "x"}, `"A-B"`},
		{"nul", map[string]any{"OK": "a\x00b"}, `"OK"`},
		{"count", many, "100"},
		{"type", map[string]any{"NUM": 5}, `"NUM"`},
	}
	seen := map[string]string{}
	for _, c := range cases {
		for _, tool := range []string{"start_service", "restart_service"} {
			_, msg := agent.call(tool, map[string]any{"name": "web", "env": c.env})
			if !strings.Contains(msg, c.want) {
				t.Fatalf("%s %s = %q, want it to name %s", tool, c.name, msg, c.want)
			}
			if strings.Contains(msg, "a\x00b") {
				t.Fatalf("a refusal quotes a value: %q", msg)
			}
		}
		_, seen[c.name] = agent.call("start_service", map[string]any{"name": "web", "env": c.env})
	}
	msgs := map[string]bool{}
	for _, m := range seen {
		msgs[m] = true
	}
	if len(msgs) != len(cases) {
		t.Fatalf("the refusals are not distinct: %v", seen)
	}
	if st := f.status(f.id); st != "stopped" {
		t.Fatalf("a refused start starts nothing: %s", st)
	}
}

func TestServicesOverrides_AgentsAreStillRefusedEveryWorktreeButOwn(t *testing.T) {
	f := newOverridesFixture(t)
	f.as("sess-1").refused("start_service", map[string]any{"name": "web", "worktree": "session:other", "env": map[string]any{"A": "b"}}, `worktree must be "own" or omitted`)
}

func TestServicesOverrides_ARestartRepeatsEachOmittedArgumentOnItsOwn(t *testing.T) {
	f := newOverridesFixture(t)
	f.session("sess-2", f.projectID, f.repoID, true)
	agent := f.as("sess-1")
	agent.ok("start_service", map[string]any{"name": "web", "worktree": "own", "env": map[string]any{"D": "first"}})
	f.awaitLog(f.id, "D=first")

	// env alone changes the variables in place, on the same branch.
	run := viewRunOf(agent.ok("restart_service", map[string]any{"name": "web", "env": map[string]any{"D": "second"}}))
	if run["where"] != "your worktree" || run["branch"] != "feat/sess-1" || run["overrides"].(map[string]any)["D"] != "second" {
		t.Fatalf("restart {env} stays in the worktree: %v", run)
	}
	f.awaitLog(f.id, "D=second")

	// worktree alone keeps the overrides.
	run = viewRunOf(agent.ok("restart_service", map[string]any{"name": "web", "worktree": "own"}))
	if run["where"] != "your worktree" || run["overrides"].(map[string]any)["D"] != "second" {
		t.Fatalf("restart {worktree} keeps the overrides: %v", run)
	}

	// The person moves the run to another session's worktree: the agent may
	// not use it, so its bare restart goes to the main checkout and keeps the
	// overrides the run had (none, as the person started it).
	f.operate("stop_service", f.id)
	if code, out := f.call("POST", "/api/start_service", map[string]any{"service_id": f.id, "worktree": "session:sess-2"}); code != 200 {
		t.Fatalf("start = %d %v", code, out)
	}
	f.awaitStatus(f.id, "running")
	run = viewRunOf(agent.ok("restart_service", named("web")))
	if run["where"] != "main checkout" {
		t.Fatalf("a worktree the agent may not use falls back to main: %v", run)
	}
}

func TestServicesOverrides_TheValuesSurviveAServerRestartAndGoWithTheService(t *testing.T) {
	f := newOverridesFixture(t)
	f.as("sess-1").ok("start_service", map[string]any{"name": "web", "env": map[string]any{"D": "agent-value"}})
	f.awaitLog(f.id, "D=agent-value")
	f.operate("stop_service", f.id)

	// A new module over the same store, as after a restart of the server.
	g := restarted(t, f.servicesFixture, services.Options{})
	if got := g.variables(f.id); len(got) == 0 {
		t.Fatal("the last run's variables are known after a restart")
	}
	var d map[string]any
	for _, v := range g.variables(f.id) {
		if v["name"] == "D" {
			d = v
		}
	}
	if d == nil || d["value"] != "agent-value" || d["source"] != "override" || d["by"] != "dev · session sess-1" {
		t.Fatalf("D = %v", d)
	}
	if code, out := g.operate("restart_service", f.id); code != 200 {
		t.Fatalf("restart = %d %v", code, out)
	}
	t.Cleanup(func() { g.operate("stop_service", f.id) })
	g.awaitLog(f.id, "D=agent-value")

	// The record is not in the service's JSON, and goes with the service.
	if rec, _ := f.store.GetRunRecord(f.id); rec == nil {
		t.Fatal("a record is stored")
	}
	g.operate("stop_service", f.id)
	if code, out := g.call("POST", "/api/delete_service", map[string]any{"service_id": f.id}); code != 200 {
		t.Fatalf("delete = %d %v", code, out)
	}
	if rec, _ := f.store.GetRunRecord(f.id); rec != nil {
		t.Fatalf("a deleted service keeps its record: %v", rec)
	}
}
