package bench

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// CaseRun is what running one case left: the calls the agent made through
// the mock layer, its final result, and what the run cost.
type CaseRun struct {
	Calls     []domain.BenchCall
	FinalText string
	CostUSD   float64
	Duration  time.Duration
}

// SetupOf is the setup a scorecard's fingerprint is taken over: what a session
// would be set up with, and the model that judges.
func SetupOf(info ports.BenchSetupInfo, judgeModel string) Setup {
	return Setup{
		AgentName:    info.AgentName,
		AgentPrompt:  info.AgentPrompt,
		Skills:       setupFiles(info.Skills),
		Instructions: setupFiles(info.Instructions),
		Model:        info.Model,
		JudgeModel:   judgeModel,
		CLIVersion:   info.CLIVersion,
	}
}

func setupFiles(in []ports.BenchSetupFile) []SetupFile {
	out := make([]SetupFile, len(in))
	for i, f := range in {
		out[i] = SetupFile{Path: f.Path, Content: f.Content}
	}
	return out
}

// RunCase runs one case once: the real CLI on the project's setup, given the
// case's mock layer as its only tools. It records what the agent called.
func (s *Service) RunCase(ctx context.Context, setup ports.BenchSetupRequest, c domain.BenchCase) (CaseRun, error) {
	if s.d.Sessions == nil || s.d.ServeMock == nil {
		return CaseRun{}, &domain.StructuredError{Code: "BENCH_UNAVAILABLE", Message: "this server cannot run an eval bench"}
	}
	layer := NewMockLayer(c.Tools)
	url, stop, err := s.d.ServeMock(layer)
	if err != nil {
		return CaseRun{}, err
	}
	defer stop()
	res, err := s.d.Sessions.Run(ctx, ports.BenchSessionRequest{BenchSetupRequest: setup, Prompt: c.Prompt, MCPName: s.d.MockName, MCPURL: url})
	run := CaseRun{Calls: layer.Calls(), FinalText: res.FinalText, CostUSD: res.CostUSD, Duration: res.Duration}
	return run, err
}
