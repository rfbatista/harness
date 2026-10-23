// The Agent isolation page, /agent-isolation: what an agent session runs
// with that is not the person's own: its credential, its claude, and the OS
// sandbox around it (ADR 0017-0019). The page is three independent sections,
// each a partial with its own view, so a section can be filled in without
// touching the others:
//
//	#credential  credential.go + credential.templ   (the agent credential)
//	#claude      claude.go + claude.templ           (the managed claude)
//	#sandbox     sandbox.go + sandbox.templ         (the switch and extra paths)
//
// To add to a section, extend its View and its partial; the page only places
// them.
package agentisolation

import (
	"net/http"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/ports"
)

// Href is the Agent isolation page.
const Href = "/agent-isolation"

// Handler serves GET /agent-isolation.
type Handler struct {
	// Sandbox is the person's sandbox switch and extra paths.
	Sandbox ports.SandboxSettings
	// Credential is the agent credential; nil shows it as not set.
	Credential ports.AgentCredential
	// OAuth is the Sign in with Claude flow; nil disables the button.
	OAuth ports.AgentOAuth
	// Driver is the platform's sandbox driver; a nil func or result is none.
	Driver func() ports.SandboxDriver
	// Claude and ClaudeBin are the managed claude and the binary agents
	// resolve to; nil shows none installed.
	Claude    ports.ManagedClaude
	ClaudeBin ports.ManagedClaudeBin
	Layout    shell.Layout
	Render    shell.Renderer
}

// View is what the page renders: one view per section.
type View struct {
	Credential CredentialView
	Claude     ClaudeView
	Sandbox    SandboxView
}

// Page renders the page.
func (h Handler) Page(w http.ResponseWriter, r *http.Request) error {
	frame, err := h.Layout(r.Context(), "Agent isolation", "", "")
	if err != nil {
		return err
	}
	frame.Top.OnAgentIsolation = true
	return h.Render(w, r, http.StatusOK, Page(frame, h.View()))
}

// View reads each section's state.
func (h Handler) View() View {
	return View{
		Credential: NewCredentialView(h.Credential, h.OAuth, h.ClaudeBin),
		Claude:     NewClaudeView(h.Claude, h.ClaudeBin),
		Sandbox:    NewSandboxView(h.Sandbox, h.Driver),
	}
}

// Blocks says whether something on the page stops agents from starting: the
// sandbox is on without a driver, the agent credential is not set, or the
// managed claude is not installed and not overridden. The top bar's link then
// carries the attention mark. A nil credential or bin is not enforced.
func Blocks(sandbox ports.SandboxSettings, driver func() ports.SandboxDriver, credential ports.AgentCredential, bin ports.ManagedClaudeBin) bool {
	if NewSandboxView(sandbox, driver).Blocks() {
		return true
	}
	if credential != nil && !credential.Status().Set {
		return true
	}
	return bin != nil && !bin.Installed()
}
