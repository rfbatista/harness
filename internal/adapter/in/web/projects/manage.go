package projects

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"operators-mcp/internal/adapter/in/web/sessions"
	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

// Summary is a project at a glance, as the server's projects context counts
// it (contract "Harness server ↔ Web UI — project management" §1). It has
// the shape of ports.ProjectSummary.
type Summary struct {
	Project                                             *domain.Project
	RepositoryCount, OpenTaskCount, RunningSessionCount int
	LastActivityAt                                      *time.Time
}

// SummaryLister lists every project with its counts, sorted by name
// (ignoring case), then id. The projects context implements it; the pages
// never count on their own, so first paint and /api/list_project_summaries
// agree.
type SummaryLister interface {
	ListProjectSummaries(ctx context.Context) ([]Summary, error)
}

// ListHref is the projects list.
const ListHref = "/projects"

// SettingsHref is a project's settings page.
func SettingsHref(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/settings"
}

// ListView is what the projects list renders.
type ListView struct {
	Frame shell.Frame
	Rows  []SummaryRow
	// Total is the line under the list: "4 projects · 3 sessions running".
	Total string
	Seed  ListSeed
}

// SummaryRow is one project as listed.
type SummaryRow struct {
	ID, Name, RootDir  string
	State, Word, Meta  string
	Href, SettingsHref string
}

// ListSeed is what projectsListPage starts from: GET /api/list_project_summaries' answer.
type ListSeed struct {
	Summaries []SummaryDTO `json:"summaries"`
}

// SummaryDTO is a summary on the wire.
type SummaryDTO struct {
	Project             *domain.Project `json:"project"`
	RepositoryCount     int             `json:"repository_count"`
	OpenTaskCount       int             `json:"open_task_count"`
	RunningSessionCount int             `json:"running_session_count"`
	LastActivityAt      *time.Time      `json:"last_activity_at"`
}

// NewListView builds the projects list. Rows read the way
// web/src/modules/projects/presentation/view.js writes them;
// web/testdata/views/project-summary.json pins both.
func NewListView(frame shell.Frame, summaries []Summary, now time.Time) ListView {
	v := ListView{Frame: frame, Rows: []SummaryRow{}, Seed: ListSeed{Summaries: []SummaryDTO{}}}
	running := 0
	for _, s := range summaries {
		p := s.Project
		state, word := summaryStatus(s.RunningSessionCount)
		v.Rows = append(v.Rows, SummaryRow{
			ID: p.ID, Name: p.Name, RootDir: p.RootDir,
			State: state, Word: word, Meta: summaryMeta(s, now),
			Href: "/projects/" + url.PathEscape(p.ID), SettingsHref: SettingsHref(p.ID),
		})
		v.Seed.Summaries = append(v.Seed.Summaries, SummaryDTO{
			Project: p, RepositoryCount: s.RepositoryCount, OpenTaskCount: s.OpenTaskCount,
			RunningSessionCount: s.RunningSessionCount, LastActivityAt: s.LastActivityAt,
		})
		running += s.RunningSessionCount
	}
	v.Total = totalLine(len(summaries), running)
	return v
}

func summaryStatus(running int) (state, word string) {
	if running > 0 {
		return "running", strconv.Itoa(running) + " running"
	}
	return "idle", "idle"
}

func summaryMeta(s Summary, now time.Time) string {
	activity := "no activity"
	if s.LastActivityAt != nil {
		if ago := sessions.RelativeTime(*s.LastActivityAt, now); ago == "now" {
			activity = "active now"
		} else {
			activity = "active " + ago + " ago"
		}
	}
	return strings.Join([]string{
		countOrNone(s.RepositoryCount, "repo", "repos"),
		countOrNone(s.OpenTaskCount, "open task", "open tasks"),
		activity,
	}, " · ")
}

func totalLine(projects, running int) string {
	if projects == 0 {
		return "no projects"
	}
	line := countOrNone(projects, "project", "projects")
	if running > 0 {
		line += " · " + countOrNone(running, "session", "sessions") + " running"
	}
	return line
}

func countOrNone(n int, one, many string) string {
	switch n {
	case 0:
		return "no " + many
	case 1:
		return "1 " + one
	default:
		return strconv.Itoa(n) + " " + many
	}
}

// SettingsView is what a project's settings page renders.
type SettingsView struct {
	Frame            shell.Frame
	Project          *domain.Project
	RepositoriesHref string
	Repositories     []SettingsRepository
	Seed             SettingsSeed
}

// SettingsRepository is a repository, as a link to its env files.
type SettingsRepository struct{ Name, EnvHref string }

// SettingsSeed is what projectsSettingsPage starts from, in the API's shape.
type SettingsSeed struct {
	Project      *domain.Project      `json:"project"`
	Repositories []*domain.Repository `json:"repositories"`
}

// NewSettingsView builds a project's settings page.
func NewSettingsView(frame shell.Frame, project *domain.Project, repos []*domain.Repository) SettingsView {
	if repos == nil {
		repos = []*domain.Repository{}
	}
	seedProject := *project
	seedProject.Repositories = nil // the page reads them from the seed's own list
	v := SettingsView{
		Frame:            frame,
		Project:          &seedProject,
		RepositoriesHref: RepositoriesHref(project.ID),
		Repositories:     []SettingsRepository{},
		Seed:             SettingsSeed{Project: &seedProject, Repositories: repos},
	}
	for _, r := range repos {
		v.Repositories = append(v.Repositories, SettingsRepository{Name: repositoryName(r), EnvHref: EnvFilesHref(project.ID, r.ID)})
	}
	return v
}
