package code

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
)

// The first paint's view of a seed: what the templ components render before
// the browser takes over. web/src/modules/code/presentation/view.js builds
// the same views from the same data; the copy is UX §5's, word for word.

// First-paint caps: the browser renders the rest once it takes over.
const (
	paintFileLines = 400
	paintDiffLines = 1200
)

// Mark is a change-mark: its letter and the word that names it.
type Mark struct {
	Change, Letter, Word string
}

var marks = map[string]Mark{
	"added":        {"added", "A", "Added"},
	"modified":     {"modified", "M", "Modified"},
	"deleted":      {"deleted", "D", "Deleted"},
	"renamed":      {"renamed", "R", "Renamed"},
	"copied":       {"copied", "C", "Copied"},
	"type_changed": {"type_changed", "T", "Type changed"},
	"untracked":    {"untracked", "U", "Untracked"},
	"contains":     {"contains", "", "Contains changes"},
}

func markOf(change string) *Mark {
	m, ok := marks[change]
	if !ok {
		return nil
	}
	return &m
}

// Seg is one item of a segmented switch.
type Seg struct {
	Label, Href, Count string
	Current            bool
	// Disabled says why the item cannot answer; "" when it can.
	Disabled string
}

// Bar is the code-source-bar.
type Bar struct {
	Views               []Seg
	Repo, Branch        string
	Commit, CommitTitle string
	Dirty               string
	State, Word         string
	Gone                bool
	BrowseHref          string
	Refs                []string
	Ref                 string
}

// ZoneTag is a zone-tag: its name and its context's hue.
type ZoneTag struct {
	Name, Hue, Title string
}

// TreeRow is a row of the file-tree, flat with its level.
type TreeRow struct {
	Path, Name, Kind, Target, Href, Title, Label string
	Depth, SetSize, PosInSet                     int
	Expanded                                     string // "true", "false" or "" for a file
	Deleted, Selected                            bool
	Mark                                         *Mark
	Dot                                          *ZoneTag
}

// SummaryRow is a row of the diff-summary.
type SummaryRow struct {
	Href, Dir, Name, Old, Note string
	Mark                       *Mark
	Added, Deleted             string
	HasStat, Current           bool
}

// Line is one row of a .code-lines block, painted as plain text.
type Line struct {
	// Hunk rows: Range and Header.
	Hunk          bool
	Range, Header string
	Kind          string // context, add, del; "" in a file
	Old, New      int    // 0 when absent
	N             int    // a file's line number
	Text          string
	NoNewline     bool
	Target        bool
}

// DiffFile is a section of the diff-view.
type DiffFile struct {
	Path, OldPath, Slug, ID, NameID, BodyID, ToggleLabel string
	Mark                                                 *Mark
	Added, Deleted                                       string
	HasStat, Collapsed                                   bool
	Zones                                                []ZoneTag
	ZoneMore                                             *ZoneTag
	Lines                                                []Line
	Digits                                               int
	Withheld                                             *Withheld
	ViewHref                                             string
}

// Withheld is the words in place of a withheld file's hunks.
type Withheld struct {
	Reason, Title, Detail string
	Load                  bool
}

// FileView is the code-view of an open file.
type FileView struct {
	Path, Meta  string
	Crumbs      []Crumb
	Zones       []ZoneTag
	ZoneMore    *ZoneTag
	Notice      *Notice
	Lines       []Line
	Digits      int
	Placeholder *Placeholder
}

// Crumb is a link of the crumbs block; the file itself has no Href.
type Crumb struct {
	Label, Href string
}

// Notice is a truncated file's strip.
type Notice struct {
	Title, Text, Path string
}

// Placeholder stands in for lines: binary, empty, a symlink or a submodule.
type Placeholder struct {
	Title, Text string
}

// Problem is an error the first paint shows: a danger banner, or an empty
// state in place of the view.
type Problem struct {
	Code, Text, Title string
	Empty             bool
}

// MapCard is a card of the code-map.
type MapCard struct {
	ID, Kind, Hue, Name, Purpose, Count string
	Zones                               []MapZone
}

// MapZone is a zone row of a map card.
type MapZone struct {
	Name, Pattern, Count, Href string
	Share                      float64
	Empty                      bool
}

// MapView is the code-map.
type MapView struct {
	Totals string
	Spread []MapSegment
	Cards  []MapCard
	Empty  bool
}

// MapSegment is a part of the overview bar.
type MapSegment struct {
	Hue, Kind string
	Share     int
}

// View is a code page's first paint.
type View struct {
	Kind    string
	Seed    Seed
	View    string
	Bar     Bar
	Problem *Problem
	// Changes
	Totals         string
	Added, Deleted string
	Scopes         []Seg
	Summary        []SummaryRow
	NoChanges      string
	Files          []DiffFile
	DiffTruncated  bool
	BaseGoneLine   string
	// Files
	Tree []TreeRow
	File *FileView
	// Map
	Map *MapView
}

// Build is the view of a seed.
func Build(s Seed) View {
	rt := s.Route
	v := View{Kind: rt.Kind, Seed: s, View: rt.View}
	if v.View == "" {
		v.View = ViewFiles
		if rt.Kind == "session" {
			v.View = ViewChanges
		}
	}
	zones := zoneIndex(s.Map)
	v.Bar = buildBar(s, v.View)
	if s.Error != nil && s.Error.Code != "WORKSPACE_MISSING" {
		v.Problem = problemOf(s, *s.Error)
	}
	if s.Diff != nil {
		v.buildDiff(s, zones)
	}
	v.Tree = treeRows(s, zones)
	if s.File != nil {
		v.File = fileView(s, *s.File, zones)
	}
	if s.Map != nil && v.View == ViewMap {
		v.Map = mapView(s, *s.Map)
	}
	return v
}

func buildBar(s Seed, view string) Bar {
	rt := s.Route
	b := Bar{Repo: s.RepositoryName, Gone: s.Gone, Refs: s.Refs, Ref: rt.Ref}
	commit := ""
	switch {
	case s.Diff != nil && s.Diff.Source.Kind == domain.CodeSourceWorkspace:
		commit = s.Diff.Source.Commit
	case len(s.Trees) > 0:
		commit = s.Trees[0].Source.Commit
	case s.File != nil:
		commit = s.File.Source.Commit
	case s.Map != nil:
		commit = s.Map.Source.Commit
	case s.Diff != nil:
		commit = s.Diff.Source.Commit
	}
	b.Commit, b.CommitTitle = short(commit), commit
	if rt.Kind == "session" {
		b.Branch = s.Branch
		switch {
		case s.Gone:
			b.State, b.Word = "gone", "worktree gone"
		case s.Live:
			b.State, b.Word = "live", "live"
		}
		b.BrowseHref = Route{Kind: "repository", ProjectID: rt.ProjectID, RepositoryID: s.repositoryID(), Ref: s.Branch}.Href("")
	} else {
		b.Branch = rt.Ref
		if b.Branch == "" && len(s.Trees) > 0 {
			b.Branch = s.Trees[0].Source.Ref
		}
		if b.Branch == "" && s.Map != nil {
			b.Branch = s.Map.Source.Ref
		}
		if b.Ref == "" {
			b.Ref = b.Branch
		}
	}
	views := []string{ViewFiles, ViewMap}
	if rt.Kind == "session" {
		views = []string{ViewChanges, ViewFiles, ViewMap}
	}
	labels := map[string]string{ViewChanges: "Changes", ViewFiles: "Files", ViewMap: "Map"}
	for _, name := range views {
		seg := Seg{Label: labels[name], Current: name == view}
		switch name {
		case ViewFiles:
			seg.Href = rt.With(ViewFiles, rt.Path).Href("")
		case ViewChanges:
			seg.Href = rt.With(ViewChanges, "").Href("")
			if s.Diff != nil {
				seg.Count = strconv.Itoa(len(s.Diff.Files))
			}
		default:
			seg.Href = rt.With(name, "").Href("")
		}
		b.Views = append(b.Views, seg)
	}
	return b
}

func (s Seed) repositoryID() string {
	switch {
	case s.Source.RepositoryID != "":
		return s.Source.RepositoryID
	case s.Diff != nil:
		return s.Diff.Source.RepositoryID
	case len(s.Trees) > 0:
		return s.Trees[0].Source.RepositoryID
	}
	return ""
}

func problemOf(s Seed, e SeedError) *Problem {
	rt := s.Route
	ref := rt.Ref
	if ref == "" {
		ref = s.Branch
	}
	on := " here"
	if ref != "" {
		on = " on " + ref
	}
	switch e.Code {
	case "PATH_NOT_FOUND":
		p := rt.Path
		if p == "" {
			p = "That path"
		}
		return &Problem{Code: e.Code, Text: p + " is not" + on}
	case "REF_NOT_FOUND":
		if rt.Kind == "repository" && rt.Ref == "" {
			name := s.RepositoryName
			if name == "" {
				name = "This repository"
			}
			return &Problem{Code: e.Code, Empty: true, Title: "No commits yet", Text: name + " has no commits to read. Files appear here after the first commit."}
		}
		return &Problem{Code: e.Code, Text: rt.Ref + " is not a branch or tag here"}
	case "INVALID_INPUT":
		return &Problem{Code: e.Code, Text: "Use a branch, tag or commit hash"}
	case "COMMIT_NOT_FOUND":
		return &Problem{Code: e.Code, Text: rt.Ref + " is not in this repository"}
	case "UNAVAILABLE":
		return &Problem{Code: e.Code, Empty: true, Title: "Code browsing is off on this server", Text: "The server was started without the code browser. The rest of the page keeps working."}
	case "REPOSITORY_NOT_FOUND":
		return &Problem{Code: e.Code, Empty: true, Title: "Repository not found", Text: "It was removed from this project."}
	case "WORKSPACE_NOT_FOUND":
		return &Problem{Code: e.Code, Empty: true, Title: "Worktree not found", Text: "This session has no worktree on record."}
	}
	return &Problem{Code: e.Code, Text: "Could not read this code"}
}

func (v *View) buildDiff(s Seed, zones map[string]ZoneTag) {
	d := s.Diff
	rt := s.Route
	v.Totals = plural(len(d.Files), "file")
	v.Added, v.Deleted = "+"+count(d.Added), "−"+count(d.Deleted)
	v.DiffTruncated = d.Truncated
	if s.BaseGone {
		if s.BaseRef != "" {
			v.BaseGoneLine = "The base branch " + s.BaseRef + " no longer resolves"
		} else {
			v.BaseGoneLine = "The base branch no longer resolves"
		}
	}
	scope := d.Scope
	baseWhy := "The base branch no longer exists"
	if s.BaseRef != "" {
		baseWhy = "The base branch " + s.BaseRef + " no longer exists"
	}
	goneWhy := "The worktree was removed, so its uncommitted work is gone"
	for _, sc := range []string{"all", "branch", "working"} {
		seg := Seg{Label: strings.ToUpper(sc[:1]) + sc[1:], Href: rt.withScope(sc).Href(""), Current: sc == scope}
		switch {
		case s.Gone && sc != "branch":
			seg.Disabled = goneWhy
		case s.BaseGone && sc != "working":
			seg.Disabled = baseWhy
		}
		v.Scopes = append(v.Scopes, seg)
	}
	if len(d.Files) == 0 {
		v.NoChanges = "No changes on this branch yet. The agent's edits appear here as it makes them."
		if scope == "working" {
			v.NoChanges = "No uncommitted changes."
		}
	}
	slugs := slugsOf(d.Files)
	budget := paintDiffLines
	for i, f := range d.Files {
		slug := slugs[f.Path]
		row := SummaryRow{Href: "#diff-" + slug, Mark: markOf(f.Status), Name: path.Base(f.Path), Current: i == 0}
		if dir := path.Dir(f.Path); dir != "." {
			row.Dir = dir + "/"
		}
		if f.OldPath != nil {
			row.Old = "from " + *f.OldPath
		}
		df := DiffFile{
			Path: f.Path, Slug: slug, ID: "diff-" + slug, NameID: "diff-" + slug + "-name", BodyID: "diff-" + slug + "-body",
			Mark: markOf(f.Status), Collapsed: f.Status == "deleted" && f.Deleted > 50,
		}
		if f.OldPath != nil {
			df.OldPath = *f.OldPath
		}
		if !f.Binary && f.Added >= 0 && f.Deleted >= 0 {
			row.HasStat, df.HasStat = true, true
			row.Added, row.Deleted = "+"+count(f.Added), "−"+count(f.Deleted)
			df.Added, df.Deleted = row.Added, row.Deleted
		}
		switch {
		case f.Binary:
			row.Note = "binary"
		case f.Withheld != nil && *f.Withheld == "too_large":
			row.Note = "not loaded"
		}
		df.ToggleLabel = "Collapse " + path.Base(f.Path)
		if df.Collapsed {
			df.ToggleLabel = "Expand " + path.Base(f.Path)
		}
		df.Zones, df.ZoneMore = zoneTags(f.Zones, zones)
		df.Withheld = withheldOf(f)
		if f.Status != "deleted" && !f.Binary && rt.Kind != "" {
			df.ViewHref = rt.With(ViewFiles, f.Path).Href("")
		}
		if f.Hunks != nil && !df.Collapsed && budget > 0 {
			df.Lines, budget = diffLines(f.Hunks, slug, budget)
		}
		df.Digits = digits(largestLine(f.Hunks))
		v.Summary = append(v.Summary, row)
		v.Files = append(v.Files, df)
	}
}

func (r Route) withScope(scope string) Route {
	r = r.With(ViewChanges, "")
	r.Scope = scope
	return r
}

func withheldOf(f domain.FileDiff) *Withheld {
	if f.Binary || (f.Withheld != nil && *f.Withheld == "binary") {
		return &Withheld{Reason: "binary", Title: "Binary file", Detail: "Its changes are not shown as text."}
	}
	if f.Withheld == nil || *f.Withheld != "too_large" {
		return nil
	}
	n := f.Added + f.Deleted
	if n > domain.MaxDiffLines {
		return &Withheld{Reason: "too_large", Title: "Too large to show here", Detail: "Over " + count(domain.MaxDiffLines) + " changed lines."}
	}
	return &Withheld{Reason: "too_large", Title: "Large diff, not loaded", Detail: count(n) + " changed lines. Loading it may slow this page.", Load: true}
}

func diffLines(hunks []domain.Hunk, slug string, budget int) ([]Line, int) {
	var out []Line
	for _, h := range hunks {
		if budget <= 0 {
			break
		}
		out = append(out, Line{Hunk: true, Range: fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OldStart, h.OldLines, h.NewStart, h.NewLines), Header: h.Header})
		for _, l := range h.Lines {
			if budget <= 0 {
				break
			}
			line := Line{Kind: l.Kind, Text: l.Text, NoNewline: l.NoNewline}
			if l.Old != nil {
				line.Old = *l.Old
			}
			if l.New != nil {
				line.New = *l.New
			}
			out = append(out, line)
			budget--
		}
	}
	return out, budget
}

func largestLine(hunks []domain.Hunk) int {
	n := 0
	for _, h := range hunks {
		n = max(n, h.OldStart+h.OldLines, h.NewStart+h.NewLines)
	}
	return n
}

func digits(n int) int {
	return max(1, len(strconv.Itoa(max(0, n))))
}

var notSlug = regexp.MustCompile(`[^A-Za-z0-9_]`)

// slugsOf is each file's anchor stem, unique in the diff (diff.js slugs).
func slugsOf(files []domain.FileDiff) map[string]string {
	seen := map[string]int{}
	out := map[string]string{}
	for _, f := range files {
		base := notSlug.ReplaceAllString(f.Path, "-")
		seen[base]++
		if seen[base] == 1 {
			out[f.Path] = base
		} else {
			out[f.Path] = base + "-" + strconv.Itoa(seen[base])
		}
	}
	return out
}

func treeRows(s Seed, zones map[string]ZoneTag) []TreeRow {
	levels := map[string][]domain.TreeEntry{}
	for _, t := range s.Trees {
		levels[t.Path] = t.Entries
	}
	open := map[string]bool{}
	for _, t := range s.Trees {
		if t.Path != "" {
			open[t.Path] = true
		}
	}
	selected := ""
	if s.File != nil {
		selected = s.File.Path
	}
	var out []TreeRow
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		entries := levels[dir]
		for i, e := range entries {
			r := TreeRow{
				Path: e.Path, Name: e.Name, Kind: e.Kind, Title: e.Path, Depth: depth, SetSize: len(entries), PosInSet: i + 1,
			}
			if e.Target != nil {
				r.Target = *e.Target
			}
			change := ""
			if e.Change != nil {
				change = *e.Change
			}
			word := ""
			if e.Kind == domain.EntryDir {
				r.Expanded = strconv.FormatBool(open[e.Path])
				if change != "" {
					r.Mark = markOf("contains")
				}
			} else {
				r.Mark = markOf(change)
				r.Href = s.Route.With(ViewFiles, e.Path).Href("")
				if len(e.Zones) > 0 {
					if z, ok := zones[e.Zones[0]]; ok {
						r.Dot = &z
					}
				}
			}
			if r.Mark != nil {
				word = ", " + strings.ToLower(r.Mark.Word)
			}
			r.Deleted = change == "deleted"
			r.Label = e.Name + word
			r.Selected = e.Path == selected
			out = append(out, r)
			if e.Kind == domain.EntryDir && open[e.Path] {
				walk(e.Path, depth+1)
			}
		}
	}
	walk("", 0)
	return out
}

func fileView(s Seed, f domain.CodeFile, zones map[string]ZoneTag) *FileView {
	v := &FileView{Path: f.Path}
	root := s.RepositoryName
	if root == "" {
		root = "root"
	}
	v.Crumbs = append(v.Crumbs, Crumb{Label: root, Href: s.Route.With(ViewFiles, "").Href("")})
	parts := strings.Split(f.Path, "/")
	for i, p := range parts {
		c := Crumb{Label: p}
		if i < len(parts)-1 {
			c.Href = s.Route.With(ViewFiles, strings.Join(parts[:i+1], "/")).Href("")
		}
		v.Crumbs = append(v.Crumbs, c)
	}
	v.Zones, v.ZoneMore = zoneTags(f.Zones, zones)
	meta := []string{languageName(f.Language)}
	if !f.Binary {
		meta = append(meta, plural(f.LineCount, "line"))
	}
	meta = append(meta, formatSize(f.Size))
	if !f.Binary && f.LineCount > 0 && (f.Size > 256*1024 || f.LineCount > 10_000) {
		meta = append(meta, "plain text: too long to highlight")
	}
	v.Meta = strings.Join(meta, " · ")
	if f.Truncated {
		v.Notice = &Notice{Title: "Showing the first 1 MiB", Text: count(f.LineCount) + " lines. The rest of this " + formatSize(f.Size) + " file is not loaded.", Path: f.Path}
	}
	switch {
	case f.Binary:
		v.Placeholder = &Placeholder{Title: "Binary file, not shown", Text: formatSize(f.Size)}
	case f.Content == nil || *f.Content == "":
		v.Placeholder = &Placeholder{Title: "Empty file", Text: path.Base(f.Path) + " has no content."}
	default:
		lines := strings.Split(strings.TrimSuffix(*f.Content, "\n"), "\n")
		v.Digits = digits(len(lines))
		for i, l := range lines {
			if i >= paintFileLines {
				break
			}
			v.Lines = append(v.Lines, Line{N: i + 1, Text: strings.TrimSuffix(l, "\r")})
		}
	}
	return v
}

// zoneIndex names each zone and gives it its context's hue: the context's
// index in the map mod 8, "none" for a zone in no context (zones.js).
func zoneIndex(m *domain.CodeMap) map[string]ZoneTag {
	out := map[string]ZoneTag{}
	if m == nil {
		return out
	}
	for i, c := range m.Contexts {
		for _, z := range c.Zones {
			if _, ok := out[z.ID]; !ok {
				out[z.ID] = ZoneTag{Name: z.Name, Hue: strconv.Itoa(i % 8), Title: "Zone · " + c.Name + " context"}
			}
		}
	}
	for _, z := range m.UnassignedZones {
		if _, ok := out[z.ID]; !ok {
			out[z.ID] = ZoneTag{Name: z.Name, Hue: "none", Title: "Zone · no bounded context"}
		}
	}
	return out
}

func zoneTags(ids []string, zones map[string]ZoneTag) ([]ZoneTag, *ZoneTag) {
	var known []ZoneTag
	for _, id := range ids {
		if z, ok := zones[id]; ok {
			known = append(known, z)
		}
	}
	if len(known) <= 2 {
		return known, nil
	}
	names := make([]string, 0, len(known)-2)
	for _, z := range known[2:] {
		names = append(names, z.Name)
	}
	return known[:2], &ZoneTag{Name: "+" + strconv.Itoa(len(known)-2), Title: strings.Join(names, ", ")}
}

func mapView(s Seed, m domain.CodeMap) *MapView {
	v := &MapView{}
	largest := 1
	zoneCount := len(m.UnassignedZones)
	for _, c := range m.Contexts {
		zoneCount += len(c.Zones)
		for _, z := range c.Zones {
			largest = max(largest, z.FileCount)
		}
	}
	for _, z := range m.UnassignedZones {
		largest = max(largest, z.FileCount)
	}
	v.Empty = zoneCount == 0
	zoneRow := func(z domain.ZoneCount) MapZone {
		r := MapZone{Name: z.Name, Pattern: z.Pattern, Count: count(z.FileCount), Share: float64(z.FileCount) / float64(largest), Href: s.Route.With(ViewFiles, literalDir(z.Pattern)).Href("")}
		if z.FileCount == 0 {
			r.Count, r.Empty = "no files", true
		}
		return r
	}
	for i, c := range m.Contexts {
		card := MapCard{ID: c.ID, Hue: strconv.Itoa(i % 8), Name: c.Name, Purpose: c.Purpose, Count: plural(c.FileCount, "file")}
		for _, z := range c.Zones {
			card.Zones = append(card.Zones, zoneRow(z))
		}
		v.Cards = append(v.Cards, card)
		if c.FileCount > 0 {
			v.Spread = append(v.Spread, MapSegment{Hue: card.Hue, Share: c.FileCount})
		}
	}
	if len(m.UnassignedZones) > 0 {
		n := 0
		card := MapCard{ID: "unassigned", Kind: "unassigned", Hue: "none", Name: "No bounded context"}
		for _, z := range m.UnassignedZones {
			n += z.FileCount
			card.Zones = append(card.Zones, zoneRow(z))
		}
		card.Count = plural(n, "file")
		v.Cards = append(v.Cards, card)
		if n > 0 {
			v.Spread = append(v.Spread, MapSegment{Hue: "none", Share: n})
		}
	}
	if m.UnzonedFileCount > 0 {
		v.Cards = append(v.Cards, MapCard{ID: "unzoned", Kind: "unzoned", Hue: "none", Name: "Unzoned files", Count: count(m.UnzonedFileCount)})
		v.Spread = append(v.Spread, MapSegment{Hue: "none", Kind: "unzoned", Share: m.UnzonedFileCount})
	}
	v.Totals = plural(m.FileCount, "file") + " · " + plural(len(m.Contexts), "context") + " · " + plural(zoneCount, "zone") + " · " + count(m.UnzonedFileCount) + " unzoned"
	return v
}

// literalDir is the longest directory a pattern names literally (zones.js).
func literalDir(pattern string) string {
	segments := strings.Split(strings.TrimPrefix(strings.TrimPrefix(pattern, "./"), "/"), "/")
	var out []string
	for _, s := range segments {
		if s == "" || strings.ContainsAny(s, "*?[]{}!") {
			break
		}
		out = append(out, s)
	}
	if len(out) == len(segments) && len(out) > 0 && strings.Contains(out[len(out)-1], ".") {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "/")
}

func short(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}

// count writes 24310 as "24,310".
func count(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}

func plural(n int, one string) string {
	if n == 1 {
		return count(n) + " " + one
	}
	return count(n) + " " + one + "s"
}

func formatSize(b int64) string {
	if b < 1024 {
		return strconv.FormatInt(b, 10) + " B"
	}
	units := []string{"KB", "MB", "GB"}
	n := float64(b) / 1024
	u := 0
	for n >= 1024 && u < len(units)-1 {
		n /= 1024
		u++
	}
	if n < 10 {
		return strconv.FormatFloat(n, 'f', 1, 64) + " " + units[u]
	}
	return strconv.Itoa(int(n+0.5)) + " " + units[u]
}

var languageNames = map[string]string{
	"go": "Go", "templ": "templ", "javascript": "JavaScript", "jsx": "JSX", "typescript": "TypeScript", "tsx": "TSX",
	"json": "JSON", "markdown": "Markdown", "html": "HTML", "css": "CSS", "scss": "SCSS", "sass": "Sass", "less": "Less",
	"yaml": "YAML", "toml": "TOML", "xml": "XML", "sql": "SQL", "shell": "Shell", "python": "Python", "ruby": "Ruby",
	"rust": "Rust", "java": "Java", "kotlin": "Kotlin", "swift": "Swift", "c": "C", "cpp": "C++", "csharp": "C#",
	"php": "PHP", "lua": "Lua", "protobuf": "Protobuf", "graphql": "GraphQL", "dockerfile": "Dockerfile",
	"makefile": "Makefile", "gomod": "go.mod", "plaintext": "Plain text",
}

func languageName(id string) string {
	if n, ok := languageNames[id]; ok {
		return n
	}
	if id == "" {
		return "Plain text"
	}
	return strings.ToUpper(id[:1]) + id[1:]
}
