//go:build !windows

package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/harnesskit/errs"
	"go.uber.org/fx"

	"operators-mcp/internal/application/agentcredential"
	"operators-mcp/internal/application/agentoauth"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/ports"
)

// In the shipped graph a login file in the agent home satisfies the
// readiness gate, wins over the pasted credential at each start, and gives
// sessions the security shim dir on macOS.
func TestALoginFileInTheAgentHomeSatisfiesTheGateAndWinsOverThePastedCredential(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "harness.db"), Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct", ShimBinary: "/opt/harness/server"}
	var (
		orch *orchestration.Service
		cred *agentcredential.Service
	)
	a := fx.New(
		fx.Supply(cfg),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule, GitCredentialsModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, ToolingModule,
		fx.Populate(&orch, &cred),
		fx.NopLogger,
	)
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(context.Background()) })
	ctx := context.Background()
	if err := orch.Ready.Check(ctx); errs.Code(err) != "AGENT_CREDENTIAL_MISSING" {
		t.Fatalf("no credential, no login: %v", err)
	}
	if _, err := cred.Set(ports.AgentCredentialAPIKey, "sk-ant-api03-k"); err != nil {
		t.Fatal(err)
	}
	if orch.AgentLogin() || len(orch.AgentCredential()) != 1 {
		t.Fatal("pasted credential not served without a login")
	}
	if _, err := cred.Remove(); err != nil {
		t.Fatal(err)
	}
	home, err := agentClaudeHomeDir(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".credentials.json"), []byte(`{"claudeAiOauth":{"accessToken":"x"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := orch.Ready.Check(ctx); err != nil {
		t.Fatalf("with a login file: %v", err)
	}
	if !orch.AgentLogin() {
		t.Fatal("sessions do not see the login")
	}
	if got := orch.KeychainShimDir != ""; got != (runtime.GOOS == "darwin") {
		t.Fatalf("keychain shim dir %q on %s", orch.KeychainShimDir, runtime.GOOS)
	}
}

func TestAOneShotUsesThePastedCredentialOnlyWithoutALoginAndGetsTheShimOnPath(t *testing.T) {
	pasted := func() []string { return []string{"ANTHROPIC_API_KEY=key"} }
	t.Setenv("PATH", "/usr/bin:/bin")
	env := oneShotEnv(pasted, func() bool { return false }, "/kc")()
	if !slices.Contains(env, "ANTHROPIC_API_KEY=key") || !slices.Contains(env, "PATH=/kc:/usr/bin:/bin") {
		t.Fatalf("no login: %v", env)
	}
	env = oneShotEnv(pasted, func() bool { return true }, "/kc")()
	if slices.Contains(env, "ANTHROPIC_API_KEY=key") || !slices.Contains(env, "PATH=/kc:/usr/bin:/bin") {
		t.Fatalf("login: %v", env)
	}
	if env = oneShotEnv(pasted, func() bool { return true }, "")(); len(env) != 0 {
		t.Fatalf("no shim dir: %v", env)
	}
}

// The login, status and logout commands run with the shim dir first on PATH.
func TestTheSignInCommandsRunWithTheSecurityShimFirstOnPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s' \"$PATH\" > \"$CLAUDE_CONFIG_DIR/path\"\necho '{\"loggedIn\": false}'\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")
	t.Setenv("PATH", "/usr/bin:/bin")
	a := agentoauth.NewAuth(agentoauth.AuthDeps{Bin: loginBin(bin), ConfigDir: home,
		Env: func() []string { return withKeychainShim(os.Environ(), "/kc") }})
	if _, err := a.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(home, "path"))
	if !strings.HasPrefix(string(got), "/kc:/usr/bin") {
		t.Fatalf("PATH = %q", got)
	}
}
