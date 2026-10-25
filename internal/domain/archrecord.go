package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ArchitectureRecordVersion is the schema version of ArchitectureRecord.
const ArchitectureRecordVersion = 1

// ArchitectureRecord is a repository's architecture: its layers, who may
// change them, the rules that guard them, and four descriptive parts the
// harness stores and shows but does not enforce. It is the one shape of the
// web API's JSON, of .harness/architecture.yaml and of the read-only file in
// the virtual shell. Field order is the YAML's key order.
type ArchitectureRecord struct {
	Version       int               `json:"version" yaml:"version"`
	Repository    string            `json:"repository" yaml:"repository"`
	Pattern       string            `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	Instructions  []ArchInstruction `json:"instructions" yaml:"instructions"`
	Tools         []ArchTool        `json:"tools" yaml:"tools"`
	Sandboxes     []ArchSandbox     `json:"sandboxes" yaml:"sandboxes"`
	Orchestration ArchOrchestration `json:"orchestration" yaml:"orchestration"`
	Layers        []ArchLayer       `json:"layers" yaml:"layers"`
	Unlayered     LayerAccess       `json:"unlayered" yaml:"unlayered"`
	Rules         []ArchRule        `json:"rules" yaml:"rules"`
}

// ArchInstruction is an instruction or rule file the repository holds. The
// prose stays in the file: only its path is recorded.
type ArchInstruction struct {
	Path      string   `json:"path" yaml:"path"`
	Kind      string   `json:"kind" yaml:"kind"` // rules | skill | subagent_prompt
	AppliesTo []string `json:"applies_to" yaml:"applies_to"`
	Loaded    string   `json:"loaded" yaml:"loaded"` // always | on_demand
	Note      string   `json:"note,omitempty" yaml:"note,omitempty"`
}

// ArchTool is a tool agents can call.
type ArchTool struct {
	Name   string   `json:"name" yaml:"name"`
	Kind   string   `json:"kind" yaml:"kind"` // mcp | function | api | cli
	UsedBy []string `json:"used_by" yaml:"used_by"`
	Writes bool     `json:"writes" yaml:"writes"`
	Usage  string   `json:"usage,omitempty" yaml:"usage,omitempty"`
}

// ArchSandbox is an execution environment.
type ArchSandbox struct {
	Name        string   `json:"name" yaml:"name"`
	Runs        string   `json:"runs,omitempty" yaml:"runs,omitempty"`
	CanReach    []string `json:"can_reach" yaml:"can_reach"`
	CannotReach []string `json:"cannot_reach" yaml:"cannot_reach"`
	UsedBy      []string `json:"used_by" yaml:"used_by"`
}

// ArchOrchestration is how agents are routed and hand work to each other.
type ArchOrchestration struct {
	Routing  []ArchRoute   `json:"routing" yaml:"routing"`
	Handoffs []ArchHandoff `json:"handoffs" yaml:"handoffs"`
}

// ArchRoute says which model an agent runs on.
type ArchRoute struct {
	Agent string `json:"agent" yaml:"agent"`
	Model string `json:"model" yaml:"model"`
	Note  string `json:"note,omitempty" yaml:"note,omitempty"`
}

// ArchHandoff says when one agent hands work to another.
type ArchHandoff struct {
	From string `json:"from" yaml:"from"`
	To   string `json:"to" yaml:"to"`
	When string `json:"when" yaml:"when"`
}

// ArchLayerMatch picks a layer's paths: exactly one of Folder and Regex.
type ArchLayerMatch struct {
	Folder string `json:"folder,omitempty" yaml:"folder,omitempty"`
	Regex  string `json:"regex,omitempty" yaml:"regex,omitempty"`
}

// ArchLayer is a bounded context of the repository's code: the paths it
// holds, and what each agent may do to them.
type ArchLayer struct {
	Name           string         `json:"name" yaml:"name"`
	Match          ArchLayerMatch `json:"match" yaml:"match"`
	BoundedContext string         `json:"bounded_context,omitempty" yaml:"bounded_context,omitempty"`
	Purpose        string         `json:"purpose,omitempty" yaml:"purpose,omitempty"`
	Access         LayerAccess    `json:"access" yaml:"access"`
}

// Rule kinds, lifecycle points and modes.
const (
	RuleEditOutsideLayer = "edit_outside_layer"
	RuleCommand          = "command"

	PointBeforeTool   = "before_tool"
	PointAfterEdit    = "after_edit"
	PointBeforeCommit = "before_commit"

	ModeBlocking = "blocking"
	ModeWarning  = "warning"

	RuleDefaultTimeoutSeconds = 30
	RuleMaxTimeoutSeconds     = 300
)

// ArchRule is an architecture rule: the harness's hooks run it at a
// lifecycle point and block or warn when it fails.
type ArchRule struct {
	ID             string   `json:"id" yaml:"id"`
	Kind           string   `json:"kind" yaml:"kind"`
	At             string   `json:"at,omitempty" yaml:"at,omitempty"`
	Mode           string   `json:"mode" yaml:"mode"`
	Message        string   `json:"message" yaml:"message"`
	Agents         []string `json:"agents,omitempty" yaml:"agents,omitempty"`
	Command        string   `json:"command,omitempty" yaml:"command,omitempty"`
	Paths          string   `json:"paths,omitempty" yaml:"paths,omitempty"`
	Tools          []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
}

// ArchFieldError is one problem of a refused record: a JSON pointer to the
// field, a code and a message.
type ArchFieldError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Validation codes of a refused record.
const (
	ArchDuplicateName    = "DUPLICATE_NAME"
	ArchInvalidRegex     = "INVALID_REGEX"
	ArchInvalidFolder    = "INVALID_FOLDER"
	ArchUnknownAgent     = "UNKNOWN_AGENT"
	ArchUnknownContext   = "UNKNOWN_BOUNDED_CONTEXT"
	ArchMissingField     = "MISSING_FIELD"
	ArchInvalidValue     = "INVALID_VALUE"
	ArchTooMany          = "TOO_MANY"
	MaxArchitectureItems = 100
)

// ArchitectureKnown is what a record's names are checked against.
type ArchitectureKnown struct {
	// Agents are the harness agent catalog's names; PlainAgent is always
	// known.
	Agents map[string]bool
	// BoundedContexts are the project's bounded contexts' names.
	BoundedContexts map[string]bool
}

// StarterArchitecture is the record offered for a repository with none: every
// agent reads the unlayered paths, software-architect and devops (when the
// catalog has them) change them, and the one edit-outside-layer rule only
// warns.
func StarterArchitecture(repository string, knownAgents map[string]bool) ArchitectureRecord {
	un := LayerAccess{Default: AccessRead, Agents: map[string]AccessLevel{}}
	for _, a := range []string{"software-architect", "devops"} {
		if knownAgents[a] {
			un.Agents[a] = AccessChange
		}
	}
	r := ArchitectureRecord{
		Version:    ArchitectureRecordVersion,
		Repository: repository,
		Unlayered:  un,
		Rules: []ArchRule{{
			ID: "stay-in-your-layer", Kind: RuleEditOutsideLayer, Mode: ModeWarning,
			Message: DefaultEditOutsideLayerMessage,
		}},
	}
	r.Normalize()
	return r
}

// DefaultEditOutsideLayerMessage is the text of the edit-outside-layer rule a
// record has no rule for.
const DefaultEditOutsideLayerMessage = "You may only change files in the layers your agent owns. Ask the architect."

// NewLayerAccess is the access of a new layer: everyone reads, the
// software-architect changes.
func NewLayerAccess() LayerAccess {
	return LayerAccess{Default: AccessRead, Agents: map[string]AccessLevel{"software-architect": AccessChange}}
}

// Normalize makes the record's lists non-nil and gives an unset access
// default the level read, so equal records are equal in JSON and YAML.
func (r *ArchitectureRecord) Normalize() {
	r.Version = ArchitectureRecordVersion
	if r.Instructions == nil {
		r.Instructions = []ArchInstruction{}
	}
	for i := range r.Instructions {
		r.Instructions[i].AppliesTo = nonNil(r.Instructions[i].AppliesTo)
	}
	if r.Tools == nil {
		r.Tools = []ArchTool{}
	}
	for i := range r.Tools {
		r.Tools[i].UsedBy = nonNil(r.Tools[i].UsedBy)
	}
	if r.Sandboxes == nil {
		r.Sandboxes = []ArchSandbox{}
	}
	for i := range r.Sandboxes {
		s := &r.Sandboxes[i]
		s.CanReach, s.CannotReach, s.UsedBy = nonNil(s.CanReach), nonNil(s.CannotReach), nonNil(s.UsedBy)
	}
	if r.Orchestration.Routing == nil {
		r.Orchestration.Routing = []ArchRoute{}
	}
	if r.Orchestration.Handoffs == nil {
		r.Orchestration.Handoffs = []ArchHandoff{}
	}
	if r.Layers == nil {
		r.Layers = []ArchLayer{}
	}
	for i := range r.Layers {
		r.Layers[i].Match.Folder = strings.TrimSuffix(r.Layers[i].Match.Folder, "/")
		normalizeAccess(&r.Layers[i].Access)
	}
	normalizeAccess(&r.Unlayered)
	if r.Rules == nil {
		r.Rules = []ArchRule{}
	}
}

func normalizeAccess(a *LayerAccess) {
	if a.Default == "" {
		a.Default = AccessRead
	}
	if len(a.Agents) == 0 {
		a.Agents = nil
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

var nameRE = regexp.MustCompile(`^[a-z0-9-]+$`)

// Validate lists every problem of the record against known: none when it is
// valid. Warnings (an instruction file that is missing, a layer that matches
// no file, overlapping layers) are not errors; the layer preview reports them.
func (r *ArchitectureRecord) Validate(known ArchitectureKnown) []ArchFieldError {
	var errs []ArchFieldError
	add := func(path, code, format string, a ...any) {
		errs = append(errs, ArchFieldError{Path: path, Code: code, Message: fmt.Sprintf(format, a...)})
	}
	agent := func(path, name string) {
		if name != PlainAgent && !known.Agents[name] {
			add(path, ArchUnknownAgent, "%q is not an agent of the catalog", name)
		}
	}
	agentList := func(path string, names []string) {
		for i, n := range names {
			if n != "all" {
				agent(fmt.Sprintf("%s/%d", path, i), n)
			}
		}
	}
	access := func(path string, a LayerAccess) {
		if a.Default != "" && !a.Default.Valid() {
			add(path+"/default", ArchInvalidValue, "%q is not none, read or change", a.Default)
		}
		for _, n := range sortedKeys(a.Agents) {
			ep := path + "/agents/" + pointerEscape(n)
			agent(ep, n)
			if !a.Agents[n].Valid() {
				add(ep, ArchInvalidValue, "%q is not none, read or change", a.Agents[n])
			}
		}
	}

	if r.Version != 0 && r.Version != ArchitectureRecordVersion {
		add("/version", ArchInvalidValue, "version must be %d", ArchitectureRecordVersion)
	}
	for i, in := range r.Instructions {
		p := fmt.Sprintf("/instructions/%d", i)
		if in.Path == "" {
			add(p+"/path", ArchMissingField, "path is required")
		}
		if !oneOf(in.Kind, "rules", "skill", "subagent_prompt") {
			add(p+"/kind", ArchInvalidValue, "kind must be rules, skill or subagent_prompt")
		}
		if !oneOf(in.Loaded, "always", "on_demand") {
			add(p+"/loaded", ArchInvalidValue, "loaded must be always or on_demand")
		}
		agentList(p+"/applies_to", in.AppliesTo)
	}
	for i, t := range r.Tools {
		p := fmt.Sprintf("/tools/%d", i)
		if t.Name == "" {
			add(p+"/name", ArchMissingField, "name is required")
		}
		if !oneOf(t.Kind, "mcp", "function", "api", "cli") {
			add(p+"/kind", ArchInvalidValue, "kind must be mcp, function, api or cli")
		}
		if t.Writes && strings.TrimSpace(t.Usage) == "" {
			add(p+"/usage", ArchMissingField, "usage is required for a tool that writes")
		}
		agentList(p+"/used_by", t.UsedBy)
	}
	for i, s := range r.Sandboxes {
		p := fmt.Sprintf("/sandboxes/%d", i)
		if s.Name == "" {
			add(p+"/name", ArchMissingField, "name is required")
		}
		agentList(p+"/used_by", s.UsedBy)
	}
	for i, rt := range r.Orchestration.Routing {
		p := fmt.Sprintf("/orchestration/routing/%d", i)
		if rt.Agent == "" {
			add(p+"/agent", ArchMissingField, "agent is required")
		} else {
			agent(p+"/agent", rt.Agent)
		}
		if rt.Model == "" {
			add(p+"/model", ArchMissingField, "model is required")
		}
	}
	for i, h := range r.Orchestration.Handoffs {
		p := fmt.Sprintf("/orchestration/handoffs/%d", i)
		for field, name := range map[string]string{"from": h.From, "to": h.To} {
			if name == "" {
				add(p+"/"+field, ArchMissingField, "%s is required", field)
			} else {
				agent(p+"/"+field, name)
			}
		}
		if h.When == "" {
			add(p+"/when", ArchMissingField, "when is required")
		}
	}

	if len(r.Layers) > MaxArchitectureItems {
		add("/layers", ArchTooMany, "at most %d layers", MaxArchitectureItems)
	}
	seen := map[string]bool{}
	for i, l := range r.Layers {
		p := fmt.Sprintf("/layers/%d", i)
		switch {
		case l.Name == "":
			add(p+"/name", ArchMissingField, "name is required")
		case !nameRE.MatchString(l.Name):
			add(p+"/name", ArchInvalidValue, "name must be lowercase letters, digits and dashes")
		case seen[l.Name]:
			add(p+"/name", ArchDuplicateName, "another layer is named %q", l.Name)
		}
		seen[l.Name] = true
		switch {
		case l.Match.Folder == "" && l.Match.Regex == "":
			add(p+"/match", ArchMissingField, "a layer matches a folder or a regex")
		case l.Match.Folder != "" && l.Match.Regex != "":
			add(p+"/match", ArchInvalidValue, "a layer matches a folder or a regex, not both")
		case l.Match.Regex != "":
			if _, err := regexp.Compile(l.Match.Regex); err != nil {
				add(p+"/match/regex", ArchInvalidRegex, "%v", err)
			}
		default:
			if msg := badFolder(l.Match.Folder); msg != "" {
				add(p+"/match/folder", ArchInvalidFolder, "%s", msg)
			}
		}
		if l.BoundedContext != "" && !known.BoundedContexts[l.BoundedContext] {
			add(p+"/bounded_context", ArchUnknownContext, "%q is not a bounded context of the project", l.BoundedContext)
		}
		access(p+"/access", l.Access)
	}
	access("/unlayered", r.Unlayered)

	if len(r.Rules) > MaxArchitectureItems {
		add("/rules", ArchTooMany, "at most %d rules", MaxArchitectureItems)
	}
	ids := map[string]bool{}
	outside := 0
	for i, rl := range r.Rules {
		p := fmt.Sprintf("/rules/%d", i)
		switch {
		case rl.ID == "":
			add(p+"/id", ArchMissingField, "id is required")
		case !nameRE.MatchString(rl.ID):
			add(p+"/id", ArchInvalidValue, "id must be lowercase letters, digits and dashes")
		case ids[rl.ID]:
			add(p+"/id", ArchDuplicateName, "another rule has the id %q", rl.ID)
		}
		ids[rl.ID] = true
		if !oneOf(rl.Mode, ModeBlocking, ModeWarning) {
			add(p+"/mode", ArchInvalidValue, "mode must be blocking or warning")
		}
		if strings.TrimSpace(rl.Message) == "" {
			add(p+"/message", ArchMissingField, "message is required")
		}
		agentList(p+"/agents", rl.Agents)
		switch rl.Kind {
		case RuleEditOutsideLayer:
			if outside++; outside > 1 {
				add(p, ArchTooMany, "at most one edit_outside_layer rule")
			}
		case RuleCommand:
			if rl.At == "" {
				add(p+"/at", ArchMissingField, "at is required")
			} else if !oneOf(rl.At, PointBeforeTool, PointAfterEdit, PointBeforeCommit) {
				add(p+"/at", ArchInvalidValue, "at must be before_tool, after_edit or before_commit")
			}
			if strings.TrimSpace(rl.Command) == "" {
				add(p+"/command", ArchMissingField, "command is required")
			}
			if rl.Paths != "" {
				if _, err := regexp.Compile(rl.Paths); err != nil {
					add(p+"/paths", ArchInvalidRegex, "%v", err)
				}
			}
			if rl.TimeoutSeconds < 0 || rl.TimeoutSeconds > RuleMaxTimeoutSeconds {
				add(p+"/timeout_seconds", ArchInvalidValue, "timeout_seconds must be between 1 and %d", RuleMaxTimeoutSeconds)
			}
		default:
			add(p+"/kind", ArchInvalidValue, "kind must be edit_outside_layer or command")
		}
	}
	return errs
}

func badFolder(f string) string {
	switch {
	case strings.HasPrefix(f, "/"):
		return "a folder is relative to the repository: no leading /"
	}
	for _, seg := range strings.Split(f, "/") {
		if seg == ".." {
			return "a folder cannot contain .."
		}
	}
	if f == ".git" || strings.HasPrefix(f, ".git/") {
		return "a folder cannot be inside .git"
	}
	return ""
}

func oneOf(s string, options ...string) bool {
	for _, o := range options {
		if s == o {
			return true
		}
	}
	return false
}

func pointerEscape(s string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(s)
}

func sortedKeys(m map[string]AccessLevel) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// EditOutsideLayerRule is the record's edit-outside-layer rule, or the
// default warning rule when it has none.
func (r *ArchitectureRecord) EditOutsideLayerRule() ArchRule {
	for _, rl := range r.Rules {
		if rl.Kind == RuleEditOutsideLayer {
			return rl
		}
	}
	return ArchRule{ID: "stay-in-your-layer", Kind: RuleEditOutsideLayer, Mode: ModeWarning, Message: DefaultEditOutsideLayerMessage}
}

// LayerZones are the record's layers as zones that belong to the repository,
// in the layers' order and without ids: a folder is an explicit path, a regex
// a pattern. The caller sets the ids and the bounded context ids.
func (r *ArchitectureRecord) LayerZones(repositoryID, projectID string) []*Zone {
	zones := make([]*Zone, len(r.Layers))
	for i, l := range r.Layers {
		a := l.Access
		z := &Zone{
			ProjectID: projectID, RepositoryID: repositoryID, Name: l.Name, Purpose: l.Purpose,
			Position: i, Access: &a, Rules: []Prompt{}, AssignedAgents: []Agent{},
		}
		if l.Match.Regex != "" {
			z.Pattern = l.Match.Regex
		} else {
			z.ExplicitPaths = []string{l.Match.Folder}
		}
		zones[i] = z
	}
	return zones
}

// CompiledArchitecture is a record ready to answer which layer a path is in
// and what an agent may do there, with the layers' regexes compiled once.
type CompiledArchitecture struct {
	Record  *ArchitectureRecord
	matcher *ZoneMatcher
}

// Compile prepares the record for lookups.
func (r *ArchitectureRecord) Compile() *CompiledArchitecture {
	return &CompiledArchitecture{Record: r, matcher: NewZoneMatcher(r.LayerZones("", ""))}
}

// LayerOf is the index of the layer path belongs to (the narrowest wins), or
// -1 when it is in none.
func (c *CompiledArchitecture) LayerOf(path string) int { return LayerIndex(c.matcher, path) }

// Matching lists the layers path belongs to, by index.
func (c *CompiledArchitecture) Matching(path string) []int { return c.matcher.Matching(path) }

// AccessOf is what agent may do to path, and the layer's name ("" for a path
// in no layer).
func (c *CompiledArchitecture) AccessOf(agent, path string) (AccessLevel, string) {
	if i := c.LayerOf(path); i >= 0 {
		return c.Record.Layers[i].Access.For(agent), c.Record.Layers[i].Name
	}
	return c.Record.Unlayered.For(agent), ""
}

// RepositoryArchitecture is a repository's stored record as the API answers
// it.
type RepositoryArchitecture struct {
	RepositoryID string
	ProjectID    string
	Exists       bool
	Version      int
	UpdatedAt    time.Time
	Record       ArchitectureRecord
	Export       ArchitectureExport
}

// Export states of the architecture file in the repository.
const (
	ExportNeverExported  = "never_exported"
	ExportInSync         = "in_sync"
	ExportDatabaseAhead  = "database_ahead"
	ExportEditedOutside  = "edited_outside"
	ExportFileInvalid    = "file_invalid"
	ArchitectureFilePath = ".harness/architecture.yaml"
)

// ArchitectureExport says whether the file in the repository matches the
// record.
type ArchitectureExport struct {
	State           string     `json:"state"`
	Path            string     `json:"path"`
	ExportedVersion int        `json:"exported_version,omitempty"`
	ExportedCommit  string     `json:"exported_commit,omitempty"`
	ExportedAt      *time.Time `json:"exported_at,omitempty"`
}

// ArchitectureExportResult is an export's commit and the state afterwards.
type ArchitectureExportResult struct {
	Commit string             `json:"commit"`
	Path   string             `json:"path"`
	Export ArchitectureExport `json:"export"`
}

// Paths the export writes.
const (
	ArchitectureSchemaPath = ".harness/architecture.schema.json"
)
