package orchestration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/adapter/out/persistence/memory"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The secret values these tests plant. Each is distinctive, so finding one in
// anything stored or served means the value leaked.
const (
	secretEnv    = "sk-ant-env-7f3a91c2"
	secretHeader = "hdr-token-5be0d4aa"
	secretMCPEnv = "mcp-env-value-9c1d7e33"
	secretArg    = "arg-secret-2d8f60b1"
	secretURL    = "url-query-secret-4a6e15f0"
	secretBasic  = "dXNlcjpwYXNzd29yZC04ZDFh"
)

var allSecrets = []string{secretEnv, secretHeader, secretMCPEnv, secretArg, secretURL, secretBasic}

// leakyServer is a catalog MCP server that carries a secret in every place a
// server can: its environment, its headers, its arguments and its URL.
func leakyServer() *domain.MCPServer {
	return &domain.MCPServer{
		ID: "vault", Name: "vault", Transport: "http",
		Command: "vault-mcp", Args: []string{"--token", secretArg},
		URL:     "https://vault.example/mcp?access_token=" + secretURL,
		Env:     map[string]string{"VAULT_TOKEN": secretMCPEnv},
		Headers: map[string]string{"Authorization": "Bearer " + secretHeader, "Proxy-Authorization": "Basic " + secretBasic},
	}
}

// leakyConfig is a session configuration with the same secrets.
func leakyConfig() llmkit.SessionConfig {
	s := leakyServer()
	return llmkit.SessionConfig{
		Env:        []string{"ANTHROPIC_API_KEY=" + secretEnv, "PATH=/bin"},
		MCPServers: []llmkit.MCPServerSpec{{Name: s.Name, Transport: s.Transport, Command: s.Command, URL: s.URL, Args: s.Args, Env: s.Env, Headers: s.Headers}},
	}
}

// assertNoSecret fails for each planted secret found in v, however it is shaped.
func assertNoSecret(t *testing.T, what string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range allSecrets {
		if strings.Contains(string(b), s) {
			t.Errorf("%s leaks %q", what, s)
		}
	}
}

// withLeakyProject gives the service a project that adds the leaky server, and
// puts the planted environment secret in the session's environment.
func withLeakyProject(svc *Service) {
	res := svc.catalog.Projects.(*fakeResolver)
	res.servers = []*domain.MCPServer{leakyServer()}
	res.proj.MCPServers = []string{"vault"}
	svc.DefaultEnv = append(svc.DefaultEnv, "ANTHROPIC_API_KEY="+secretEnv)
	svc.AgentCredential = func() []string { return []string{"CLAUDE_CODE_OAUTH_TOKEN=" + secretEnv} }
}

func TestSessionContextSecrets_AnEntryBuiltFromALeakyConfigurationCarriesNone(t *testing.T) {
	cfg := leakyConfig()
	// The brief is part of the prompt the entry keeps: the entry must hold what
	// the person wrote, never what the session's environment holds.
	cfg.AppendSystem = "## Your task\n\nDeploy the service."
	trail := &pieceTrail{}
	trail.mark(&cfg, domain.PieceBrief, "Task brief", "Harness")
	e := buildContextEntry(contextInput{
		Kind: domain.ContextEntryStart, ID: "e", Cfg: cfg, System: trail.pieces,
		Attached: mcpAttachment{Servers: []*domain.MCPServer{leakyServer()}}, KnownTools: map[string][]string{"vault": {"read_secret"}},
		Opening: "go",
	})
	assertNoSecret(t, "entry", e)
	assertNoSecret(t, "entry view", e.View())
	if _, ok := serverByName(e, "vault"); !ok {
		t.Fatal("the server is not in the entry: the test would pass for the wrong reason")
	}
}

func TestSessionContextSecrets_AnEntryNeverNamesAnEnvironmentVariableOrAHeader(t *testing.T) {
	e := buildContextEntry(contextInput{Kind: domain.ContextEntryStart, ID: "e", Cfg: leakyConfig(),
		Attached: mcpAttachment{Servers: []*domain.MCPServer{leakyServer()}}})
	b, _ := json.Marshal(e)
	// C2: not even by name.
	for _, name := range []string{"ANTHROPIC_API_KEY", "VAULT_TOKEN", "Authorization", "Proxy-Authorization", "access_token", "--token", "vault-mcp", "vault.example"} {
		if strings.Contains(string(b), name) {
			t.Errorf("entry names %q: %s", name, b)
		}
	}
}

func TestSessionContextSecrets_ARealTerminalStartStoresAndServesNone(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Contexts = memory.NewSessionContextStore()
	withLeakyProject(svc)
	sess, _ := startInteractive(t, svc, InteractiveRequest{Prompt: "begin"})

	v := contextOf(t, svc, sess.ID)
	if _, ok := serverByName(v.Entries[0], "vault"); !ok {
		t.Fatalf("the project's server did not reach the session: %+v", v.Entries[0].MCPServers)
	}
	assertNoSecret(t, "served context", v)
	stored, err := svc.Contexts.Load(context.Background(), sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertNoSecret(t, "stored context", stored)
	for _, p := range v.Entries[0].Instructions {
		full, err := svc.SessionContextPiece(context.Background(), sess.ID, v.Entries[0].ID, p.ID)
		if err != nil {
			t.Fatal(err)
		}
		assertNoSecret(t, "piece "+p.ID, full)
	}
}

func TestSessionContextSecrets_ARealResumeStoresAndServesNone(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Contexts = memory.NewSessionContextStore()
	withLeakyProject(svc)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ResumeInteractive(context.Background(), resumeOf(sess.ID)); err != nil {
		t.Fatal(err)
	}
	v := contextOf(t, svc, sess.ID)
	if len(v.Entries) != 2 {
		t.Fatalf("entries = %d", len(v.Entries))
	}
	assertNoSecret(t, "context after a resume", v)
}

func TestSessionContextSecrets_ARealHeadlessStartStoresAndServesNone(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.Contexts = memory.NewSessionContextStore()
	withLeakyProject(svc)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })
	v := contextOf(t, svc, d.ID)
	if _, ok := serverByName(v.Entries[0], "vault"); !ok {
		t.Fatalf("the project's server did not reach the session: %+v", v.Entries[0].MCPServers)
	}
	assertNoSecret(t, "served context", v)
}

func TestSessionContextSecrets_AHeadlessInitErrorRepeatingConfiguredValuesIsStoredWithoutThem(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	redact := secretRedactor(leakyConfig())
	cases := map[string]string{
		"env value":            "spawn failed with ANTHROPIC_API_KEY=" + secretEnv,
		"mcp env value":        "VAULT_TOKEN=" + secretMCPEnv + " rejected",
		"bearer header":        "401 for Authorization: Bearer " + secretHeader,
		"basic header":         "407 for Proxy-Authorization: Basic " + secretBasic,
		"header value alone":   "rejected token " + secretHeader,
		"a value twice":        secretEnv + " and again " + secretEnv,
		"split over two lines": "first line\n   " + secretMCPEnv + "\nlast line",
		"in the middle":        "x" + secretHeader + "y",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"mcp_servers": []map[string]string{{"name": "github", "status": "failed", "error": text}}})
			// A different text each time, so each is a change of its own.
			applyInit(t, svc, string(raw), redact)
			assertNoSecret(t, "context", contextOf(t, svc, "s1"))
		})
	}
}

// A failed server's error often echoes the command line claude ran or the URL
// it called. Both can hold a secret the harness was given as an argument or in
// a query string (contract C2: tokens and credentials are never served).
func TestSessionContextSecrets_AHeadlessInitErrorEchoingACommandArgumentIsStoredWithoutIt(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	redact := secretRedactor(leakyConfig())
	raw, _ := json.Marshal(map[string]any{"mcp_servers": []map[string]string{{
		"name": "github", "status": "failed", "error": "spawn vault-mcp --token " + secretArg + " ENOENT",
	}}})
	applyInit(t, svc, string(raw), redact)
	v := contextOf(t, svc, "s1")
	for _, c := range v.Changes {
		if strings.Contains(c.Detail, secretArg) {
			t.Errorf("a change leaks the argument: %+v", c)
		}
	}
	gh, _ := serverByName(v.Entries[0], "github")
	if strings.Contains(gh.StatusReason, secretArg) {
		t.Errorf("status_reason leaks a command argument: %q", gh.StatusReason)
	}
}

func TestSessionContextSecrets_AHeadlessInitErrorEchoingTheURLIsStoredWithoutItsQuerySecret(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	redact := secretRedactor(leakyConfig())
	raw, _ := json.Marshal(map[string]any{"mcp_servers": []map[string]string{{
		"name": "github", "status": "failed", "error": "GET https://vault.example/mcp?access_token=" + secretURL + " returned 401",
	}}})
	applyInit(t, svc, string(raw), redact)
	gh, _ := serverByName(contextOf(t, svc, "s1").Entries[0], "github")
	if strings.Contains(gh.StatusReason, secretURL) {
		t.Errorf("status_reason leaks the URL's query secret: %q", gh.StatusReason)
	}
}

func TestSessionContextSecrets_ABearerTokenIsRemovedEvenWithoutAKnownConfiguration(t *testing.T) {
	// No redactor: a restart lost it, or the session never had one.
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	applyInit(t, svc, `{"mcp_servers":[{"name":"github","status":"failed","error":"401 with Authorization: bearer abc123SECRET and BEARER   zzz999SECRET"}]}`, nil)
	gh, _ := serverByName(contextOf(t, svc, "s1").Entries[0], "github")
	if strings.Contains(gh.StatusReason, "abc123SECRET") || strings.Contains(gh.StatusReason, "zzz999SECRET") {
		t.Errorf("status_reason = %q", gh.StatusReason)
	}
}

func TestSessionContextSecrets_ASecretAtTheEdgeOfTheErrorCutIsStillRemoved(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	redact := secretRedactor(leakyConfig())
	// The secret starts before byte 400 and ends after it: cutting first would
	// leave its head in the text.
	text := strings.Repeat("e", maxStatusReason-8) + secretMCPEnv + " tail"
	raw, _ := json.Marshal(map[string]any{"mcp_servers": []map[string]string{{"name": "github", "status": "failed", "error": text}}})
	applyInit(t, svc, string(raw), redact)
	gh, _ := serverByName(contextOf(t, svc, "s1").Entries[0], "github")
	if strings.Contains(gh.StatusReason, secretMCPEnv[:8]) {
		t.Errorf("the head of a secret survived the cut: %q", gh.StatusReason)
	}
	if len(gh.StatusReason) > maxStatusReason {
		t.Errorf("reason is %d bytes, over %d", len(gh.StatusReason), maxStatusReason)
	}
}

func TestSessionContextSecrets_ARealHeadlessInitErrorEchoingTheSessionsEnvironmentIsStoredWithoutIt(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.Contexts = memory.NewSessionContextStore()
	withLeakyProject(svc)
	svc.DefaultEnv = append(svc.DefaultEnv, `CLAUDE_FAKE_INIT={"type":"system","subtype":"init","session_id":"fake","mcp_servers":[{"name":"task","status":"failed","error":"auth failed for ANTHROPIC_API_KEY=`+secretEnv+` and Bearer `+secretHeader+`"}]}`)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })
	deadline := time.Now().Add(5 * time.Second)
	for {
		e := contextOf(t, svc, d.ID).Entries[0]
		if task, _ := serverByName(e, "task"); task.Status == domain.MCPFailed {
			assertNoSecret(t, "served context", contextOf(t, svc, d.ID))
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the init report was not kept: %+v", e.MCPServers)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSessionContextSecrets_TheSecretsAreForgottenWhenTheSessionEnds(t *testing.T) {
	svc := contextService(t)
	svc.keepRedactor("s1", leakyConfig())
	if svc.redactors["s1"] == nil {
		t.Fatal("not kept")
	}
	svc.dropRedactor("s1")
	if _, ok := svc.redactors["s1"]; ok {
		t.Error("a session's secret values stay in memory after it ends")
	}
}

func resumeOf(id string) ports.ResumeRequest { return ports.ResumeRequest{SessionID: id} }
