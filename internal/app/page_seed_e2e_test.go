//go:build !windows

package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The four-times project is the page-time benchmark's seeded project: the
// harness project at four times its size on 2026-11-03 (49 tasks), with the
// things the live server has and the page-load budget project leaves out.
// The live cost was measured by profiling the running server: it is the
// agent catalog on disk, read again and again for every session that runs as
// an agent. A budget project whose catalog is an empty folder cannot show it.
const (
	fourTimesTasks    = 200
	fourTimesSessions = 400
	fourTimesLive     = 20
	fourTimesRepos    = 3
	fourTimesOthers   = 7 // the person's other projects, listed in every frame
	// The catalog as a person who has used agents for a while keeps it.
	catalogSkills        = 300
	catalogFilesPerSkill = 40
	catalogAgents        = 8
	catalogSkillsPerAgnt = 20
	catalogMCPServers    = 6
)

// newFourTimesProject seeds the project and answers it.
func newFourTimesProject(t *testing.T) *budgetProject {
	t.Helper()
	b := newBudgetProject(t)
	b.agents = seedAgentCatalog(t, b.dir)
	b.live = fourTimesLive
	b.addTickets(fourTimesTasks - len(b.tickets))
	b.addRepositories(fourTimesRepos - 1)
	b.addSessions(fourTimesSessions)
	b.moveTasksBySessions(t)
	for i := range fourTimesOthers {
		other, err := b.cat.Projects.CreateProject(b.ctx, fmt.Sprintf("other-%d", i), b.repoDir)
		if err != nil {
			t.Fatal(err)
		}
		b.runSessionIn(t, other.ID, i)
		for j := range 10 {
			if _, err := b.plan.CreateTicket(b.ctx, other.ID, fmt.Sprintf("Task %d", j), "A task of another project.", "in_progress", ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	return b
}

// moveTasksBySessions moves most tasks to review as the session that works on
// them, as an agent's own status tool does. A page that lists tasks names the
// agent of the session behind each task's last move, so the live project's
// cost of naming agents is in the seed.
func (b *budgetProject) moveTasksBySessions(t *testing.T) {
	t.Helper()
	review := domain.TicketStatusReview
	for i, ticket := range b.tickets {
		if i%10 == 9 {
			continue // a few stay where the person left them
		}
		if _, err := b.plan.PatchTicket(b.ctx, ticket, ports.TicketPatch{Status: &review, BySessionID: fmt.Sprintf("s-%03d", i)}); err != nil {
			t.Fatal(err)
		}
	}
}

// runSessionIn starts a session that runs as a seeded agent in another
// project: the frame lists every project with the agents of its live sessions.
func (b *budgetProject) runSessionIn(t *testing.T, projectID string, n int) {
	t.Helper()
	if _, err := b.sessions.Create(&domain.Session{
		ID: fmt.Sprintf("other-s-%d", n), ProjectID: projectID, RepositoryID: b.repo, Task: "Work in another project",
		WorkingDir: b.repoDir, Status: domain.SessionRunning, AgentID: b.agents[n%len(b.agents)], Interactive: true, RunsOn: domain.RunnerServer, Mode: domain.SessionModeImplement,
	}); err != nil {
		t.Fatal(err)
	}
}

// seedAgentCatalog writes an agent catalog into the data directory's agents
// folder: skills with a few files each, agents that use them and MCP servers.
// It answers the agents' names.
func seedAgentCatalog(t *testing.T, dataDir string) []string {
	t.Helper()
	root := filepath.Join(dataDir, "agents")
	write := func(rel, content string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	skills := make([]string, catalogSkills)
	for i := range skills {
		skills[i] = fmt.Sprintf("skill-%03d", i)
		write("skills/"+skills[i]+"/SKILL.md", fmt.Sprintf("---\nname: %s\ndescription: What %s is for.\n---\n\nUse it well.\n", skills[i], skills[i]))
		for f := range catalogFilesPerSkill - 1 {
			write(fmt.Sprintf("skills/%s/references/ref-%d.md", skills[i], f), "A reference.\n")
		}
	}
	servers := make([]string, catalogMCPServers)
	var serverJSON []string
	for i := range servers {
		servers[i] = fmt.Sprintf("server-%d", i)
		serverJSON = append(serverJSON, fmt.Sprintf(`%q: {"command": "/bin/true", "args": ["--serve"]}`, servers[i]))
	}
	write("mcp-servers.json", "{"+strings.Join(serverJSON, ",")+"}\n")
	agents := make([]string, catalogAgents)
	var agentJSON []string
	for i := range agents {
		agents[i] = fmt.Sprintf("agent-%d", i)
		var own []string
		for j := range catalogSkillsPerAgnt {
			own = append(own, fmt.Sprintf("%q", skills[(i*catalogSkillsPerAgnt+j)%catalogSkills]))
		}
		agentJSON = append(agentJSON, fmt.Sprintf(`%q: {"description": "Agent %d.", "skills": [%s], "mcp_servers": [%q, %q]}`,
			agents[i], i, strings.Join(own, ","), servers[i%catalogMCPServers], servers[(i+1)%catalogMCPServers]))
	}
	write("agents.json", `{"agents": {`+strings.Join(agentJSON, ",")+`}}`+"\n")
	return agents
}

// The four-times project serves every project page.
func TestTheFourTimesProjectServesEveryProjectPage(t *testing.T) {
	b := newFourTimesProject(t)
	base := "/projects/" + b.project
	for name, path := range map[string]string{
		"board": base, "task page": base + "/tasks/" + b.tickets[0], "settings": base + "/settings",
		"documents": base + "/documents", "code": base + "/repositories/" + b.repo + "/code", "design": base + "/design",
		"usage": base + "/usage", "reviews": base + "/reviews",
	} {
		t.Logf("%s: idle %v; %s", name, b.idle(path), b.timing(path))
	}
}

// timing is the Server-Timing header of one GET of path.
func (b *budgetProject) timing(path string) string {
	b.t.Helper()
	rec := httptest.NewRecorder()
	b.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		b.t.Fatalf("GET %s: %d", path, rec.Code)
	}
	return rec.Header().Get("Server-Timing")
}
