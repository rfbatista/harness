package agentcatalog

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/rfbatista/harnesskit/mcpserver"

	"operators-mcp/internal/domain"
)

// serverTransport is the transport of a saved server: stdio, http or sse.
func serverTransport(s domain.CatalogServerInput) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s.Transport)) {
	case "", "stdio":
		return "stdio", nil
	case "http", "streamable-http", "streamable_http":
		return "http", nil
	case "sse":
		return "sse", nil
	}
	return "", invalid("transport", "transport is stdio, http or sse")
}

// checkServer validates a server's shape. The name is checked by the caller.
func checkServer(s domain.CatalogServerInput) (string, error) {
	transport, err := serverTransport(s)
	if err != nil {
		return "", err
	}
	if transport == "stdio" && strings.TrimSpace(s.Command) == "" {
		return "", invalid("command", "a stdio server needs a command")
	}
	if transport != "stdio" && strings.TrimSpace(s.URL) == "" {
		return "", invalid("url", "an "+transport+" server needs a url")
	}
	for k := range s.Env {
		if strings.TrimSpace(k) == "" {
			return "", invalid("env", "an environment variable needs a name")
		}
	}
	for k := range s.Headers {
		if strings.TrimSpace(k) == "" {
			return "", invalid("headers", "a header needs a name")
		}
	}
	return transport, nil
}

// mapNode is an object of strings. Keys the old object had keep their order;
// new keys follow, sorted.
func mapNode(old *node, m map[string]string) *node {
	n := newObject()
	for _, k := range old.keys() {
		if v, ok := m[k]; ok {
			n.set(k, scalarOf(v))
		}
	}
	var fresh []string
	for k := range m {
		if n.get(k) == nil {
			fresh = append(fresh, k)
		}
	}
	sort.Strings(fresh)
	for _, k := range fresh {
		n.set(k, scalarOf(m[k]))
	}
	return n
}

// serverNode is the mcp-servers.json entry for a server. A key the form does
// not own stays as it was.
func serverNode(old *node, s domain.CatalogServerInput, transport string) *node {
	n := old
	if n == nil {
		n = newObject()
	}
	setOrDrop := func(key string, v *node, keep bool) {
		if keep {
			n.set(key, v)
		} else {
			n.remove(key)
		}
	}
	if transport == "stdio" {
		n.remove("type")
		n.remove("url")
		n.remove("headers")
		n.set("command", scalarOf(s.Command))
		setOrDrop("args", stringArray(s.Args), len(s.Args) > 0)
		setOrDrop("env", mapNode(n.get("env"), s.Env), len(s.Env) > 0)
		return n
	}
	n.remove("command")
	n.remove("args")
	n.remove("env")
	n.set("type", scalarOf(transport))
	n.set("url", scalarOf(s.URL))
	setOrDrop("headers", mapNode(n.get("headers"), s.Headers), len(s.Headers) > 0)
	return n
}

// SaveCatalogMCPServer creates, updates or renames an MCP server and sets
// which agents and projects get it.
func (c *Catalog) SaveCatalogMCPServer(in domain.SaveCatalogMCPServer) (*domain.CatalogWriteResult, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	t, err := c.begin()
	if err != nil {
		return nil, err
	}
	s := in.Server
	if !serverNameRe.MatchString(s.Name) {
		return nil, invalid("name", "an MCP server name is letters, digits, dashes and underscores, up to 64")
	}
	transport, err := checkServer(s)
	if err != nil {
		return nil, err
	}
	servers := t.serversMap()
	prev := in.PreviousName
	if prev != "" && servers.get(prev) == nil {
		return nil, &domain.StructuredError{Code: "MCP_SERVER_NOT_FOUND", Message: "MCP server " + prev + " not found"}
	}
	if (prev == "" || prev != s.Name) && servers.get(s.Name) != nil {
		return nil, nameTaken("MCP server", s.Name, func(n string) bool { return servers.get(n) != nil })
	}

	// Who gets it: every named agent and project must exist.
	var unknown []string
	for _, a := range in.Agents {
		if t.agentsMap().get(a) == nil {
			unknown = append(unknown, a)
		}
	}
	byID := map[string]*domain.Project{}
	for _, p := range t.projectLists() {
		byID[p.ID] = p
	}
	for _, id := range in.Projects {
		if byID[id] == nil {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		return nil, unknownRefs(unknown)
	}

	verb := "Saved"
	switch {
	case prev == "":
		verb = "Created"
		servers.set(s.Name, serverNode(nil, s, transport))
	case prev == s.Name:
		serverNode(servers.get(prev), s, transport)
	default:
		servers.rename(prev, s.Name)
		serverNode(servers.get(s.Name), s, transport)
		refs := 0
		for _, m := range t.agentsMap().members {
			if list := m.val.get("mcp_servers"); list != nil && setMembership(list, prev, false) {
				list.items = append(list.items, scalarOf(s.Name))
				refs++
			}
		}
		for _, p := range t.projectLists() {
			cur := t.currentProjectNames(p)
			if contains(cur, prev) {
				t.setProject(p, replaceName(cur, prev, s.Name))
				refs++
			}
		}
		verb = fmt.Sprintf("Renamed %s to %s (%d references updated) and saved", prev, s.Name, refs)
	}
	if in.Agents != nil {
		for _, m := range t.agentsMap().members {
			wanted := contains(in.Agents, m.key)
			list := m.val.get("mcp_servers")
			if list == nil {
				if !wanted {
					continue
				}
				list = newArray()
				m.val.set("mcp_servers", list)
			}
			setMembership(list, s.Name, wanted)
		}
	}
	if in.Projects != nil {
		for _, p := range t.projectLists() {
			cur := t.currentProjectNames(p)
			wanted := contains(in.Projects, p.ID)
			switch {
			case wanted && !contains(cur, s.Name):
				t.setProject(p, append(append([]string{}, cur...), s.Name))
			case !wanted && contains(cur, s.Name):
				t.setProject(p, without(cur, s.Name))
			}
		}
	}
	summary := fmt.Sprintf("%s %s — %s is on %d agents and %d projects", verb, mcpServersFile, s.Name,
		t.serverAgentCount(s.Name), t.serverProjectCount(s.Name))
	return t.commit(in.CatalogWriteOptions, summary)
}

func (t *txn) serverAgentCount(name string) int {
	n := 0
	for _, m := range t.agentsMap().members {
		if contains(m.val.get("mcp_servers").strs(), name) {
			n++
		}
	}
	return n
}

func (t *txn) serverProjectCount(name string) int {
	n := 0
	for _, p := range t.projectLists() {
		if contains(t.currentProjectNames(p), name) {
			n++
		}
	}
	return n
}

func replaceName(list []string, from, to string) []string {
	out := make([]string, 0, len(list))
	for _, x := range list {
		if x == from {
			x = to
		}
		out = append(out, x)
	}
	return out
}

func without(list []string, drop string) []string {
	out := make([]string, 0, len(list))
	for _, x := range list {
		if x != drop {
			out = append(out, x)
		}
	}
	return out
}

// DeleteCatalogMCPServer removes an MCP server and every agent and project
// reference to it.
func (c *Catalog) DeleteCatalogMCPServer(in domain.DeleteCatalogItem) (*domain.CatalogWriteResult, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	t, err := c.begin()
	if err != nil {
		return nil, err
	}
	if t.serversMap().get(in.Name) == nil {
		return nil, &domain.StructuredError{Code: "MCP_SERVER_NOT_FOUND", Message: "MCP server " + in.Name + " not found"}
	}
	t.serversMap().remove(in.Name)
	refs := 0
	for _, m := range t.agentsMap().members {
		if list := m.val.get("mcp_servers"); list != nil && setMembership(list, in.Name, false) {
			refs++
		}
	}
	for _, p := range t.projectLists() {
		if cur := t.currentProjectNames(p); contains(cur, in.Name) {
			t.setProject(p, without(cur, in.Name))
			refs++
		}
	}
	return t.commit(in.CatalogWriteOptions, fmt.Sprintf("Deleted %s from %s (%d references removed)", in.Name, mcpServersFile, refs))
}

// expand replaces each ${VAR} with the variable's value. A variable that is
// not set becomes empty. The values it used are returned so a message can be
// cleaned of them.
func (c *Catalog) expand(s string, used map[string]string) string {
	return envRefRe.ReplaceAllStringFunc(s, func(m string) string {
		name := envRefRe.FindStringSubmatch(m)[1]
		v, _ := c.lookupEnv(name)
		// A value of one or two characters would garble every message that
		// has the letter, so those are not tracked for scrubbing.
		if len(v) >= 4 {
			used[v] = name
		}
		return v
	})
}

// TestCatalogMCPServer probes an unsaved server once. ${VAR} is expanded for
// this call alone: the values are never stored, returned or logged.
func (c *Catalog) TestCatalogMCPServer(ctx context.Context, in domain.TestCatalogMCPServer) (*domain.CatalogProbeResult, error) {
	s := in.Server
	transport, err := checkServer(s)
	if err != nil {
		return nil, err
	}
	if c.prober == nil {
		return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "testing MCP servers is not available"}
	}
	used := map[string]string{}
	srv := mcpserver.Server{Name: s.Name, Command: c.expand(s.Command, used), URL: c.expand(s.URL, used)}
	if srv.Name == "" {
		srv.Name = "test"
	}
	switch transport {
	case "stdio":
		srv.Transport = mcpserver.TransportStdio
	case "sse":
		srv.Transport = mcpserver.TransportSSE
	default:
		srv.Transport = mcpserver.TransportStreamableHTTP
	}
	for _, a := range s.Args {
		srv.Args = append(srv.Args, c.expand(a, used))
	}
	if len(s.Env) > 0 {
		srv.Env = map[string]string{}
		for k, v := range s.Env {
			srv.Env[k] = c.expand(v, used)
		}
	}
	if len(s.Headers) > 0 {
		srv.Headers = map[string]string{}
		for k, v := range s.Headers {
			srv.Headers[k] = c.expand(v, used)
		}
	}

	res, perr := c.prober.Probe(ctx, srv)
	if perr != nil && res.Error == "" {
		res.Error = perr.Error()
	}
	out := &domain.CatalogProbeResult{Probe: domain.CatalogProbe{Status: res.Status, Tools: []domain.CatalogProbeTool{}}}
	if out.Probe.Status == "" {
		out.Probe.Status = "error"
	}
	for _, n := range res.ToolNames {
		// The probe lists tool names only.
		out.Probe.Tools = append(out.Probe.Tools, domain.CatalogProbeTool{Name: n})
	}
	if out.Probe.Status != "ok" {
		out.Probe.Error = scrub(res.Error, used)
		out.Probe.Hint = c.hint(s, out.Probe.Error)
	}
	return out, nil
}

// scrub puts ${NAME} back where a variable's value stands in text.
func scrub(text string, used map[string]string) string {
	// Longest values first, so one value inside another is not cut in two.
	vals := make([]string, 0, len(used))
	for v := range used {
		vals = append(vals, v)
	}
	sort.Slice(vals, func(i, j int) bool { return len(vals[i]) > len(vals[j]) })
	for _, v := range vals {
		text = strings.ReplaceAll(text, v, "${"+used[v]+"}")
	}
	return text
}

// hint is what to try when a probe failed.
func (c *Catalog) hint(s domain.CatalogServerInput, errText string) string {
	var unset []string
	seen := map[string]bool{}
	scan := func(text string) {
		for _, m := range envRefRe.FindAllStringSubmatch(text, -1) {
			if _, ok := c.lookupEnv(m[1]); !ok && !seen[m[1]] {
				seen[m[1]] = true
				unset = append(unset, m[1])
			}
		}
	}
	scan(s.Command)
	scan(s.URL)
	for _, a := range s.Args {
		scan(a)
	}
	for _, v := range s.Env {
		scan(v)
	}
	for _, v := range s.Headers {
		scan(v)
	}
	sort.Strings(unset)
	low := strings.ToLower(errText)
	switch {
	case len(unset) > 0:
		return strings.Join(unset, ", ") + " is not set in the harness server's environment."
	case strings.Contains(low, "executable file not found") || strings.Contains(low, "no such file"):
		return "The command was not found on the harness server. Check that " + s.Command + " is installed and on its PATH."
	case strings.Contains(low, "deadline exceeded") || strings.Contains(low, "timeout") || strings.Contains(low, "timed out"):
		return "The server did not answer in time."
	case strings.Contains(low, "connection refused") || strings.Contains(low, "no such host"):
		return "Nothing answered at that address. Check the url."
	case strings.Contains(low, "401") || strings.Contains(low, "403") || strings.Contains(low, "unauthorized"):
		return "The server refused the credentials. Check the token."
	}
	return ""
}
