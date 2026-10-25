package catalogstore

import (
	"time"

	"github.com/rfbatista/harnesskit/mcpserver"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// MCPServers is the capabilities context's MCP server repository, over the
// catalog. It implements mcpserver.Store.
type MCPServers struct{ cat editor }

var _ mcpserver.Store = (*MCPServers)(nil)

// NewMCPServers is the MCP server repository over the catalog.
func NewMCPServers(cat ports.CatalogEditor) *MCPServers { return &MCPServers{cat: cat} }

// transportOf maps the catalog's transport names to the store's.
func transportOf(catalogTransport string) string {
	if catalogTransport == "http" {
		return mcpserver.TransportStreamableHTTP
	}
	return catalogTransport
}

func serverOf(s domain.CatalogMCPServer) *mcpserver.Server {
	return &mcpserver.Server{
		ID: s.Name, Name: s.Name, Transport: transportOf(s.Transport), Command: s.Command,
		Args: s.Args, URL: s.URL, Env: s.Env, Headers: s.Headers,
	}
}

// Get returns the server named id, or nil.
func (r *MCPServers) Get(id string) *mcpserver.Server {
	for _, s := range r.List() {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// List returns every server.
func (r *MCPServers) List() []*mcpserver.Server {
	view, err := r.cat.ReadCatalog()
	if err != nil {
		return nil
	}
	out := make([]*mcpserver.Server, 0, len(view.MCPServers))
	for _, s := range view.MCPServers {
		out = append(out, serverOf(s))
	}
	return out
}

func inputOf(in mcpserver.Input) domain.CatalogServerInput {
	transport := "stdio"
	switch mcpserver.NormalizeTransport(in.Transport) {
	case mcpserver.TransportSSE:
		transport = "sse"
	case mcpserver.TransportStreamableHTTP:
		transport = "http"
	}
	return domain.CatalogServerInput{
		Name: in.Name, Transport: transport, Command: in.Command, Args: in.Args,
		Env: in.Env, URL: in.URL, Headers: in.Headers,
	}
}

// Create adds a server. A description is not kept: the catalog has none.
func (r *MCPServers) Create(in mcpserver.Input) (*mcpserver.Server, error) {
	if _, err := r.cat.SaveCatalogMCPServer(domain.SaveCatalogMCPServer{Server: inputOf(in)}); err != nil {
		return nil, err
	}
	return r.Get(in.Name), nil
}

// Update replaces a server's configuration. Which agents and projects get it
// stays.
func (r *MCPServers) Update(id string, in mcpserver.Input) (*mcpserver.Server, error) {
	if r.Get(id) == nil {
		return nil, errNotFound("MCP_SERVER_NOT_FOUND", "MCP server "+id)
	}
	if _, err := r.cat.SaveCatalogMCPServer(domain.SaveCatalogMCPServer{PreviousName: id, Server: inputOf(in)}); err != nil {
		return nil, err
	}
	return r.Get(in.Name), nil
}

// UpdateProbeResult reports the result on the server. The catalog keeps no
// probe results, so nothing is stored.
func (r *MCPServers) UpdateProbeResult(id string, result mcpserver.ProbeResult) (*mcpserver.Server, error) {
	s := r.Get(id)
	if s == nil {
		return nil, errNotFound("MCP_SERVER_NOT_FOUND", "MCP server "+id)
	}
	now := time.Now()
	s.LastProbeAt, s.LastProbeStatus, s.LastProbeError = &now, result.Status, result.Error
	s.ToolCount, s.ResourceCount, s.PromptCount = result.ToolCount, result.ResourceCount, result.PromptCount
	return s, nil
}

// Delete removes a server and every reference to it.
func (r *MCPServers) Delete(id string) error {
	_, err := r.cat.DeleteCatalogMCPServer(domain.DeleteCatalogItem{Name: id})
	return err
}
