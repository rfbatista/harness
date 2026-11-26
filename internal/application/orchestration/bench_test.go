package orchestration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/adapter/in/mcpbench"
	"operators-mcp/internal/application/bench"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

// benchWorld is a bench service over the orchestration service whose CLI is
// the scripted fake: a project with a repository, an agent with a prompt and
// a skill, a catalog MCP server the agent is attached to, and an instruction
// file.
type benchWorld struct {
	bench      *bench.Service
	sessions   *Service
	launchFile string
	repoRoot   string
}

func newBenchWorld(t *testing.T, script string) *benchWorld {
	t.Helper()
	svc, _, _ := newTestService(t)
	res := svc.catalog.Projects.(*fakeResolver)
	res.servers = []*domain.MCPServer{{ID: "gh", Name: "github", Transport: "http", URL: "http://127.0.0.1:1/github"}}
	res.agents = map[string]*domain.Agent{"dev": {
		ID: "dev", Name: "go-developer", MCPServerIDs: []string{"gh"},
		Prompt: &domain.Prompt{Content: "You write Go, test first."},
		Skills: []domain.Skill{{Name: "tdd", Files: []domain.SkillFile{{Path: "SKILL.md", Content: "---\nname: tdd\ndescription: red green\n---\nRed then green."}}}},
	}}
	svc.Architectures = withInstructions(instructionFile("CLAUDE.md", "memory", "be brief"))

	exe, _ := os.Executable()
	launch := filepath.Join(t.TempDir(), "launch.json")
	svc.DefaultEnv = []string{"CLAUDE_FAKE=1", fakeScriptEnv + "=" + script, fakeBenchRecordEnv + "=" + launch}
	svc.BenchRuntime = claude.New(llmkit.Options{Bin: exe})
	svc.BenchCLIVersion = func() string { return "2.1.0" }

	b := bench.New(bench.Deps{
		Sessions:  svc,
		ServeMock: mcpbench.Serve,
		MockName:  mcpbench.Name,
	})
	return &benchWorld{bench: b, sessions: svc, launchFile: launch, repoRoot: res.repo.RootDir}
}

func (w *benchWorld) serveMock(l *bench.MockLayer) (string, func(), error) { return mcpbench.Serve(l) }

func (w *benchWorld) launch(t *testing.T) fakeBenchLaunch {
	t.Helper()
	b, err := os.ReadFile(w.launchFile)
	if err != nil {
		t.Fatalf("the fake CLI recorded no launch: %v", err)
	}
	var l fakeBenchLaunch
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	return l
}

var benchSetupReq = ports.BenchSetupRequest{ProjectID: "p1", RepositoryID: "r1", AgentID: "dev", Model: "sonnet"}

var readmeCase = domain.BenchCase{
	Prompt: "Read the README.",
	Tools:  []domain.BenchTool{{Name: "read_file", Result: "# Demo"}},
}

func runBenchCase(t *testing.T, w *benchWorld, c domain.BenchCase) bench.CaseRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	run, err := w.bench.RunCase(ctx, benchSetupReq, c)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestBenchRunLaunchesTheCLIWithOnlyTheMockServerOutsideEveryRepository(t *testing.T) {
	w := newBenchWorld(t, `[{"tool":"read_file","params":{"path":"README.md"}}]`)
	run := runBenchCase(t, w, readmeCase)

	l := w.launch(t)
	if !slices.Equal(l.Servers, []string{"bench"}) {
		t.Fatalf("the CLI got MCP servers %v, want only the mock one", l.Servers)
	}
	if strings.Contains(l.MCPConfig, "github") || strings.Contains(l.MCPConfig, "permission") {
		t.Fatalf("the project's or the harness's servers are attached: %s", l.MCPConfig)
	}
	if strings.HasPrefix(l.Cwd, w.repoRoot) {
		t.Fatalf("the run's directory %s is under the repository %s", l.Cwd, w.repoRoot)
	}
	if _, err := os.Stat(l.Cwd); !os.IsNotExist(err) {
		t.Fatalf("the throwaway directory %s was not removed (%v)", l.Cwd, err)
	}
	if run.FinalText == "" || run.CostUSD == 0 {
		t.Fatalf("run = %+v, want the CLI's result and cost", run)
	}
}

func TestBenchRunRecordsTheCallAndAnswersWithTheCannedResult(t *testing.T) {
	w := newBenchWorld(t, `[{"tool":"read_file","params":{"path":"README.md"}},{"tool":"delete_everything","params":{}}]`)
	run := runBenchCase(t, w, readmeCase)

	if len(run.Calls) != 2 || run.Calls[0].Tool != "read_file" || run.Calls[0].Params["path"] != "README.md" || run.Calls[1].Tool != "delete_everything" {
		t.Fatalf("recorded calls = %+v", run.Calls)
	}
	answers := w.launch(t).Answers
	if len(answers) != 2 || !strings.Contains(answers[0], "# Demo") {
		t.Fatalf("answers = %v, want the canned result first", answers)
	}
	if !strings.Contains(answers[1], "isError") && !strings.Contains(answers[1], "error") {
		t.Fatalf("the unmocked tool was not answered with an error: %s", answers[1])
	}
}

func TestBenchRunReceivesTheProjectsSetupAsASessionWould(t *testing.T) {
	w := newBenchWorld(t, `[]`)
	runBenchCase(t, w, readmeCase)

	l := w.launch(t)
	args := strings.Join(l.Args, "\x00")
	if !strings.Contains(args, "--model\x00sonnet") {
		t.Fatalf("the model is not passed: %v", l.Args)
	}
	if !strings.Contains(args, "You write Go, test first.") {
		t.Fatalf("the agent's prompt is not passed: %v", l.Args)
	}
	if !strings.Contains(args, "--plugin-dir") {
		t.Fatalf("the agent's skills are not passed: %v", l.Args)
	}

	info, err := w.sessions.Setup(context.Background(), benchSetupReq)
	if err != nil {
		t.Fatal(err)
	}
	if info.AgentName != "go-developer" || info.AgentPrompt != "You write Go, test first." || info.Model != "sonnet" || info.CLIVersion != "2.1.0" {
		t.Fatalf("setup = %+v", info)
	}
	if len(info.Skills) == 0 || !strings.Contains(info.Skills[0].Content, "Red then green.") {
		t.Fatalf("setup skills = %+v", info.Skills)
	}
	var paths []string
	for _, f := range info.Instructions {
		paths = append(paths, f.Path)
	}
	if !slices.Contains(paths, "CLAUDE.md") || !slices.Contains(paths, "ARCHITECTURE.md") {
		t.Fatalf("setup instructions = %v, want the repository's files", paths)
	}
}

func TestBenchSetupFingerprintFollowsTheAgentPrompt(t *testing.T) {
	w := newBenchWorld(t, `[]`)
	fingerprint := func() string {
		info, err := w.sessions.Setup(context.Background(), benchSetupReq)
		if err != nil {
			t.Fatal(err)
		}
		return bench.Fingerprint(bench.SetupOf(info, "opus"))
	}
	before, again := fingerprint(), fingerprint()
	if before != again {
		t.Fatal("an unchanged setup changed its fingerprint")
	}
	w.sessions.catalog.Projects.(*fakeResolver).agents["dev"].Prompt.Content = "You write Rust."
	if fingerprint() == before {
		t.Fatal("a changed agent prompt kept the fingerprint")
	}
}

// settingsOf is the JSON the CLI was handed through --settings.
func launchSettings(t *testing.T, args []string) map[string]json.RawMessage {
	t.Helper()
	i := slices.Index(args, "--settings")
	if i < 0 || i+1 >= len(args) {
		t.Fatalf("the CLI got no --settings: %v", args)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args[i+1]), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBenchRunLaunchesWithNoBuiltInToolAllowedAndNoPermissionBypass(t *testing.T) {
	w := newBenchWorld(t, `[]`)
	runBenchCase(t, w, readmeCase)

	l := w.launch(t)
	args := strings.Join(l.Args, "\x00")
	if strings.Contains(args, "bypassPermissions") || strings.Contains(args, "--permission-prompt-tool") {
		t.Fatalf("the run is not gated by its tool list alone: %v", l.Args)
	}
	i := slices.Index(l.Args, "--allowedTools")
	if i < 0 || i+1 >= len(l.Args) || l.Args[i+1] != "mcp__bench" || i+2 >= len(l.Args) || l.Args[i+2] != "Skill" || (i+3 < len(l.Args) && !strings.HasPrefix(l.Args[i+3], "--")) {
		t.Fatalf("only the mock server's tools and Skill may be allowed: %v", l.Args)
	}
	var perms struct {
		Deny []string `json:"deny"`
	}
	if err := json.Unmarshal(launchSettings(t, l.Args)["permissions"], &perms); err != nil {
		t.Fatalf("the settings carry no permissions: %v", err)
	}
	for _, tool := range []string{"Bash", "Edit", "Write", "MultiEdit", "NotebookEdit", "WebFetch", "WebSearch", "Read", "Glob", "Grep", "Task", "TodoWrite"} {
		if !slices.Contains(perms.Deny, tool) {
			t.Errorf("built-in %s is not denied: %v", tool, perms.Deny)
		}
	}
	if !slices.Equal(l.Servers, []string{"bench"}) {
		t.Fatalf("servers = %v", l.Servers)
	}
}

func TestBenchRunLimitsTheCLIsTurns(t *testing.T) {
	w := newBenchWorld(t, `[]`)
	runBenchCase(t, w, readmeCase)
	l := w.launch(t)
	i := slices.Index(l.Args, "--max-turns")
	if i < 0 || i+1 >= len(l.Args) || l.Args[i+1] == "0" {
		t.Fatalf("the CLI has no turn limit: %v", l.Args)
	}
}

func TestBenchRunUnderTheSandboxWritesOnlyItsThrowawayDirectory(t *testing.T) {
	w := newBenchWorld(t, `[]`)
	driver := &portstest.RecordingSandboxDriver{}
	home := t.TempDir()
	projectHomes := t.TempDir()
	w.sessions.Sandbox = &Sandbox{Driver: driver, Home: home, ProjectHomesRoot: projectHomes, Checkouts: func() []string { return []string{w.repoRoot} }}
	runBenchCase(t, w, readmeCase)

	calls := driver.Calls()
	if len(calls) != 1 {
		t.Fatalf("the CLI was wrapped %d times, want once", len(calls))
	}
	l := w.launch(t)
	// The directory is gone after the run, so it is told by its name.
	dirName := filepath.Base(l.Cwd)
	policy := calls[0].Policy
	if filepath.Base(policy.WorkingDir) != dirName || !strings.HasPrefix(dirName, "harness-bench-") {
		t.Fatalf("working dir = %s, want the throwaway dir %s", policy.WorkingDir, l.Cwd)
	}
	resolvedHome, _ := filepath.EvalSymlinks(home)
	underHome := func(p string) bool {
		r, err := filepath.EvalSymlinks(p)
		return err == nil && strings.HasPrefix(r, resolvedHome)
	}
	var throwaway int
	for _, p := range policy.Writable {
		switch {
		case filepath.Base(p) == dirName:
			throwaway++
		case !underHome(p):
			t.Errorf("writable %s is neither the throwaway dir nor the agent home", p)
		}
	}
	if throwaway == 0 {
		t.Fatalf("the throwaway dir is not writable: %v", policy.Writable)
	}
	if !slices.Contains(policy.Hidden, projectHomes) {
		t.Fatalf("the folder of the project Claude homes is not hidden: %v", policy.Hidden)
	}
	if !slices.Contains(policy.Hidden, w.repoRoot) {
		t.Fatalf("the project repository is not hidden: %v", policy.Hidden)
	}
	if got := launchSettings(t, l.Args)["sandbox"]; string(got) != `{"enabled":false}` {
		t.Fatalf("claude's own sandbox is not switched off under ours: %s", got)
	}
}

func TestBenchRunIsRefusedUnderASandboxWithNoDriver(t *testing.T) {
	w := newBenchWorld(t, `[]`)
	w.sessions.Sandbox = &Sandbox{}
	_, err := w.bench.RunCase(context.Background(), benchSetupReq, readmeCase)
	if err == nil {
		t.Fatal("a run went unconfined with the sandbox on and no driver")
	}
	if _, statErr := os.Stat(w.launchFile); statErr == nil {
		t.Fatal("a CLI was started")
	}
}

func TestBenchSetupReadsTheCLIVersionAtEachCallAndARunDoesNotAskForIt(t *testing.T) {
	w := newBenchWorld(t, `[]`)
	calls := 0
	w.sessions.BenchCLIVersion = func() string { calls++; return "2.1." + string(rune('0'+calls)) }
	first, _ := w.sessions.Setup(context.Background(), benchSetupReq)
	second, _ := w.sessions.Setup(context.Background(), benchSetupReq)
	if first.CLIVersion != "2.1.1" || second.CLIVersion != "2.1.2" {
		t.Fatalf("versions %q %q: a version read once per process hides a CLI that changed", first.CLIVersion, second.CLIVersion)
	}
	runBenchCase(t, w, readmeCase)
	if calls != 2 {
		t.Fatalf("a case run asked for the CLI version (%d calls)", calls)
	}
}
