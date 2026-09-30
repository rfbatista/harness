package domain

// Zone holds zone state (pattern, metadata, explicit paths).
// It is the core entity of the architecture context.
// A zone belongs to a project and paths are relative to that project's root.
type Zone struct {
	ID               string   `json:"id"`
	ProjectID        string   `json:"project_id"`
	BoundedContextID string   `json:"bounded_context_id,omitempty"`
	Name             string   `json:"name"`
	Pattern          string   `json:"pattern"`
	Purpose          string   `json:"purpose"`
	Rules            []Prompt `json:"rules"`
	AssignedAgents   []Agent  `json:"assigned_agents"`
	ExplicitPaths    []string `json:"explicit_paths"`
}
