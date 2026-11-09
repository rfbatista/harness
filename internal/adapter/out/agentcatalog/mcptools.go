package agentcatalog

import (
	"context"
	"time"

	"operators-mcp/internal/domain"
)

// Keys of a server's entry in mcp-tools.json.
const (
	toolsServersKey = "servers"
	knownToolsKey   = "known_tools"
	probedAtKey     = "probed_at"
	disabledKey     = "disabled_tools"
)

// toolEntries is the "servers" object of mcp-tools.json in the transaction,
// created on first use.
func (t *txn) toolEntries() *node { return t.object(t.tools, toolsServersKey) }

// toolEntry is a server's entry, created empty when missing.
func (t *txn) toolEntry(name string) *node {
	entries := t.toolEntries()
	if e := entries.get(name); e != nil && e.kind == 'o' {
		return e
	}
	e := newObject()
	entries.set(name, e)
	return e
}

// dropEmptyToolEntry keeps the file free of servers with nothing to say.
func (t *txn) dropEmptyToolEntry(name string) {
	if e := t.toolEntries().get(name); e != nil && len(e.members) == 0 {
		t.toolEntries().remove(name)
	}
}

// renameTools moves a server's tool data to its new name.
func (t *txn) renameTools(from, to string) {
	if t.toolEntries().get(from) != nil {
		t.toolEntries().remove(to)
		t.toolEntries().rename(from, to)
	}
}

// setDisabledTools stores the tool default: the disabled names, each once.
func (t *txn) setDisabledTools(name string, disabled []string) {
	var names []string
	for _, d := range disabled {
		if d != "" && !contains(names, d) {
			names = append(names, d)
		}
	}
	e := t.toolEntry(name)
	if len(names) == 0 {
		e.remove(disabledKey)
	} else {
		e.set(disabledKey, stringArray(names))
	}
	t.dropEmptyToolEntry(name)
}

// setKnownTools stores what a successful probe found.
func (t *txn) setKnownTools(name string, tools []string, at time.Time) {
	e := t.toolEntry(name)
	e.set(knownToolsKey, stringArray(tools))
	e.set(probedAtKey, scalarOf(at.UTC().Format(time.RFC3339)))
}

// withToolData fills a server's known tools, probe time and tool default
// from the tool file's entry for it.
func withToolData(srv *domain.CatalogMCPServer, entry *node) {
	srv.DisabledTools = nonNil(entry.get(disabledKey).strs())
	if known := entry.get(knownToolsKey); known != nil && known.kind == 'a' {
		srv.KnownTools = known.strs()
		if at := entry.get(probedAtKey).str(); at != "" {
			srv.ProbedAt = &at
		}
	}
}

// probeOutcome is a probe as the save and the refresh answer it, and the tool
// names to keep when it succeeded.
func (c *Catalog) probeOutcome(ctx context.Context, s domain.CatalogServerInput) (domain.CatalogProbeOutcome, []string, bool) {
	res, err := c.TestCatalogMCPServer(ctx, domain.TestCatalogMCPServer{Server: s})
	if err != nil {
		return domain.CatalogProbeOutcome{Status: "error", Error: err.Error()}, nil, false
	}
	out := domain.CatalogProbeOutcome{Status: res.Probe.Status, Error: res.Probe.Error, Hint: res.Probe.Hint}
	names := make([]string, 0, len(res.Probe.Tools))
	for _, tl := range res.Probe.Tools {
		names = append(names, tl.Name)
	}
	return out, names, res.Probe.Status == "ok"
}

// serverInput is a saved server as a probe takes it.
func (c *Catalog) serverInput(srv domain.CatalogMCPServer) domain.CatalogServerInput {
	return domain.CatalogServerInput{Name: srv.Name, Transport: srv.Transport, Command: srv.Command, Args: srv.Args, Env: srv.Env, URL: srv.URL, Headers: srv.Headers}
}

// RefreshCatalogMCPServerTools probes a saved server and stores the tools it
// found. It writes only mcp-tools.json and takes no base version.
func (c *Catalog) RefreshCatalogMCPServerTools(ctx context.Context, in domain.RefreshCatalogMCPServerTools) (*domain.CatalogMCPServerTools, error) {
	if in.Name == domain.ServicesServerName {
		return nil, domain.ErrServicesServerBuiltIn()
	}
	view, err := c.ReadCatalog()
	if err != nil {
		return nil, err
	}
	var saved *domain.CatalogMCPServer
	for i := range view.MCPServers {
		if view.MCPServers[i].Name == in.Name && !view.MCPServers[i].BuiltIn {
			saved = &view.MCPServers[i]
		}
	}
	if saved == nil {
		return nil, domain.NewCatalogError("UNKNOWN_REFERENCE", "these do not exist: "+in.Name, map[string]any{"names": []string{in.Name}})
	}
	outcome, names, ok := c.probeOutcome(ctx, c.serverInput(*saved))
	if ok {
		c.wmu.Lock()
		t, err := c.begin()
		if err == nil && t.serversMap().get(in.Name) != nil {
			t.setKnownTools(in.Name, names, time.Now())
			_, err = t.commit(domain.CatalogWriteOptions{Force: true}, "Refreshed the tools of "+in.Name)
		}
		c.wmu.Unlock()
		if err != nil {
			return nil, err
		}
	}
	view, err = c.ReadCatalog()
	if err != nil {
		return nil, err
	}
	for _, s := range view.MCPServers {
		if s.Name == in.Name {
			return &domain.CatalogMCPServerTools{Server: s, Probe: outcome}, nil
		}
	}
	return nil, domain.NewCatalogError("UNKNOWN_REFERENCE", "these do not exist: "+in.Name, map[string]any{"names": []string{in.Name}})
}
