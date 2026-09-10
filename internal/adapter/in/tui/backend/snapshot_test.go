package backend

import (
	"testing"

	"operators-mcp/internal/domain"
)

func TestLoadSnapshotReadsEveryCatalog(t *testing.T) {
	f := NewFake()
	f.Projects = []*domain.Project{{ID: "p1", Name: "one"}}
	f.Tickets = []*domain.Ticket{{ID: "t1", ProjectID: "p1"}}
	f.Sessions = []*domain.Session{{ID: "s1", ProjectID: "p1", TicketID: "t1", Status: domain.SessionRunning}}
	f.Agents = []*domain.Agent{{ID: "a1", Name: "reviewer"}}
	f.Skills = []*domain.Skill{{ID: "k1", Name: "tdd"}}
	f.MCPServers = []*domain.MCPServer{{ID: "m1", Name: "fs"}}
	f.Settings[domain.SettingWorkspacesRoot] = "/wt"

	s := Load(f)
	if len(s.Projects) != 1 || len(s.Tickets) != 1 || len(s.Sessions) != 1 || len(s.Agents) != 1 || len(s.Skills) != 1 || len(s.MCPServers) != 1 {
		t.Fatalf("snapshot incomplete: %+v", s)
	}
	if s.Settings[domain.SettingWorkspacesRoot] != "/wt" {
		t.Fatalf("settings missing: %+v", s.Settings)
	}
	if s.Project("p1") == nil || s.Project("nope") != nil {
		t.Fatal("Project lookup broken")
	}
	if s.ProjectName("p1") != "one" || s.ProjectName("nope") != "nope" {
		t.Fatalf("ProjectName: %q %q", s.ProjectName("p1"), s.ProjectName("nope"))
	}
}
