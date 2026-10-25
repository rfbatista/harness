package domain

// The agent catalog's read model: the JSON shapes of the contract "Harness
// server ↔ Web UI — the agent catalog". The catalog itself is a folder in the
// harness data directory (ADR 0030); these types are what reading it yields.

// CatalogProblem is something wrong with a catalog item that does not stop
// the catalog from being read.
type CatalogProblem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Catalog problem codes.
const (
	CatalogProblemMissingSkill     = "MISSING_SKILL"
	CatalogProblemUnknownBundle    = "UNKNOWN_BUNDLE"
	CatalogProblemBundleCycle      = "BUNDLE_CYCLE"
	CatalogProblemUnknownMCPServer = "UNKNOWN_MCP_SERVER"
	CatalogProblemNoDescription    = "NO_DESCRIPTION"
	CatalogProblemSecretNotSet     = "SECRET_NOT_SET"
	CatalogProblemInvalidSkill     = "INVALID_SKILL"
)

// CatalogBroken says which catalog file does not parse, and where.
type CatalogBroken struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

// CatalogAgent is one agent of the catalog, with its skills resolved.
type CatalogAgent struct {
	Name              string           `json:"name"`
	Description       string           `json:"description"`
	Aliases           []string         `json:"aliases"`
	Entries           []string         `json:"entries"`
	ResolvedSkills    []string         `json:"resolved_skills"`
	DroppedDuplicates []string         `json:"dropped_duplicates"`
	MissingSkills     []string         `json:"missing_skills"`
	MCPServers        []string         `json:"mcp_servers"`
	AllowedTools      []string         `json:"allowed_tools"`
	Problems          []CatalogProblem `json:"problems"`
}

// CatalogBundle is a named list of skills and other bundles.
type CatalogBundle struct {
	Name           string   `json:"name"`
	Entries        []string `json:"entries"`
	ResolvedSkills []string `json:"resolved_skills"`
	UsedByAgents   []string `json:"used_by_agents"`
	UsedByBundles  []string `json:"used_by_bundles"`
}

// CatalogSkillSummary is a skill as the whole-catalog read lists it.
type CatalogSkillSummary struct {
	Name          string           `json:"name"`
	Description   string           `json:"description"`
	FileCount     int              `json:"file_count"`
	HasExtraFiles bool             `json:"has_extra_files"`
	Agents        []string         `json:"agents"`
	Bundles       []string         `json:"bundles"`
	AgentCount    int              `json:"agent_count"`
	Problems      []CatalogProblem `json:"problems"`
}

// CatalogEnvRef is a ${VAR} an MCP server names, and whether the harness
// server's environment has it. Never its value.
type CatalogEnvRef struct {
	Name string `json:"name"`
	Set  bool   `json:"set"`
}

// CatalogProjectRef names a project that gives its sessions an MCP server.
type CatalogProjectRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CatalogMCPServer is one MCP server of the catalog. Values are exactly as
// written: ${VAR} stays unexpanded.
type CatalogMCPServer struct {
	Name      string              `json:"name"`
	Transport string              `json:"transport"`
	Command   string              `json:"command"`
	Args      []string            `json:"args"`
	Env       map[string]string   `json:"env"`
	URL       string              `json:"url"`
	Headers   map[string]string   `json:"headers"`
	EnvRefs   []CatalogEnvRef     `json:"env_refs"`
	Agents    []string            `json:"agents"`
	Projects  []CatalogProjectRef `json:"projects"`
	Problems  []CatalogProblem    `json:"problems"`
}

// CatalogBuiltInTool is a Claude built-in tool an agent may be restricted to.
type CatalogBuiltInTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CatalogView is the whole catalog, for the Agents page.
type CatalogView struct {
	Root         string                `json:"root"`
	Versions     map[string]string     `json:"versions"`
	Broken       *CatalogBroken        `json:"broken"`
	Agents       []CatalogAgent        `json:"agents"`
	Bundles      []CatalogBundle       `json:"bundles"`
	Skills       []CatalogSkillSummary `json:"skills"`
	MCPServers   []CatalogMCPServer    `json:"mcp_servers"`
	BuiltInTools []CatalogBuiltInTool  `json:"built_in_tools"`
}

// CatalogSkillFile is one file of a skill other than SKILL.md.
type CatalogSkillFile struct {
	Path     string `json:"path"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// CatalogSkill is a skill with its full content.
type CatalogSkill struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Body        string             `json:"body"`
	Files       []CatalogSkillFile `json:"files"`
	Agents      []string           `json:"agents"`
	Bundles     []string           `json:"bundles"`
	AgentCount  int                `json:"agent_count"`
}

// CatalogSkillView is the answer of a skill read.
type CatalogSkillView struct {
	Skill    CatalogSkill      `json:"skill"`
	Versions map[string]string `json:"versions"`
}

// CatalogError is a refusal of the catalog with the extra fields the contract
// names beside the code (field, suggestion, names, current_versions, …). It
// unwraps to its StructuredError so every adapter sees the code and message.
type CatalogError struct {
	*StructuredError
	Fields map[string]any
}

// Unwrap returns the coded error.
func (e *CatalogError) Unwrap() error { return e.StructuredError }

// NewCatalogError is a CatalogError with code, message and extra fields.
func NewCatalogError(code, message string, fields map[string]any) *CatalogError {
	return &CatalogError{StructuredError: &StructuredError{Code: code, Message: message}, Fields: fields}
}
