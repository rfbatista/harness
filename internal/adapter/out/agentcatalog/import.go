package agentcatalog

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/rfbatista/harnesskit/mcpserver"
	"github.com/rfbatista/harnesskit/skill"

	"operators-mcp/internal/domain"
)

// ImportSource is what the one-time catalog import reads: the repository's
// agents/ folder, and the catalog the database used to hold (ADR 0030).
type ImportSource struct {
	// RepoDir is the repository's agents/ folder: agents.json, skills/ and
	// one .mcp.json per agent folder. Empty, or a folder without agents.json
	// and skills/, imports nothing from files.
	RepoDir string
	// The database's agents, skills and MCP servers.
	Agents     []*domain.Agent
	Skills     []*skill.Skill
	MCPServers []*mcpserver.Server
}

// ImportReport says what an import did, for the log.
type ImportReport struct {
	// Skipped is set when the catalog already had content: nothing was done.
	Skipped bool
	Agents  int
	Bundles int
	Skills  int
	Servers int
	// DroppedServers names each server definition the import dropped because
	// another file defined the same name differently.
	DroppedServers []string
	// Clashes names each database item dropped because the repository had one
	// with its name.
	Clashes []string
	// Dropped names what the catalog cannot hold (an agent's prompt).
	Dropped []string
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// slugOf is a name as a catalog slug: lower case, other characters as dashes.
func slugOf(name string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 64 {
		s = strings.Trim(s[:64], "-")
	}
	return s
}

// catalogIsEmpty reports whether the catalog folder is missing or holds
// nothing.
func (c *Catalog) catalogIsEmpty() (bool, error) {
	entries, err := os.ReadDir(c.root)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

// Import fills a missing or empty catalog from src, once. A catalog that has
// any content is never touched.
func (c *Catalog) Import(src ImportSource) (ImportReport, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	var rep ImportReport
	empty, err := c.catalogIsEmpty()
	if err != nil {
		return rep, err
	}
	if !empty {
		rep.Skipped = true
		return rep, nil
	}

	agentsRoot := newObject()
	serversRoot := newObject()
	servers := newObject()
	serversRoot.set("mcpServers", servers)
	// files is every file to write: path in the catalog → bytes and mode.
	files := map[string]importFile{}

	if src.RepoDir != "" {
		if err := c.importRepo(src.RepoDir, agentsRoot, servers, files, &rep); err != nil {
			return rep, err
		}
	}
	c.importDatabase(src, agentsRoot, servers, files, &rep)

	rep.Agents = len(agentsRoot.get("agents").keys())
	rep.Bundles = len(agentsRoot.get("bundles").keys())
	rep.Servers = len(servers.keys())
	skillNames := map[string]bool{}
	for p := range files {
		if rest, ok := strings.CutPrefix(p, skillsDir+"/"); ok {
			name, _, _ := strings.Cut(rest, "/")
			skillNames[name] = true
		}
	}
	rep.Skills = len(skillNames)
	if len(files) == 0 && rep.Agents == 0 && rep.Bundles == 0 && rep.Servers == 0 && len(agentsRoot.keys()) == 0 {
		return rep, nil
	}
	if len(agentsRoot.keys()) > 0 {
		files[agentsFile] = importFile{data: append(agentsRoot.encode(), '\n'), mode: 0o644}
	}
	if rep.Servers > 0 {
		files[mcpServersFile] = importFile{data: append(serversRoot.encode(), '\n'), mode: 0o644}
	}
	if err := c.writeImport(files); err != nil {
		return rep, err
	}
	slog.Info("agent catalog imported", "dir", c.root, "agents", rep.Agents, "bundles", rep.Bundles,
		"skills", rep.Skills, "mcp_servers", rep.Servers)
	for _, d := range rep.DroppedServers {
		slog.Warn("agent catalog import dropped an MCP server definition", "detail", d)
	}
	for _, d := range rep.Clashes {
		slog.Info("agent catalog import kept the repository's version", "detail", d)
	}
	for _, d := range rep.Dropped {
		slog.Warn("agent catalog import dropped what the catalog cannot hold", "detail", d)
	}
	return rep, nil
}

type importFile struct {
	data []byte
	mode fs.FileMode
}

// writeImport writes the files into a sibling folder and moves it into
// place, so a crash leaves no half-filled catalog.
func (c *Catalog) writeImport(files map[string]importFile) error {
	tmp := c.root + ".importing"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	for p, f := range files {
		full := filepath.Join(tmp, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, f.data, f.mode); err != nil {
			return err
		}
		// WriteFile is subject to the umask: set the mode the source had.
		if err := os.Chmod(full, f.mode); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(c.root), 0o755); err != nil {
		return err
	}
	// An empty folder is replaced by the rename on POSIX; remove it anyway.
	if err := os.Remove(c.root); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(tmp, c.root)
}

// importRepo reads the repository's agents/ folder: agents.json kept as it
// is but for each agent's "mcp" field, every skill folder, and the servers of
// every .mcp.json merged by name.
func (c *Catalog) importRepo(dir string, agentsRoot, servers *node, files map[string]importFile, rep *ImportReport) error {
	data, err := os.ReadFile(filepath.Join(dir, agentsFile))
	switch {
	case err == nil:
		n, perr := parseNode(data)
		if perr != nil || n == nil || n.kind != 'o' {
			return fmt.Errorf("import: %s in %s does not parse", agentsFile, dir)
		}
		agentsRoot.members = n.members
	case errors.Is(err, os.ErrNotExist):
	default:
		return err
	}

	// Each agent's servers come from the .mcp.json its "mcp" field names. The
	// agents go in key order, so which file wins a clash does not depend on
	// how agents.json is ordered.
	agents := agentsRoot.get("agents")
	keys := agents.keys()
	sort.Strings(keys)
	loadedFiles := map[string][]string{} // path → server names in file order
	origin := map[string]string{}        // server name → agent whose file defined it
	for _, key := range keys {
		a := agents.get(key)
		mcp := a.get("mcp").str()
		if mcp == "" {
			continue
		}
		ownDir := a.get("dir").str()
		if ownDir == "" {
			ownDir = key
		}
		folder := ownDir
		if mcp != "self" {
			folder = mcp
		}
		path := filepath.Join(dir, folder, ".mcp.json")
		names, seen := loadedFiles[path]
		if !seen {
			names = c.importServerFile(path, key, servers, origin, rep)
			loadedFiles[path] = names
		}
		a.set("mcp_servers", stringArray(names))
	}
	// "mcp" becomes "mcp_servers" where it stood.
	for _, m := range agentsOf(agents) {
		if m.val.get("mcp") == nil {
			continue
		}
		list := m.val.get("mcp_servers")
		m.val.remove("mcp_servers")
		m.val.rename("mcp", "mcp_servers")
		m.val.set("mcp_servers", list)
	}

	// Every skill folder, as it is.
	skillRoot := filepath.Join(dir, skillsDir)
	entries, err := os.ReadDir(skillRoot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		base := filepath.Join(skillRoot, e.Name())
		err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			body, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(skillRoot, p)
			files[skillsDir+"/"+filepath.ToSlash(rel)] = importFile{data: body, mode: info.Mode().Perm()}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func agentsOf(agents *node) []*member {
	if agents == nil {
		return nil
	}
	return agents.members
}

// importServerFile merges one .mcp.json into servers by name and returns the
// names it defines, in file order. A name already defined differently keeps
// the first definition; the drop is reported.
func (c *Catalog) importServerFile(path, agent string, servers *node, origin map[string]string, rep *ImportReport) []string {
	names := []string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return names
	}
	root, perr := parseNode(data)
	if perr != nil || root == nil {
		rep.Dropped = append(rep.Dropped, path+" does not parse")
		return names
	}
	for _, m := range root.get("mcpServers").members {
		names = append(names, m.key)
		if prev := servers.get(m.key); prev != nil {
			if string(prev.encode()) != string(m.val.encode()) {
				rep.DroppedServers = append(rep.DroppedServers, fmt.Sprintf("%s: the definition in the folder of %s differs from the one kept from the folder of %s", m.key, agent, origin[m.key]))
			}
			continue
		}
		servers.set(m.key, m.val)
		origin[m.key] = agent
	}
	return names
}

// importDatabase adds what the old database catalog held. The repository's
// version of an item wins when both have the name.
func (c *Catalog) importDatabase(src ImportSource, agentsRoot, servers *node, files map[string]importFile, rep *ImportReport) {
	skillByID := map[string]string{}
	for _, s := range src.Skills {
		skillByID[s.ID] = slugOf(s.Name)
	}
	serverByID := map[string]string{}
	for _, s := range src.MCPServers {
		serverByID[s.ID] = serverName(s.Name)
	}

	for _, s := range src.MCPServers {
		name := serverName(s.Name)
		if name == "" {
			rep.Dropped = append(rep.Dropped, fmt.Sprintf("MCP server %q has no usable name", s.Name))
			continue
		}
		if servers.get(name) != nil {
			rep.Clashes = append(rep.Clashes, "MCP server "+name)
			continue
		}
		in := domain.CatalogServerInput{Name: name, Command: s.Command, Args: s.Args, Env: s.Env, URL: s.URL, Headers: s.Headers}
		switch mcpserver.NormalizeTransport(s.Transport) {
		case mcpserver.TransportSSE:
			in.Transport = "sse"
		case mcpserver.TransportStreamableHTTP:
			in.Transport = "http"
		default:
			in.Transport = "stdio"
		}
		transport, err := serverTransport(in)
		if err != nil {
			continue
		}
		servers.set(name, serverNode(nil, in, transport))
	}

	for _, s := range src.Skills {
		name := slugOf(s.Name)
		if name == "" {
			rep.Dropped = append(rep.Dropped, fmt.Sprintf("skill %q has no usable name", s.Name))
			continue
		}
		prefix := skillsDir + "/" + name + "/"
		if _, taken := files[prefix+skillFileName]; taken {
			rep.Clashes = append(rep.Clashes, "skill "+name)
			continue
		}
		wroteMarkdown := false
		for _, f := range s.Files {
			if f.Dir || f.Path == "" {
				continue
			}
			rel, ok := skill.NormalizePath(f.Path)
			if !ok {
				continue
			}
			body := []byte(f.Content)
			if f.Encoding == "base64" {
				if b, err := base64.StdEncoding.DecodeString(f.Content); err == nil {
					body = b
				}
			}
			if rel == skillFileName {
				wroteMarkdown = true
			}
			files[prefix+rel] = importFile{data: body, mode: 0o644}
		}
		if !wroteMarkdown {
			md, err := skillMarkdown(name, s.Description, s.Content, nil)
			if err != nil {
				continue
			}
			files[prefix+skillFileName] = importFile{data: md, mode: 0o644}
		}
	}

	agents := agentsRoot.get("agents")
	if agents == nil && len(src.Agents) > 0 {
		agents = newObject()
		agentsRoot.set("agents", agents)
	}
	for _, a := range src.Agents {
		key := slugOf(a.Name)
		if key == "" {
			rep.Dropped = append(rep.Dropped, fmt.Sprintf("agent %q has no usable name", a.Name))
			continue
		}
		if agents.get(key) != nil {
			rep.Clashes = append(rep.Clashes, "agent "+key)
			continue
		}
		obj := newObject()
		obj.set("description", scalarOf(a.Description))
		var skills, serverNames []string
		for _, id := range a.SkillIDs {
			if n := skillByID[id]; n != "" {
				skills = append(skills, n)
			}
		}
		for _, id := range a.MCPServerIDs {
			if n := serverByID[id]; n != "" {
				serverNames = append(serverNames, n)
			}
		}
		obj.set("skills", stringArray(skills))
		if len(serverNames) > 0 {
			obj.set("mcp_servers", stringArray(serverNames))
		}
		agents.set(key, obj)
		if a.PromptID != "" {
			rep.Dropped = append(rep.Dropped, fmt.Sprintf("agent %s: its prompt %s (prompts are not in the catalog)", key, a.PromptID))
		}
	}
}

// serverName is an MCP server name the catalog accepts, or "".
func serverName(name string) string {
	n := strings.TrimSpace(name)
	if serverNameRe.MatchString(n) {
		return n
	}
	return strings.Trim(regexp.MustCompile(`[^A-Za-z0-9_-]+`).ReplaceAllString(n, "-"), "-")
}
