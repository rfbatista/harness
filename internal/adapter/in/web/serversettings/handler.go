// Package serversettings is the Server page, /server: where the harness
// keeps its data. It is read-only: the data dir is set when the server
// starts, with -data.dir or HARNESS_DATA_DIR.
package serversettings

import (
	"net/http"
	"path/filepath"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Href is the Server page.
const Href = "/server"

// Paths are where the running server keeps its data, absolute. DataDir is
// "" when the database is in memory and the data goes to temporary
// directories.
type Paths struct {
	DataDir string
	// Source says where DataDir came from, in words.
	Source   string
	Database string
}

// Handler serves GET /server.
type Handler struct {
	Paths Paths
	// Settings paints the switches; nil shows each at its default.
	Settings ports.SettingsReader
	Layout   shell.Layout
	Render   shell.Renderer
}

// Page renders the page.
func (h Handler) Page(w http.ResponseWriter, r *http.Request) error {
	frame, err := h.Layout(r.Context(), "Server", "", "")
	if err != nil {
		return err
	}
	frame.Top.OnServer = true
	v := NewView(h.Paths)
	if h.Settings != nil {
		v.AgentsMoveDocuments = domain.AgentsMoveDocuments(h.Settings.Setting(domain.SettingAgentsMoveDocuments))
	}
	return h.Render(w, r, http.StatusOK, Page(frame, v))
}

// View is what the page renders.
type View struct {
	// InMemory: there is no data dir, only temporary directories.
	InMemory bool
	DataDir  string
	Source   string
	// Rows are what the data dir holds, each with its path.
	Rows []Row
	// AgentsMoveDocuments is the stored "Agents move documents to project"
	// switch, off while unset.
	AgentsMoveDocuments bool
}

// Row is one thing the data dir holds.
type Row struct {
	Title, Path, Lead string
}

// NewView lists the data dir's contents under p.
func NewView(p Paths) View {
	if p.DataDir == "" {
		return View{InMemory: true, Rows: []Row{{Title: "Database", Path: p.Database, Lead: "In memory: nothing is kept when the server stops."}}}
	}
	in := func(name string) string { return filepath.Join(p.DataDir, name) }
	return View{
		DataDir: p.DataDir,
		Source:  p.Source,
		Rows: []Row{
			{Title: "Database", Path: p.Database, Lead: "Projects, tasks, sessions and settings."},
			{Title: "Token savers", Path: in("tools"), Lead: "The installed rtk, caveman and codegraph."},
			{Title: "Design assets", Path: in("artifacts"), Lead: "Copies of the project artifacts."},
			{Title: "Task resources", Path: in("task-resources"), Lead: "Files people uploaded to tasks."},
			{Title: "Git credentials", Path: in("git"), Lead: "Each repository's credentials, encrypted with secret.key beside it."},
		},
	}
}
