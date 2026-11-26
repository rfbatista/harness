package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BenchSessions = (*Service)(nil)

// benchSetup composes what a bench run's CLI is configured with, the way a
// session of the project would be: the agent's prompt and skills, the
// repository's delivered instruction files and the model. It has no task
// brief, no task or services server and none of the project's MCP servers; the
// caller adds the one mock server.
func (s *Service) benchSetup(ctx context.Context, req ports.BenchSetupRequest) (llmkit.SessionConfig, ports.BenchSetupInfo, func(), error) {
	noop := func() {}
	info := ports.BenchSetupInfo{Model: req.Model}
	proj, err := s.catalog.Projects.GetProject(ctx, req.ProjectID)
	if err != nil {
		return llmkit.SessionConfig{}, info, noop, err
	}
	repositoryID := req.RepositoryID
	if repositoryID == "" {
		repos, err := s.catalog.Repositories.ListRepositories(ctx, proj.ID)
		if err != nil {
			return llmkit.SessionConfig{}, info, noop, err
		}
		if len(repos) > 0 {
			repositoryID = repos[0].ID
		}
	}
	ag, err := s.resolveAgent(req.AgentID, domain.SessionModeDefault)
	if err != nil {
		return llmkit.SessionConfig{}, info, noop, err
	}
	cleanup := noop
	if ag.cleanup != nil {
		cleanup = ag.cleanup
	}

	cfg := llmkit.SessionConfig{
		Model:        req.Model,
		Permission:   llmkit.PermissionAsk,
		Env:          s.DefaultEnv,
		AppendSystem: ag.appendSystem,
		Driver:       claude.Config{SkillDirs: ag.skillDirs},
	}
	cfg.InstructionFiles, _ = s.deliveredFiles(ctx, domain.RoleDelegate, ag.agent, proj.ID, repositoryID)
	s.withAgentCredential(&cfg, proj.ID)

	if ag.agent != nil {
		info.AgentName = ag.agent.Name
	}
	info.AgentPrompt = ag.appendSystem
	for _, sk := range sessionSkills(ag.agent, nil) {
		for _, f := range sk.Files {
			info.Skills = append(info.Skills, ports.BenchSetupFile{Path: domain.Slug(sk.Name) + "/" + f.Path, Content: f.Content})
		}
	}
	for _, f := range cfg.InstructionFiles {
		info.Instructions = append(info.Instructions, ports.BenchSetupFile{Path: f.Path, Content: string(f.Content)})
	}
	return cfg, info, cleanup, nil
}

// Setup describes what a bench run on the request is set up with. The CLI
// version is read here, at each run's start and not once per process, so a
// CLI that was updated between two runs shows in their fingerprints.
func (s *Service) Setup(ctx context.Context, req ports.BenchSetupRequest) (ports.BenchSetupInfo, error) {
	_, info, cleanup, err := s.benchSetup(ctx, req)
	cleanup()
	if err == nil && s.BenchCLIVersion != nil {
		info.CLIVersion = s.BenchCLIVersion()
	}
	return info, err
}

// benchBuiltInTools are the CLI's own tools, all turned off for a bench run:
// the agent is measured on the mock tools alone, and nothing it does may reach
// the machine.
var benchBuiltInTools = []string{
	"Bash", "BashOutput", "KillShell", "Edit", "MultiEdit", "Write", "NotebookEdit",
	"Read", "Glob", "Grep", "WebFetch", "WebSearch", "Task", "Agent", "TodoWrite", "SlashCommand", "ExitPlanMode",
}

// withBenchToolsLocked leaves a bench run's CLI with the mock server's tools
// and nothing else. The built-in tools are denied in its settings, and only
// the mock server is allowed, with no approval tool and no permission bypass:
// any other tool call is refused by the CLI.
func withBenchToolsLocked(cfg *llmkit.SessionConfig, mcpName string) {
	cfg.Permission = llmkit.PermissionAsk
	cfg.AllowedTools = []string{"mcp__" + mcpName}
	deny, _ := json.Marshal(map[string][]string{"deny": benchBuiltInTools})
	withClaudeSetting(cfg, "permissions", deny)
}

// Run starts the CLI headless in a throwaway directory with the project's
// setup and the one mock MCP server, sends the prompt and waits for its
// result. Nothing of the project is touched: the directory is made here and
// removed after, and the project's own MCP servers are not attached.
func (s *Service) Run(ctx context.Context, req ports.BenchSessionRequest) (ports.BenchSessionResult, error) {
	if s.BenchRuntime == nil {
		return ports.BenchSessionResult{}, &domain.StructuredError{Code: "BENCH_UNAVAILABLE", Message: "this server cannot run an eval bench"}
	}
	cfg, _, cleanup, err := s.benchSetup(ctx, req.BenchSetupRequest)
	defer cleanup()
	if err != nil {
		return ports.BenchSessionResult{}, err
	}
	dir, err := os.MkdirTemp("", "harness-bench-*")
	if err != nil {
		return ports.BenchSessionResult{}, err
	}
	defer os.RemoveAll(dir)

	cfg.ID = llmkit.NewSessionID()
	cfg.WorkingDir = dir
	cfg.Attribution = cfg.ID
	cfg.MaxTurns = req.MaxTurns
	cfg.MCPServers = []llmkit.MCPServerSpec{{Name: req.MCPName, Transport: "http", URL: req.MCPURL}}
	cfg.Env = append(slices.Clone(cfg.Env), "TMPDIR="+dir, "TMP="+dir, "TEMP="+dir)
	withBenchToolsLocked(&cfg, req.MCPName)
	// Under the session sandbox driver the CLI writes this directory and
	// nothing else; a platform with the sandbox on and no driver refuses.
	wrap, err := s.Sandbox.BenchWrapFor(req.ProjectID, dir, s.BenchSandboxAllow)
	if err != nil {
		return ports.BenchSessionResult{}, err
	}
	if wrap != nil {
		cfg.Wrap = wrap
		withClaudeSandboxOff(&cfg)
	}

	started := time.Now()
	sess, err := s.BenchRuntime.Start(ctx, cfg)
	if err != nil {
		return ports.BenchSessionResult{}, fmt.Errorf("start bench session: %w", err)
	}
	defer func() { _ = sess.Stop() }()
	if err := sess.Send(ctx, req.Prompt); err != nil {
		return ports.BenchSessionResult{}, fmt.Errorf("send the bench prompt: %w", err)
	}
	var res ports.BenchSessionResult
	var text strings.Builder
	for {
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case ev, open := <-sess.Events():
			if !open {
				return res, &domain.StructuredError{Code: "BENCH_RUN_ENDED", Message: "the CLI ended before it reported a result"}
			}
			switch ev.Type {
			case llmkit.EventAssistant:
				text.WriteString(ev.Text)
			case llmkit.EventResult:
				res.FinalText = ev.Text
				if res.FinalText == "" {
					res.FinalText = text.String()
				}
				res.CostUSD = ev.CostUSD
				res.Duration = time.Since(started)
				return res, nil
			case llmkit.EventError, llmkit.EventExit:
				return res, &domain.StructuredError{Code: "BENCH_RUN_ENDED", Message: "the CLI ended before it reported a result: " + ev.Err}
			}
		}
	}
}
