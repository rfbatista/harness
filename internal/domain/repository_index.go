package domain

import "time"

// RepositoryIndexDir is where codegraph keeps a repository index, relative
// to the checkout (ADR 0014).
const RepositoryIndexDir = ".codegraph"

// RepositoryIndexMaxAge is how old a repository index may grow before a
// session ending on its repository rebuilds it.
const RepositoryIndexMaxAge = 24 * time.Hour

// RepositoryIndex is one repository's codegraph index (a Repository index in
// the Glossary) as the Token savers page shows it.
type RepositoryIndex struct {
	RepositoryID   string `json:"repository_id"`
	RepositoryName string `json:"repository_name"`
	ProjectID      string `json:"project_id"`
	ProjectName    string `json:"project_name"`
	// Checkout is the repository's own checkout, which holds .codegraph/.
	Checkout string `json:"checkout"`
	// BuiltAt is when the index was last written; nil when there is none.
	BuiltAt *time.Time `json:"built_at,omitempty"`
	// Building says a build runs now.
	Building bool `json:"building"`
	// Error is why the last build failed, until the next one starts.
	Error string `json:"error,omitempty"`
}
