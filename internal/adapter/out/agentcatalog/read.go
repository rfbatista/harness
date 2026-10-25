package agentcatalog

import (
	"encoding/base64"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/rfbatista/harnesskit/skill"

	"operators-mcp/internal/domain"
)

// builtInTools are the Claude built-in tools an agent's allowed tools may name.
var builtInTools = []domain.CatalogBuiltInTool{
	{Name: "Read", Description: "Read files"},
	{Name: "Write", Description: "Create or overwrite files"},
	{Name: "Edit", Description: "Edit files by exact replacement"},
	{Name: "NotebookEdit", Description: "Edit Jupyter notebook cells"},
	{Name: "Glob", Description: "Find files by name pattern"},
	{Name: "Grep", Description: "Search file contents"},
	{Name: "Bash", Description: "Run shell commands"},
	{Name: "WebFetch", Description: "Fetch a web page"},
	{Name: "WebSearch", Description: "Search the web"},
	{Name: "TodoWrite", Description: "Keep a to-do list"},
	{Name: "Task", Description: "Start a sub-agent"},
}

// skillInfo is what the whole-catalog read keeps of a skill: its frontmatter
// and the size of its folder, never its content.
type skillInfo struct {
	name, description string
	files             int
	problems          []domain.CatalogProblem
}

// scanSkills lists every skill folder, reading only each SKILL.md's head.
func (c *Catalog) scanSkills() map[string]*skillInfo {
	out := map[string]*skillInfo{}
	entries, err := os.ReadDir(filepath.Join(c.root, skillsDir))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() || !slugRe.MatchString(e.Name()) {
			continue
		}
		dir := filepath.Join(c.root, skillsDir, e.Name())
		info := &skillInfo{name: e.Name(), problems: []domain.CatalogProblem{}}
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				info.files++
			}
			return nil
		})
		data, err := os.ReadFile(filepath.Join(dir, skillFileName))
		switch {
		case err != nil:
			info.problems = append(info.problems, domain.CatalogProblem{Code: domain.CatalogProblemInvalidSkill, Message: "SKILL.md is missing"})
		case !strings.HasPrefix(string(data), "---"):
			info.problems = append(info.problems, domain.CatalogProblem{Code: domain.CatalogProblemInvalidSkill, Message: "SKILL.md has no frontmatter"})
		default:
			doc, perr := skill.ParseDocument(data)
			if perr != nil {
				info.problems = append(info.problems, domain.CatalogProblem{Code: domain.CatalogProblemInvalidSkill, Message: "SKILL.md frontmatter does not parse: " + perr.Error()})
				break
			}
			info.description = doc.Description
			if doc.Description == "" {
				info.problems = append(info.problems, domain.CatalogProblem{Code: domain.CatalogProblemNoDescription, Message: "the skill has no description"})
			}
		}
		out[e.Name()] = info
	}
	return out
}

// resolution is a list of entries expanded against the bundles.
type resolution struct {
	skills     []string
	duplicates []string
	problems   []domain.CatalogProblem
}

// resolve expands entries: a plain name is a skill, "@name" a bundle,
// replaced by its own resolved entries. Duplicates are dropped in first-seen
// order and listed. An unknown bundle or a cycle is a problem, and the entry
// is skipped.
func resolve(entries []string, bundles map[string][]string) resolution {
	var r resolution
	seen := map[string]bool{}
	dupSeen := map[string]bool{}
	var walk func(entries []string, active map[string]bool)
	walk = func(entries []string, active map[string]bool) {
		for _, e := range entries {
			if !strings.HasPrefix(e, "@") {
				if seen[e] {
					if !dupSeen[e] {
						dupSeen[e] = true
						r.duplicates = append(r.duplicates, e)
					}
					continue
				}
				seen[e] = true
				r.skills = append(r.skills, e)
				continue
			}
			name := strings.TrimPrefix(e, "@")
			if active[name] {
				r.problems = append(r.problems, domain.CatalogProblem{Code: domain.CatalogProblemBundleCycle, Message: "bundle @" + name + " includes itself"})
				continue
			}
			list, ok := bundles[name]
			if !ok {
				r.problems = append(r.problems, domain.CatalogProblem{Code: domain.CatalogProblemUnknownBundle, Message: "there is no bundle @" + name})
				continue
			}
			nested := make(map[string]bool, len(active)+1)
			for k := range active {
				nested[k] = true
			}
			nested[name] = true
			walk(list, nested)
		}
	}
	walk(entries, map[string]bool{})
	return r
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ReadCatalog reads the whole catalog.
func (c *Catalog) ReadCatalog() (*domain.CatalogView, error) {
	l, err := c.load()
	if err != nil {
		return nil, err
	}
	skills := c.scanSkills()

	bundleEntries := map[string][]string{}
	var bundleOrder []string
	if b := l.agents.get("bundles"); b != nil {
		for _, m := range b.members {
			bundleEntries[m.key] = m.val.strs()
			bundleOrder = append(bundleOrder, m.key)
		}
	}
	serverNames := l.servers.keys()

	v := &domain.CatalogView{
		Root:         c.root,
		Versions:     l.versions,
		Broken:       l.broken,
		Agents:       []domain.CatalogAgent{},
		Bundles:      []domain.CatalogBundle{},
		Skills:       []domain.CatalogSkillSummary{},
		MCPServers:   []domain.CatalogMCPServer{},
		BuiltInTools: builtInTools,
	}

	agentsOfSkill := map[string][]string{}
	reach := map[string]int{}
	bundlesOfSkill := map[string][]string{}
	agentsOfBundle := map[string][]string{}
	bundlesOfBundle := map[string][]string{}
	agentsOfServer := map[string][]string{}

	if a := l.agents.get("agents"); a != nil {
		for _, m := range a.members {
			ag := domain.CatalogAgent{
				Name:         m.key,
				Description:  m.val.get("description").str(),
				Aliases:      nonNil(m.val.get("aliases").strs()),
				Entries:      nonNil(m.val.get("skills").strs()),
				MCPServers:   nonNil(m.val.get("mcp_servers").strs()),
				AllowedTools: nonNil(m.val.get("allowed_tools").strs()),
				Problems:     []domain.CatalogProblem{},
			}
			r := resolve(ag.Entries, bundleEntries)
			ag.ResolvedSkills = nonNil(r.skills)
			ag.DroppedDuplicates = nonNil(r.duplicates)
			ag.MissingSkills = []string{}
			ag.Problems = append(ag.Problems, r.problems...)
			if ag.Description == "" {
				ag.Problems = append(ag.Problems, domain.CatalogProblem{Code: domain.CatalogProblemNoDescription, Message: "the agent has no description"})
			}
			for _, s := range ag.ResolvedSkills {
				reach[s]++
				if _, ok := skills[s]; !ok {
					ag.MissingSkills = append(ag.MissingSkills, s)
					ag.Problems = append(ag.Problems, domain.CatalogProblem{Code: domain.CatalogProblemMissingSkill, Message: "the skill " + s + " does not exist"})
				}
			}
			for _, e := range ag.Entries {
				if strings.HasPrefix(e, "@") {
					agentsOfBundle[e[1:]] = append(agentsOfBundle[e[1:]], ag.Name)
				} else {
					agentsOfSkill[e] = append(agentsOfSkill[e], ag.Name)
				}
			}
			for _, s := range ag.MCPServers {
				agentsOfServer[s] = append(agentsOfServer[s], ag.Name)
				if !contains(serverNames, s) {
					ag.Problems = append(ag.Problems, domain.CatalogProblem{Code: domain.CatalogProblemUnknownMCPServer, Message: "the MCP server " + s + " does not exist"})
				}
			}
			v.Agents = append(v.Agents, ag)
		}
	}

	for _, name := range bundleOrder {
		for _, e := range bundleEntries[name] {
			if strings.HasPrefix(e, "@") {
				bundlesOfBundle[e[1:]] = append(bundlesOfBundle[e[1:]], name)
			} else {
				bundlesOfSkill[e] = append(bundlesOfSkill[e], name)
			}
		}
		r := resolve(bundleEntries[name], bundleEntries)
		v.Bundles = append(v.Bundles, domain.CatalogBundle{
			Name:           name,
			Entries:        nonNil(bundleEntries[name]),
			ResolvedSkills: nonNil(r.skills),
			UsedByAgents:   nonNil(agentsOfBundle[name]),
			UsedByBundles:  nonNil(bundlesOfBundle[name]),
		})
	}
	// A bundle's reach lists were filled while the loop ran; the first bundles
	// may have been listed before a later one named them.
	for i := range v.Bundles {
		v.Bundles[i].UsedByBundles = nonNil(bundlesOfBundle[v.Bundles[i].Name])
	}

	names := make([]string, 0, len(skills))
	for n := range skills {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s := skills[n]
		v.Skills = append(v.Skills, domain.CatalogSkillSummary{
			Name:          n,
			Description:   s.description,
			FileCount:     s.files,
			HasExtraFiles: s.files > 1,
			Agents:        nonNil(agentsOfSkill[n]),
			Bundles:       nonNil(bundlesOfSkill[n]),
			AgentCount:    reach[n],
			Problems:      s.problems,
		})
	}

	projects := c.listProjects()
	for _, name := range serverNames {
		srv := c.serverView(name, l.servers.get(name), agentsOfServer[name])
		for _, p := range projects {
			if contains(p.MCPServers, name) {
				srv.Projects = append(srv.Projects, domain.CatalogProjectRef{ID: p.ID, Name: p.Name})
			}
		}
		v.MCPServers = append(v.MCPServers, srv)
	}
	return v, nil
}

// serverView is one mcp-servers.json entry as the catalog read lists it.
func (c *Catalog) serverView(name string, n *node, agents []string) domain.CatalogMCPServer {
	srv := domain.CatalogMCPServer{
		Name:     name,
		Command:  n.get("command").str(),
		Args:     nonNil(n.get("args").strs()),
		Env:      n.get("env").strMap(),
		URL:      n.get("url").str(),
		Headers:  n.get("headers").strMap(),
		EnvRefs:  []domain.CatalogEnvRef{},
		Agents:   nonNil(agents),
		Projects: []domain.CatalogProjectRef{},
		Problems: []domain.CatalogProblem{},
	}
	switch t := n.get("type").str(); {
	case t == "http" || t == "sse":
		srv.Transport = t
	case srv.URL != "":
		srv.Transport = "http"
	default:
		srv.Transport = "stdio"
	}
	refs := map[string]bool{}
	scan := func(s string) {
		for _, m := range envRefRe.FindAllStringSubmatch(s, -1) {
			refs[m[1]] = true
		}
	}
	scan(srv.Command)
	scan(srv.URL)
	for _, a := range srv.Args {
		scan(a)
	}
	for _, v := range srv.Env {
		scan(v)
	}
	for _, v := range srv.Headers {
		scan(v)
	}
	vars := make([]string, 0, len(refs))
	for r := range refs {
		vars = append(vars, r)
	}
	sort.Strings(vars)
	for _, r := range vars {
		_, set := c.lookupEnv(r)
		srv.EnvRefs = append(srv.EnvRefs, domain.CatalogEnvRef{Name: r, Set: set})
		if !set {
			srv.Problems = append(srv.Problems, domain.CatalogProblem{Code: domain.CatalogProblemSecretNotSet, Message: r + " is not set in the harness server's environment"})
		}
	}
	return srv
}

// CheckEnvVars says whether each variable is set. Never a value.
func (c *Catalog) CheckEnvVars(names []string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		_, ok := c.lookupEnv(n)
		out[n] = ok
	}
	return out
}

// ReadCatalogSkill reads one skill with its body and every other file.
func (c *Catalog) ReadCatalogSkill(name string) (*domain.CatalogSkillView, error) {
	if !slugRe.MatchString(name) {
		return nil, domain.NewCatalogError("INVALID_INPUT", "a skill name is lower-case letters, digits and dashes", map[string]any{"field": "name"})
	}
	dir := filepath.Join(c.root, skillsDir, name)
	data, err := os.ReadFile(filepath.Join(dir, skillFileName))
	if err != nil {
		return nil, &domain.StructuredError{Code: "SKILL_NOT_FOUND", Message: "skill " + name + " not found"}
	}
	doc, _ := skill.ParseDocument(data)
	sk := domain.CatalogSkill{Name: name, Description: doc.Description, Body: doc.Body, Files: []domain.CatalogSkillFile{}}
	if !strings.HasPrefix(string(data), "---") {
		sk.Body = strings.TrimSpace(string(data))
	}
	files, version, err := readSkillFolder(dir)
	if err != nil {
		return nil, err
	}
	sk.Files = files
	cat, err := c.ReadCatalog()
	if err != nil {
		return nil, err
	}
	for _, s := range cat.Skills {
		if s.Name == name {
			sk.Agents, sk.Bundles, sk.AgentCount = s.Agents, s.Bundles, s.AgentCount
		}
	}
	sk.Agents, sk.Bundles = nonNil(sk.Agents), nonNil(sk.Bundles)
	return &domain.CatalogSkillView{Skill: sk, Versions: map[string]string{"skills/" + name: version}}, nil
}

// readSkillFolder lists every file of a skill folder except SKILL.md, with
// content, and the folder's version: a hash over every file's path and bytes.
func readSkillFolder(dir string) ([]domain.CatalogSkillFile, string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != dir {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	sort.Strings(paths)
	h := newFolderHash()
	files := []domain.CatalogSkillFile{}
	for _, p := range paths {
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		info, err := os.Stat(p)
		if err != nil {
			return nil, "", err
		}
		if info.IsDir() {
			h.add(rel+"/", nil)
			files = append(files, domain.CatalogSkillFile{Path: rel, Dir: true})
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, "", err
		}
		h.add(rel, data)
		if rel == skillFileName {
			continue
		}
		f := domain.CatalogSkillFile{Path: rel, Size: info.Size(), Encoding: "utf-8"}
		if utf8.Valid(data) {
			f.Content = string(data)
		} else {
			f.Content = base64.StdEncoding.EncodeToString(data)
			f.Encoding = "base64"
		}
		files = append(files, f)
	}
	return files, h.sum(), nil
}
