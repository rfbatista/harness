//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// The product owner runs as a catalog agent with a project prompt (child 7).
// These tests read what the server handed to claude at each launch: the
// command line, the MCP config, and the skills of the plugin directory, which
// the stand-in claude copies out before the server removes it.

// fakeRecordingClaude is the stand-in claude that also keeps, per process, the
// MCP config it was given and a listing of each plugin dir's skills.
func fakeRecordingClaude() string {
	hook := `echo "$*" > "$dir/launch.$$"
prev=""
for a in "$@"; do
  if [ "$prev" = "--mcp-config" ]; then cp "$a" "$dir/mcp.$$" 2>/dev/null; fi
  if [ "$prev" = "--plugin-dir" ]; then (cd "$a" && find . -type f | sort) >> "$dir/skills.$$" 2>/dev/null; fi
  prev="$a"
done
`
	return strings.Replace(fakeHeadlessClaude, `echo "$*" > "$dir/launch.$$"`+"\n", hook, 1)
}

// newExtensionE2E is newPOE2E over the recording stand-in claude.
func newExtensionE2E(t *testing.T) *poE2E {
	t.Helper()
	e := newPOE2E(t)
	if err := os.WriteFile(filepath.Join(e.fakeDir, "claude"), []byte(fakeRecordingClaude()), 0o755); err != nil {
		t.Fatal(err)
	}
	return e
}

// launchFiles reads the files named prefix.* the stand-in wrote, one per process.
func (e *poE2E) launchFiles(prefix string) []string {
	e.t.Helper()
	files, _ := filepath.Glob(filepath.Join(e.fakeDir, prefix+".*"))
	var out []string
	for _, f := range files {
		b, _ := os.ReadFile(f)
		out = append(out, string(b))
	}
	return out
}

func (e *poE2E) catalogAgent(name string, skills []string, servers []string) *domain.Agent {
	e.t.Helper()
	var skillIDs, serverIDs []string
	for _, s := range skills {
		sk, err := e.cat.Capabilities.CreateSkill(domain.SkillInput{Name: s, Description: s + " skill", Files: []domain.SkillFile{{Path: "SKILL.md", Content: "# " + s}}})
		if err != nil {
			e.t.Fatal(err)
		}
		skillIDs = append(skillIDs, sk.ID)
	}
	for _, s := range servers {
		srv, err := e.cat.Capabilities.CreateMCPServer(domain.MCPServerInput{Name: s, Transport: domain.MCPTransportStdio, Command: s + "-bin"})
		if err != nil {
			e.t.Fatal(err)
		}
		serverIDs = append(serverIDs, srv.ID)
	}
	a, err := e.cat.Agents.CreateAgent(e.ctx, name, "", "", skillIDs, serverIDs)
	if err != nil {
		e.t.Fatal(err)
	}
	return a
}

func (e *poE2E) chooseAgent(projectID string, agentID string) {
	e.t.Helper()
	if status, code := e.post("update_product_owner", map[string]any{"project_id": projectID, "agent_id": agentID}, nil); status != 200 {
		e.t.Fatalf("choose agent = %d %s", status, code)
	}
}

// threadLaunch starts a thread of the project and answers what its one process
// was handed: the command line, the MCP config and the skill files.
type threadLaunch struct{ args, mcp, skills string }

// startedWith starts a thread in project, waits for its first turn, and reads
// the files its process wrote. launches counts the processes already seen.
func (e *poE2E) startedWith(project string, seen int) (threadID string, l threadLaunch) {
	e.t.Helper()
	var out struct {
		ThreadID string `json:"thread_id"`
	}
	if status, code := e.post("start_product_owner_thread", map[string]any{"project_id": project, "message": "hello"}, &out); status != 200 {
		e.t.Fatalf("start = %d %s", status, code)
	}
	e.waitTurn(out.ThreadID, "idle")
	return out.ThreadID, e.launchAt(seen)
}

// launchAt reads the n-th (from 0) process by start time.
func (e *poE2E) launchAt(n int) threadLaunch {
	e.t.Helper()
	args := e.launches()
	if len(args) <= n {
		e.t.Fatalf("only %d launches", len(args))
	}
	// The launch files are named by process id; match each by its command line.
	var l threadLaunch
	l.args = args[n]
	for _, pid := range e.pidsOf(l.args) {
		l.mcp = e.readFile("mcp." + pid)
		l.skills = e.readFile("skills." + pid)
	}
	return l
}

// pidsOf finds the process ids whose launch file holds args.
func (e *poE2E) pidsOf(args string) []string {
	files, _ := filepath.Glob(filepath.Join(e.fakeDir, "launch.*"))
	var out []string
	for _, f := range files {
		if b, _ := os.ReadFile(f); strings.TrimRight(string(b), "\n") == args {
			out = append(out, strings.TrimPrefix(filepath.Base(f), "launch."))
		}
	}
	return out
}

func (e *poE2E) readFile(name string) string {
	b, _ := os.ReadFile(filepath.Join(e.fakeDir, name))
	return string(b)
}

func (e *poE2E) otherProject(name string) string {
	e.t.Helper()
	dir := filepath.Join(e.t.TempDir(), "repo")
	gitInit(e.t, dir)
	p, err := e.cat.Projects.CreateProject(e.ctx, name, dir)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.cat.Projects.CreateRepository(e.ctx, p.ID, name, "", "https://example.com/"+name+".git", dir); err != nil {
		e.t.Fatal(err)
	}
	return p.ID
}

func TestAThreadRunsAsTheProjectsChosenAgentWithItsSkillsAndMcpServers(t *testing.T) {
	e := newExtensionE2E(t)
	a := e.catalogAgent("product-manager", []string{"roadmap"}, []string{"linear", "notion"})

	// Nothing chosen: plain claude, none of the agent's tools.
	_, plain := e.startedWith(e.project, 0)
	if strings.Contains(plain.skills, "roadmap") || strings.Contains(plain.mcp, "linear") {
		t.Errorf("no agent chosen, yet the launch has its tools: skills %q mcp %s", plain.skills, plain.mcp)
	}

	e.chooseAgent(e.project, a.ID)
	_, got := e.startedWith(e.project, 1)
	if !strings.Contains(got.skills, "skills/roadmap/SKILL.md") {
		t.Errorf("skills = %q, want the agent's roadmap skill", got.skills)
	}
	for _, want := range []string{`"linear"`, `"notion"`, `"task"`} {
		if !strings.Contains(got.mcp, want) {
			t.Errorf("mcp config lacks %s: %s", want, got.mcp)
		}
	}
	if !strings.Contains(got.args, "mcp__task__list_project_tasks") {
		t.Errorf("the product owner's task tools are missing: %.200s", got.args)
	}
}

func TestASkillAddedToTheAgentReachesTheNextThreadAndNotTheEarlierOne(t *testing.T) {
	e := newExtensionE2E(t)
	a := e.catalogAgent("product-manager", []string{"roadmap"}, nil)
	e.chooseAgent(e.project, a.ID)
	_, first := e.startedWith(e.project, 0)

	extra, err := e.cat.Capabilities.CreateSkill(domain.SkillInput{Name: "release-notes", Description: "notes", Files: []domain.SkillFile{{Path: "SKILL.md", Content: "# n"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.cat.Agents.UpdateAgent(e.ctx, a.ID, a.Name, a.Description, "", append(append([]string(nil), a.SkillIDs...), extra.ID), a.MCPServerIDs); err != nil {
		t.Fatal(err)
	}
	_, second := e.startedWith(e.project, 1)
	if !strings.Contains(second.skills, "release-notes") || !strings.Contains(second.skills, "roadmap") {
		t.Errorf("the next thread's skills = %q", second.skills)
	}
	if strings.Contains(first.skills, "release-notes") {
		t.Errorf("the earlier thread's launch changed: %q", first.skills)
	}
}

func TestAServerTheProjectBansIsNotGivenEvenWhenTheAgentListsItAndTaskIsNeverBanned(t *testing.T) {
	e := newExtensionE2E(t)
	a := e.catalogAgent("product-manager", nil, []string{"linear", "notion"})
	e.chooseAgent(e.project, a.ID)
	if _, err := e.cat.Projects.SetProjectMCPSettings(e.ctx, e.project, domain.ProjectMCPSettings{Banned: []string{"notion"}}); err != nil {
		t.Fatal(err)
	}
	_, got := e.startedWith(e.project, 0)
	if strings.Contains(got.mcp, `"notion"`) || !strings.Contains(got.mcp, `"linear"`) {
		t.Errorf("mcp config = %s, want linear and not the banned notion", got.mcp)
	}

	// The built-in task server is outside the catalog: banning its name changes nothing.
	_, _ = e.cat.Projects.SetProjectMCPSettings(e.ctx, e.project, domain.ProjectMCPSettings{Banned: []string{"notion", "task"}})
	_, again := e.startedWith(e.project, 1)
	if !strings.Contains(again.mcp, `"task"`) {
		t.Errorf("the task server is gone: %s", again.mcp)
	}
}

func TestAnAgentThatListsTheServicesServerNeverGivesItToAThread(t *testing.T) {
	e := newExtensionE2E(t)
	a, err := e.cat.Agents.CreateAgent(e.ctx, "ops-pm", "", "", nil, []string{"services"})
	if err != nil {
		t.Skipf("an agent cannot list the built-in services server here: %v", err)
	}
	e.chooseAgent(e.project, a.ID)
	_, got := e.startedWith(e.project, 0)
	if strings.Contains(got.mcp, "services") || strings.Contains(got.args, "mcp__services__") {
		t.Errorf("the thread got the services server: %s", got.mcp)
	}
}

func TestTwoProjectsKeepTheirOwnAgentAndThreadsStartAsEach(t *testing.T) {
	e := newExtensionE2E(t)
	other := e.otherProject("other")
	a := e.catalogAgent("alpha", []string{"alpha-skill"}, []string{"alpha-srv"})
	b := e.catalogAgent("beta", []string{"beta-skill"}, []string{"beta-srv"})
	e.chooseAgent(e.project, a.ID)
	e.chooseAgent(other, b.ID)
	_, one := e.startedWith(e.project, 0)
	_, two := e.startedWith(other, 1)
	if !strings.Contains(one.skills, "alpha-skill") || strings.Contains(one.skills, "beta-skill") || !strings.Contains(one.mcp, "alpha-srv") || strings.Contains(one.mcp, "beta-srv") {
		t.Errorf("first project: skills %q mcp %s", one.skills, one.mcp)
	}
	if !strings.Contains(two.skills, "beta-skill") || strings.Contains(two.skills, "alpha-skill") || !strings.Contains(two.mcp, "beta-srv") || strings.Contains(two.mcp, "alpha-srv") {
		t.Errorf("second project: skills %q mcp %s", two.skills, two.mcp)
	}
}

func TestARunningThreadKeepsItsAgentThroughASettingsChangeAndItsResume(t *testing.T) {
	e := newExtensionE2E(t)
	e.po.SetIdle(150 * time.Millisecond)
	a := e.catalogAgent("alpha", []string{"alpha-skill"}, []string{"alpha-srv"})
	b := e.catalogAgent("beta", []string{"beta-skill"}, []string{"beta-srv"})
	e.chooseAgent(e.project, a.ID)
	id, _ := e.startedWith(e.project, 0)
	th := e.thread(id)
	eventuallyPO(t, "the idle process to end", func() string {
		if e.orch.ProductOwnerRunning(th.Thread.SessionID) {
			return "still running"
		}
		return ""
	})
	e.chooseAgent(e.project, b.ID)
	if status, code := e.send(id, "again"); status != 200 {
		t.Fatalf("send = %d %s", status, code)
	}
	e.waitTurn(id, "idle")
	resumed := e.launchAt(1)
	if !strings.Contains(resumed.mcp, "alpha-srv") || strings.Contains(resumed.mcp, "beta-srv") || !strings.Contains(resumed.skills, "alpha-skill") {
		t.Errorf("the resume changed agent: skills %q mcp %s", resumed.skills, resumed.mcp)
	}
	_, fresh := e.startedWith(e.project, 2)
	if !strings.Contains(fresh.mcp, "beta-srv") || strings.Contains(fresh.mcp, "alpha-srv") {
		t.Errorf("the new thread is not the new agent: %s", fresh.mcp)
	}
}

func TestAnAgentTheProjectHasNotEnabledStartsNothing(t *testing.T) {
	e := newExtensionE2E(t)
	a := e.catalogAgent("alpha", nil, nil)
	e.catalogAgent("beta", nil, nil)
	e.chooseAgent(e.project, a.ID)
	if _, err := e.cat.Projects.SetProjectAgents(e.ctx, e.project, domain.AgentsSelected, []string{"beta"}); err != nil {
		t.Fatal(err)
	}
	status, code := e.post("start_product_owner_thread", map[string]any{"project_id": e.project, "message": "hello"}, nil)
	if status == 200 {
		time.Sleep(300 * time.Millisecond)
	}
	if n := len(e.launches()); n != 0 {
		t.Fatalf("%d processes started, want none (start answered %d %s)", n, status, code)
	}
	if status == 200 || code == "" {
		t.Errorf("start = %d %q, want a coded refusal", status, code)
	}
}

func TestAThreadContextEntryListsTheAgentItsSkillsAndItsServers(t *testing.T) {
	e := newExtensionE2E(t)
	a := e.catalogAgent("product-manager", []string{"roadmap"}, []string{"linear"})
	e.chooseAgent(e.project, a.ID)
	id, _ := e.startedWith(e.project, 0)
	view, err := e.orch.SessionContext(e.ctx, e.thread(id).Thread.SessionID)
	if err != nil || len(view.Entries) != 1 {
		t.Fatalf("entries = %+v, %v", view.Entries, err)
	}
	entry := view.Entries[0]
	if entry.Facts.Agent != "product-manager" {
		t.Errorf("agent fact = %q", entry.Facts.Agent)
	}
	var skill, server bool
	for _, s := range entry.Skills {
		skill = skill || s.Name == "roadmap"
	}
	for _, s := range entry.MCPServers {
		server = server || s.Name == "linear"
	}
	if !skill || !server {
		t.Errorf("skills %+v servers %+v, want roadmap and linear", entry.Skills, entry.MCPServers)
	}
}
