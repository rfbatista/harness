package app

import (
	"path/filepath"
	"slices"
	"testing"

	"go.uber.org/fx"

	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/ports"
)

// In the shipped graph a stored agent credential reaches the sessions' env,
// and replacing it takes effect on the next start.
func TestSessionsAreBoundToTheStoredAgentCredential(t *testing.T) {
	dir := t.TempDir()
	var (
		orch *orchestration.Service
		cred *agentcredential.Service
	)
	a := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "harness.db"), Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule, GitCredentialsModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, ToolingModule,
		fx.Populate(&orch, &cred),
		fx.NopLogger,
	)
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	if orch.AgentCredential == nil || len(orch.AgentCredential()) != 0 {
		t.Fatal("sessions are not bound to the agent credential, or one exists already")
	}
	if _, err := cred.Set(ports.AgentCredentialAPIKey, "sk-ant-api03-k"); err != nil {
		t.Fatal(err)
	}
	if got := orch.AgentCredential(); !slices.Equal(got, []string{"ANTHROPIC_API_KEY=sk-ant-api03-k"}) {
		t.Fatalf("session env = %v", got)
	}
}
