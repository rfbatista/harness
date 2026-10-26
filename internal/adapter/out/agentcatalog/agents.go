package agentcatalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
)

func invalid(field, message string) error {
	return domain.NewCatalogError("INVALID_INPUT", message, map[string]any{"field": field})
}

func unknownRefs(names []string) error {
	return domain.NewCatalogError("UNKNOWN_REFERENCE", "these do not exist: "+strings.Join(names, ", "), map[string]any{"names": names})
}

// freeName is name with a number added until no taken name matches it.
func freeName(name string, taken func(string) bool) string {
	for i := 2; ; i++ {
		if cand := name + "-" + strconv.Itoa(i); !taken(cand) {
			return cand
		}
	}
}

func nameTaken(kind, name string, taken func(string) bool) error {
	return domain.NewCatalogError("NAME_TAKEN", "a "+kind+" named "+name+" already exists",
		map[string]any{"field": "name", "suggestion": freeName(name, taken)})
}

func (c *Catalog) skillExists(name string) bool {
	if !slugRe.MatchString(name) {
		return false
	}
	info, err := os.Stat(filepath.Join(c.root, skillsDir, name))
	return err == nil && info.IsDir()
}

// checkEntries refuses entries that name no skill or no bundle. bundles is
// the set of bundle names the save leaves in place.
func (t *txn) checkEntries(field string, entries []string, bundles *node) error {
	var unknown []string
	for _, e := range entries {
		if name, ok := strings.CutPrefix(e, "@"); ok {
			if !slugRe.MatchString(name) {
				return invalid(field, "a bundle entry is @ and a lower-case name: "+e)
			}
			if bundles.get(name) == nil {
				unknown = append(unknown, e)
			}
			continue
		}
		if !slugRe.MatchString(e) {
			return invalid(field, "a skill name is lower-case letters, digits and dashes: "+e)
		}
		if !t.c.skillExists(e) {
			unknown = append(unknown, e)
		}
	}
	if len(unknown) > 0 {
		return unknownRefs(unknown)
	}
	return nil
}

// resolveCount is how many skills an agent's entries resolve to.
func (t *txn) resolveCount(entries []string) int {
	bundles := map[string][]string{}
	for _, m := range t.bundlesView().members {
		bundles[m.key] = m.val.strs()
	}
	return len(resolve(entries, bundles).skills)
}

// SaveCatalogAgent creates, updates or renames an agent.
func (c *Catalog) SaveCatalogAgent(in domain.SaveCatalogAgent) (*domain.CatalogWriteResult, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	t, err := c.begin()
	if err != nil {
		return nil, err
	}
	a := in.Agent
	if !slugRe.MatchString(a.Name) {
		return nil, invalid("name", "an agent name is lower-case letters, digits and dashes, up to 64")
	}
	if strings.TrimSpace(a.Description) == "" && !in.AllowNoDescription {
		return nil, invalid("description", "an agent needs a description")
	}
	agents := t.agentsMap()
	prev := in.PreviousName
	if prev != "" && agents.get(prev) == nil {
		return nil, &domain.StructuredError{Code: "AGENT_NOT_FOUND", Message: "agent " + prev + " not found"}
	}
	if (prev == "" || prev != a.Name) && agents.get(a.Name) != nil {
		return nil, nameTaken("agent", a.Name, func(n string) bool { return agents.get(n) != nil })
	}
	if err := t.checkEntries("entries", a.Entries, t.bundlesView()); err != nil {
		return nil, err
	}
	var unknown []string
	for _, s := range a.MCPServers {
		if !serverNameRe.MatchString(s) {
			return nil, invalid("mcp_servers", "bad MCP server name: "+s)
		}
		if t.serversView().get(s) == nil {
			unknown = append(unknown, s)
		}
	}
	if len(unknown) > 0 {
		return nil, unknownRefs(unknown)
	}

	// Which selected projects enable it: each must exist and be selected.
	if in.Projects != nil {
		byID := map[string]*domain.Project{}
		for _, p := range t.projectLists() {
			byID[p.ID] = p
		}
		var unknown []string
		for _, id := range in.Projects {
			p := byID[id]
			switch {
			case p == nil:
				unknown = append(unknown, id)
			case p.EnabledAgentsMode != domain.AgentsSelected:
				return nil, invalid("projects", "project "+p.Name+" enables all agents: change it in its own settings")
			}
		}
		if len(unknown) > 0 {
			return nil, unknownRefs(unknown)
		}
	}

	verb := "Saved"
	obj := agents.get(prev)
	switch {
	case prev == "":
		obj = newObject()
		agents.set(a.Name, obj)
		verb = "Created"
	case prev != a.Name:
		agents.rename(prev, a.Name)
		for _, p := range t.projectLists() {
			if p.EnabledAgentsMode == domain.AgentsSelected {
				if cur := t.enabledAgents(p); contains(cur, prev) {
					t.setEnabledAgents(p, replaceName(cur, prev, a.Name))
				}
			}
		}
		verb = "Renamed " + prev + " to " + a.Name + " and saved"
	}
	obj.set("description", scalarOf(a.Description))
	setList := func(key string, items []string) {
		if len(items) == 0 && obj.get(key) == nil {
			return
		}
		obj.set(key, stringArray(items))
	}
	setList("aliases", a.Aliases)
	setList("skills", a.Entries)
	setList("mcp_servers", a.MCPServers)
	setList("allowed_tools", a.AllowedTools)
	if in.Projects != nil {
		for _, p := range t.projectLists() {
			if p.EnabledAgentsMode != domain.AgentsSelected {
				continue
			}
			cur := t.enabledAgents(p)
			switch wanted := contains(in.Projects, p.ID); {
			case wanted && !contains(cur, a.Name):
				t.setEnabledAgents(p, append(append([]string{}, cur...), a.Name))
			case !wanted && contains(cur, a.Name):
				t.setEnabledAgents(p, without(cur, a.Name))
			}
		}
	}
	summary := fmt.Sprintf("%s %s — %s now has %d skills", verb, agentsFile, a.Name, t.resolveCount(a.Entries))
	return t.commit(in.CatalogWriteOptions, summary)
}

// DeleteCatalogAgent removes an agent.
func (c *Catalog) DeleteCatalogAgent(in domain.DeleteCatalogItem) (*domain.CatalogWriteResult, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	t, err := c.begin()
	if err != nil {
		return nil, err
	}
	if t.agentsMap().get(in.Name) == nil {
		return nil, &domain.StructuredError{Code: "AGENT_NOT_FOUND", Message: "agent " + in.Name + " not found"}
	}
	t.agentsMap().remove(in.Name)
	for _, p := range t.projectLists() {
		if p.EnabledAgentsMode == domain.AgentsSelected {
			if cur := t.enabledAgents(p); contains(cur, in.Name) {
				t.setEnabledAgents(p, without(cur, in.Name))
			}
		}
	}
	return t.commit(in.CatalogWriteOptions, "Deleted "+in.Name+" from "+agentsFile)
}

// rewriteEntries applies fn to every skills list of every agent and every
// bundle. fn returns the entry's new text, or false to drop it. It reports
// how many entries changed.
func (t *txn) rewriteEntries(fn func(string) (string, bool)) int {
	changed := 0
	each := func(list *node) {
		if list == nil || list.kind != 'a' {
			return
		}
		var items []*node
		for _, it := range list.items {
			if it.kind != 's' || len(it.raw) == 0 || it.raw[0] != '"' {
				items = append(items, it)
				continue
			}
			old := it.str()
			next, keep := fn(old)
			switch {
			case !keep:
				changed++
			case next != old:
				changed++
				items = append(items, scalarOf(next))
			default:
				items = append(items, it)
			}
		}
		list.items = items
	}
	for _, m := range t.agentsMap().members {
		each(m.val.get("skills"))
	}
	for _, m := range t.bundlesMap().members {
		each(m.val)
	}
	return changed
}

// SaveCatalogBundle creates, updates or renames a bundle.
func (c *Catalog) SaveCatalogBundle(in domain.SaveCatalogBundle) (*domain.CatalogWriteResult, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	t, err := c.begin()
	if err != nil {
		return nil, err
	}
	b := in.Bundle
	if !slugRe.MatchString(b.Name) {
		return nil, invalid("name", "a bundle name is lower-case letters, digits and dashes, up to 64")
	}
	bundles := t.bundlesMap()
	prev := in.PreviousName
	if prev != "" && bundles.get(prev) == nil {
		return nil, &domain.StructuredError{Code: "BUNDLE_NOT_FOUND", Message: "bundle " + prev + " not found"}
	}
	if (prev == "" || prev != b.Name) && bundles.get(b.Name) != nil {
		return nil, nameTaken("bundle", b.Name, func(n string) bool { return bundles.get(n) != nil })
	}
	verb := "Saved"
	refs := 0
	switch {
	case prev == "":
		verb = "Created"
	case prev != b.Name:
		bundles.rename(prev, b.Name)
		refs = t.rewriteEntries(func(e string) (string, bool) {
			if e == "@"+prev {
				return "@" + b.Name, true
			}
			return e, true
		})
		verb = "Renamed " + prev + " to " + b.Name + " (" + strconv.Itoa(refs) + " references updated) and saved"
	}
	if err := t.checkEntries("entries", b.Entries, bundles); err != nil {
		return nil, err
	}
	bundles.set(b.Name, stringArray(b.Entries))
	// A bundle that reaches itself would never resolve.
	if cyc := t.cycleThrough(b.Name); cyc != "" {
		return nil, domain.NewCatalogError("BUNDLE_CYCLE", "the bundles would include each other: "+cyc, map[string]any{"field": "entries"})
	}
	summary := fmt.Sprintf("%s %s — bundle %s now has %d skills", verb, agentsFile, b.Name, t.resolveCount([]string{"@" + b.Name}))
	return t.commit(in.CatalogWriteOptions, summary)
}

// cycleThrough returns the path of bundles by which name includes itself, or
// "" when it does not.
func (t *txn) cycleThrough(name string) string {
	bundles := t.bundlesMap()
	var walk func(cur string, path []string) string
	seen := map[string]bool{}
	walk = func(cur string, path []string) string {
		for _, e := range bundles.get(cur).strs() {
			next, ok := strings.CutPrefix(e, "@")
			if !ok {
				continue
			}
			if next == name {
				return strings.Join(append(path, "@"+name), " → ")
			}
			if seen[next] {
				continue
			}
			seen[next] = true
			if p := walk(next, append(path, "@"+next)); p != "" {
				return p
			}
		}
		return ""
	}
	return walk(name, []string{"@" + name})
}

// DeleteCatalogBundle removes a bundle and every entry that names it.
func (c *Catalog) DeleteCatalogBundle(in domain.DeleteCatalogItem) (*domain.CatalogWriteResult, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	t, err := c.begin()
	if err != nil {
		return nil, err
	}
	if t.bundlesMap().get(in.Name) == nil {
		return nil, &domain.StructuredError{Code: "BUNDLE_NOT_FOUND", Message: "bundle " + in.Name + " not found"}
	}
	t.bundlesMap().remove(in.Name)
	refs := t.rewriteEntries(func(e string) (string, bool) { return e, e != "@"+in.Name })
	return t.commit(in.CatalogWriteOptions, fmt.Sprintf("Deleted bundle %s from %s (%d references removed)", in.Name, agentsFile, refs))
}
