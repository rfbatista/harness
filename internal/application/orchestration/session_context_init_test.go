package orchestration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
)

func serversEntry() domain.ContextEntry {
	unknown := func(name string) domain.ContextServer {
		return domain.ContextServer{Name: name, Attached: domain.AttachedBuiltIn, Status: domain.MCPUnknown, StatusReason: domain.MCPStatusUnknownReason}
	}
	return domain.ContextEntry{ID: "e1", Kind: domain.ContextEntryStart, At: time.Unix(10, 0),
		MCPServers: []domain.ContextServer{unknown("task"), unknown("github"), unknown("browser")}}
}

func applyInit(t *testing.T, svc *Service, raw string, redact func(string) string) bool {
	t.Helper()
	changed, err := svc.applyInitReport(context.Background(), "s1", json.RawMessage(raw), redact, time.Unix(50, 0))
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

func TestSessionContextInit_TheReportSetsEachServersStatusAndClaudeVersion(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	changed := applyInit(t, svc, `{"type":"system","subtype":"init","claude_code_version":"2.1.316","mcp_servers":[
		{"name":"task","status":"connected"},
		{"name":"github","status":"failed","error":"401 unauthorized"}]}`, nil)
	if !changed {
		t.Fatal("a report with servers reported no change")
	}
	v := contextOf(t, svc, "s1")
	e := v.Entries[0]
	task, _ := serverByName(e, "task")
	gh, _ := serverByName(e, "github")
	browser, _ := serverByName(e, "browser")
	if task.Status != domain.MCPConnected || task.StatusReason != "" {
		t.Errorf("task = %+v", task)
	}
	if gh.Status != domain.MCPFailed || gh.StatusReason != "401 unauthorized" {
		t.Errorf("github = %+v", gh)
	}
	if browser.Status != domain.MCPUnknown || browser.StatusReason != domain.MCPStatusUnknownReason {
		t.Errorf("a server the report does not name must stay unknown: %+v", browser)
	}
	if e.Facts.ClaudeVersion != "2.1.316" {
		t.Errorf("claude version = %q", e.Facts.ClaudeVersion)
	}
	got := map[string]domain.ContextChange{}
	for _, c := range v.Changes {
		got[c.Kind+" "+c.Subject] = c
	}
	if c := got[domain.ContextChangeMCPConnected+" task"]; c.Detail == "" || !c.At.Equal(time.Unix(50, 0)) {
		t.Errorf("mcp_connected = %+v", c)
	}
	if c := got[domain.ContextChangeMCPFailed+" github"]; !strings.Contains(c.Detail, "401 unauthorized") {
		t.Errorf("mcp_failed = %+v", c)
	}
}

func TestSessionContextInit_ANewStatusIsNotGuessed(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	applyInit(t, svc, `{"mcp_servers":[{"name":"github","status":"pending"}]}`, nil)
	gh, _ := serverByName(contextOf(t, svc, "s1").Entries[0], "github")
	if gh.Status != domain.MCPUnknown {
		t.Errorf("a status other than connected or failed must stay unknown: %+v", gh)
	}
}

func TestSessionContextInit_AnErrorCarryingAConfiguredValueIsStoredWithoutIt(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	redact := secretRedactor(llmkit.SessionConfig{
		Env: []string{"GH_TOKEN=ghp_supersecret"},
		MCPServers: []llmkit.MCPServerSpec{{Name: "github",
			Env: map[string]string{"A": "env-value-1"}, Headers: map[string]string{"Authorization": "Bearer header-value-2"}}},
	})
	applyInit(t, svc, `{"mcp_servers":[{"name":"github","status":"failed","error":"auth failed for Bearer header-value-2 with ghp_supersecret and env-value-1"}]}`, redact)
	v := contextOf(t, svc, "s1")
	b, _ := json.Marshal(v)
	for _, leak := range []string{"header-value-2", "ghp_supersecret", "env-value-1"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("stored %q: %s", leak, b)
		}
	}
	gh, _ := serverByName(v.Entries[0], "github")
	if gh.Status != domain.MCPFailed || gh.StatusReason == "" {
		t.Errorf("github = %+v", gh)
	}
}

func TestSessionContextInit_AReportWithoutServersChangesNothing(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	if applyInit(t, svc, `{"type":"system","subtype":"init"}`, nil) {
		t.Error("an init with no servers reported a change")
	}
}

func TestSessionContextInit_ATerminalSessionKeepsItsServersUnknown(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Contexts = contextService(t).Contexts
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	for _, s := range contextOf(t, svc, sess.ID).Entries[0].MCPServers {
		if s.Status != domain.MCPUnknown {
			t.Errorf("%s = %q in a terminal session", s.Name, s.Status)
		}
	}
	for _, c := range contextOf(t, svc, sess.ID).Changes {
		if c.Kind == domain.ContextChangeMCPConnected || c.Kind == domain.ContextChangeMCPFailed {
			t.Errorf("a terminal session got %+v", c)
		}
	}
}

func TestSessionContextInit_AHeadlessStartKeepsClaudesInitReport(t *testing.T) {
	svc, _, _ := newTestService(t)
	svc.Contexts = contextService(t).Contexts
	svc.DefaultEnv = append(svc.DefaultEnv, `CLAUDE_FAKE_INIT={"type":"system","subtype":"init","session_id":"fake","claude_code_version":"9.9.9","mcp_servers":[{"name":"task","status":"connected"}]}`)
	d, err := svc.Start(context.Background(), StartRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Task: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Stop(context.Background(), d.ID) })
	deadline := time.Now().Add(5 * time.Second)
	for {
		e := contextOf(t, svc, d.ID).Entries[0]
		if task, _ := serverByName(e, "task"); task.Status == domain.MCPConnected && e.Facts.ClaudeVersion == "9.9.9" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the init report was not kept: %+v", e.MCPServers)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSessionContextInit_KnownToolsNeverGiveAServerAStatus(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Contexts = contextService(t).Contexts
	svc.MCPKnownTools = func() map[string][]string {
		return map[string][]string{"task": {"get_task", "message_architect"}}
	}
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	e := contextOf(t, svc, sess.ID).Entries[0]
	if _, ok := toolByName(e, "mcp__task__get_task"); !ok {
		t.Fatal("the known tool is not in the entry")
	}
	for _, s := range e.MCPServers {
		if s.Status != domain.MCPUnknown || s.StatusReason != domain.MCPStatusUnknownReason {
			t.Errorf("%s = status %q (%q) in a terminal session with known tools", s.Name, s.Status, s.StatusReason)
		}
	}
}

func TestSessionContextInit_ARepeatedReportAddsNoChange(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	report := `{"mcp_servers":[{"name":"task","status":"connected"}]}`
	if !applyInit(t, svc, report, nil) {
		t.Fatal("the first report reported no change")
	}
	if applyInit(t, svc, report, nil) {
		t.Error("the same report again reported a change")
	}
	n := 0
	for _, c := range contextOf(t, svc, "s1").Changes {
		if c.Kind == domain.ContextChangeMCPConnected {
			n++
		}
	}
	if n != 1 {
		t.Errorf("mcp_connected changes = %d, want 1", n)
	}
}

func TestSessionContextInit_ArgsAndURLSecretsInAnErrorAreStoredWithoutThem(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	redact := secretRedactor(llmkit.SessionConfig{MCPServers: []llmkit.MCPServerSpec{{
		Name: "github", Args: []string{"--token", "arg-secret-1", "--key=arg-secret-2"},
		URL: "https://user:url-pass-3@host/mcp?access_token=query-secret-4&x=1234",
	}}})
	applyInit(t, svc, `{"mcp_servers":[{"name":"github","status":"failed","error":"bad arg-secret-1 arg-secret-2 url-pass-3 query-secret-4"}]}`, redact)
	b, _ := json.Marshal(contextOf(t, svc, "s1"))
	for _, leak := range []string{"arg-secret-1", "arg-secret-2", "url-pass-3", "query-secret-4"} {
		if strings.Contains(string(b), leak) {
			t.Errorf("stored %q: %s", leak, b)
		}
	}
}
