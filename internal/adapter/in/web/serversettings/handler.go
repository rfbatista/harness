// Package serversettings is the Server page, /server: where the harness
// keeps its data. It is read-only: the data dir is set when the server
// starts, with -data.dir or HARNESS_DATA_DIR.
package serversettings

import (
	"net/http"
	"path/filepath"
	"strconv"

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
	v := NewView(h.Paths)
	read := func(string) string { return "" }
	v.MemoryBudgetBytes = domain.DefaultMemoryBudgetBytes
	if h.Settings != nil {
		v.AgentsMoveDocuments = domain.AgentsMoveDocuments(h.Settings.Setting(domain.SettingAgentsMoveDocuments))
		read = h.Settings.Setting
		v.MemoryBudgetBytes, _ = domain.ResolveMemoryBudget("", "", h.Settings.Setting(domain.SettingMemoryBudget))
	}
	v.BranchBlocks = domain.BranchBlocks(read)
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
	// BranchBlocks is the branch block read: one switch per session mode.
	BranchBlocks []domain.BranchBlock
	// MemoryBudgetBytes is the global orçamento de memória da sessão: the
	// stored value, or 4 GB while it is unset or invalid.
	MemoryBudgetBytes int64
}

// MemoryBudgetGB is the orçamento in GB as the field shows it: 4, 2.5.
func (v View) MemoryBudgetGB() string {
	return strconv.FormatFloat(float64(v.MemoryBudgetBytes)/(1<<30), 'f', -1, 64)
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
