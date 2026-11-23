package orchestration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
)

func initOf(servers ...[3]string) string {
	var list []map[string]string
	for _, s := range servers {
		m := map[string]string{"name": s[0], "status": s[1]}
		if s[2] != "" {
			m["error"] = s[2]
		}
		list = append(list, m)
	}
	b, _ := json.Marshal(map[string]any{"type": "system", "subtype": "init", "mcp_servers": list})
	return string(b)
}

func changesOfKind(t *testing.T, svc *Service, kind string) []domain.ContextChange {
	t.Helper()
	var out []domain.ContextChange
	for _, c := range contextOf(t, svc, "s1").Changes {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

func githubOf(t *testing.T, svc *Service) domain.ContextServer {
	t.Helper()
	s, _ := serverByName(contextOf(t, svc, "s1").Entries[0], "github")
	return s
}

func TestSessionContextInitMore_AFailedServerThatThenConnectsLosesItsErrorAndSaysSo(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	applyInit(t, svc, initOf([3]string{"github", "failed", "connection refused"}), nil)
	if s := githubOf(t, svc); s.Status != domain.MCPFailed || s.StatusReason != "connection refused" {
		t.Fatalf("github = %+v", s)
	}
	if !applyInit(t, svc, initOf([3]string{"github", "connected", ""}), nil) {
		t.Error("failed → connected reported no change")
	}
	if s := githubOf(t, svc); s.Status != domain.MCPConnected || s.StatusReason != "" {
		t.Errorf("github = %+v, want connected with no stale error", s)
	}
	if n := len(changesOfKind(t, svc, domain.ContextChangeMCPFailed)); n != 1 {
		t.Errorf("mcp_failed changes = %d", n)
	}
	if n := len(changesOfKind(t, svc, domain.ContextChangeMCPConnected)); n != 1 {
		t.Errorf("mcp_connected changes = %d", n)
	}
}

func TestSessionContextInitMore_AConnectedServerThatThenFailsIsFailed(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	applyInit(t, svc, initOf([3]string{"github", "connected", ""}), nil)
	if !applyInit(t, svc, initOf([3]string{"github", "failed", "timeout"}), nil) {
		t.Error("connected → failed reported no change")
	}
	if s := githubOf(t, svc); s.Status != domain.MCPFailed || s.StatusReason != "timeout" {
		t.Errorf("github = %+v", s)
	}
}

func TestSessionContextInitMore_ARepeatedFailureAddsNoChangeButANewErrorDoes(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	report := initOf([3]string{"github", "failed", "401"})
	applyInit(t, svc, report, nil)
	if applyInit(t, svc, report, nil) {
		t.Error("the same failure again reported a change")
	}
	if n := len(changesOfKind(t, svc, domain.ContextChangeMCPFailed)); n != 1 {
		t.Errorf("mcp_failed changes = %d, want 1", n)
	}
	if !applyInit(t, svc, initOf([3]string{"github", "failed", "403 now"}), nil) {
		t.Error("a new error text reported no change")
	}
	if s := githubOf(t, svc); s.StatusReason != "403 now" {
		t.Errorf("reason = %q", s.StatusReason)
	}
}

func TestSessionContextInitMore_AFailureWithNoTextIsStillAFailure(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	applyInit(t, svc, initOf([3]string{"github", "failed", ""}), nil)
	s := githubOf(t, svc)
	if s.Status != domain.MCPFailed || s.StatusReason != "" {
		t.Errorf("github = %+v", s)
	}
	c := changesOfKind(t, svc, domain.ContextChangeMCPFailed)
	if len(c) != 1 || strings.HasSuffix(c[0].Detail, ": ") {
		t.Errorf("changes = %+v", c)
	}
}

func TestSessionContextInitMore_APendingServerAfterAFailureKeepsWhatClaudeSaidBefore(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	applyInit(t, svc, initOf([3]string{"github", "failed", "401"}), nil)
	if applyInit(t, svc, initOf([3]string{"github", "pending", ""}), nil) {
		t.Error("an unrecognised status reported a change")
	}
	if s := githubOf(t, svc); s.Status != domain.MCPFailed {
		t.Errorf("github = %+v", s)
	}
}

func TestSessionContextInitMore_AServerTheSessionNeverGotIsIgnored(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	if applyInit(t, svc, initOf([3]string{"ghost", "failed", "x"}, [3]string{"claude_ai_Gmail", "connected", ""}), nil) {
		t.Error("servers this session does not have reported a change")
	}
	e := contextOf(t, svc, "s1").Entries[0]
	if len(e.MCPServers) != 3 {
		t.Errorf("servers = %+v", e.MCPServers)
	}
	if n := len(contextOf(t, svc, "s1").Changes); n != 1 { // the entry_added only
		t.Errorf("changes = %d", n)
	}
}

func TestSessionContextInitMore_AReportThatIsNotAnObjectIsPassedOver(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	for _, raw := range []string{`not json`, `"init"`, `[1,2]`, `null`, ``, `{"mcp_servers":"none"}`, `{"mcp_servers":[{"name":1}]}`} {
		ok, err := svc.applyInitReport(context.Background(), "s1", json.RawMessage(raw), nil, time.Now())
		if ok || err != nil {
			t.Errorf("%q: changed=%v err=%v", raw, ok, err)
		}
	}
	if s := githubOf(t, svc); s.Status != domain.MCPUnknown {
		t.Errorf("github = %+v", s)
	}
}

func TestSessionContextInitMore_OnlyTheNewestEntryTakesTheReport(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	second := serversEntry()
	second.ID, second.Kind, second.At = "e2", domain.ContextEntryResume, time.Unix(20, 0)
	storeEntry(t, svc, second)
	applyInit(t, svc, initOf([3]string{"github", "connected", ""}), nil)
	v := contextOf(t, svc, "s1")
	if s, _ := serverByName(v.Entries[0], "github"); s.Status != domain.MCPUnknown {
		t.Errorf("the older entry was changed: %+v", s)
	}
	if s, _ := serverByName(v.Entries[1], "github"); s.Status != domain.MCPConnected {
		t.Errorf("the newest entry: %+v", s)
	}
}

func TestSessionContextInitMore_ClaudesVersionIsKeptWithoutAChangeAndOnlyWhenItDiffers(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	if applyInit(t, svc, `{"claude_code_version":"2.1.0"}`, nil) {
		t.Error("a version alone reported a change")
	}
	if v := contextOf(t, svc, "s1").Entries[0].Facts.ClaudeVersion; v != "2.1.0" {
		t.Errorf("version = %q", v)
	}
	applyInit(t, svc, `{"claude_code_version":"2.2.0"}`, nil)
	if v := contextOf(t, svc, "s1").Entries[0].Facts.ClaudeVersion; v != "2.2.0" {
		t.Errorf("version = %q", v)
	}
	applyInit(t, svc, `{"mcp_servers":[]}`, nil) // says no version: keep the one known
	if v := contextOf(t, svc, "s1").Entries[0].Facts.ClaudeVersion; v != "2.2.0" {
		t.Errorf("an init without a version erased it: %q", v)
	}
}

func TestSessionContextInitMore_AnErrorIsOneBoundedLineOnAUTF8Boundary(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	text := "line one\n\n   line   two\t" + strings.Repeat("é", 500)
	raw, _ := json.Marshal(map[string]any{"mcp_servers": []map[string]string{{"name": "github", "status": "failed", "error": text}}})
	applyInit(t, svc, string(raw), nil)
	r := githubOf(t, svc).StatusReason
	if strings.ContainsAny(r, "\n\t") || strings.Contains(r, "  ") || !strings.HasPrefix(r, "line one line two ") {
		t.Errorf("reason is not one tidy line: %q", r)
	}
	if len(r) > maxStatusReason || !utf8.ValidString(r) || len(r) < maxStatusReason-3 {
		t.Errorf("reason = %d bytes, valid=%v", len(r), utf8.ValidString(r))
	}
}

func TestSessionContextInitMore_TheHeadlessInitEventIsReadOnceNoMatterHowOftenClaudeSaysIt(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	ev := llmkit.Event{Type: llmkit.EventSystem, Subtype: "init", At: time.Unix(77, 0),
		Raw: json.RawMessage(initOf([3]string{"task", "connected", ""}, [3]string{"github", "failed", "no"}))}
	svc.onInit("s1", ev)
	svc.onInit("s1", ev)
	svc.onInit("s1", ev)
	v := contextOf(t, svc, "s1")
	if n := len(changesOfKind(t, svc, domain.ContextChangeMCPConnected)); n != 1 {
		t.Errorf("mcp_connected = %d", n)
	}
	if n := len(changesOfKind(t, svc, domain.ContextChangeMCPFailed)); n != 1 {
		t.Errorf("mcp_failed = %d", n)
	}
	for _, c := range v.Changes {
		if c.Kind == domain.ContextChangeMCPFailed && !c.At.Equal(time.Unix(77, 0)) {
			t.Errorf("the change is stamped %v, not the event's time", c.At)
		}
	}
}

func TestSessionContextInitMore_AnInitEventWithNoTimeIsStampedNow(t *testing.T) {
	svc := contextService(t)
	storeEntry(t, svc, serversEntry())
	before := time.Now().Add(-time.Second)
	svc.onInit("s1", llmkit.Event{Type: llmkit.EventSystem, Subtype: "init", Raw: json.RawMessage(initOf([3]string{"task", "connected", ""}))})
	c := changesOfKind(t, svc, domain.ContextChangeMCPConnected)
	if len(c) != 1 || c[0].At.Before(before) {
		t.Errorf("changes = %+v", c)
	}
}

func TestSessionContextInitMore_AnInitForASessionThatEndedAndWasForgottenIsHarmless(t *testing.T) {
	svc := contextService(t)
	svc.onInit("nope", llmkit.Event{Type: llmkit.EventSystem, Subtype: "init", Raw: json.RawMessage(initOf([3]string{"task", "connected", ""}))})
	if c, _ := svc.Contexts.Load(context.Background(), "nope"); c != nil {
		t.Errorf("a context appeared: %+v", c)
	}
}

// A resumed or terminal session never reports: its servers must stay unknown
// whatever the other entries of the session say (contract C3).
func TestSessionContextInitMore_ATerminalSessionWithManyServersStaysUnknownInEveryEntry(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.Contexts = contextService(t).Contexts
	withLeakyProject(svc)
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	resumeAfterEnd(t, svc, sess.ID)
	v := contextOf(t, svc, sess.ID)
	if len(v.Entries) != 2 {
		t.Fatal(len(v.Entries))
	}
	for _, e := range v.Entries {
		if e.Kind == domain.ContextEntryStart && len(e.MCPServers) < 2 {
			t.Fatalf("expected several servers: %+v", e.MCPServers)
		}
		for _, s := range e.MCPServers {
			if s.Status != domain.MCPUnknown || s.StatusReason != domain.MCPStatusUnknownReason {
				t.Errorf("%s/%s = %q (%q)", e.ID, s.Name, s.Status, s.StatusReason)
			}
		}
		if e.Facts.ClaudeVersion != "" {
			t.Errorf("a terminal session got a claude version: %q", e.Facts.ClaudeVersion)
		}
	}
}
