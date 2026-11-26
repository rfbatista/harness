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
