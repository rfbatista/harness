package domain

// LanguageTerm is one entry of a bounded context's ubiquitous language:
// a domain term and what it means inside that context.
type LanguageTerm struct {
	Term       string `json:"term"`
	Definition string `json:"definition"`
}

// BoundedContext models a DDD bounded context: a named area of a project's
// domain with its own purpose and vocabulary. Zones link to it via
// Zone.BoundedContextID; a zone belongs to at most one context.
type BoundedContext struct {
	ID                 string         `json:"id"`
	ProjectID          string         `json:"project_id"`
	Name               string         `json:"name"`
	Purpose            string         `json:"purpose"`
	UbiquitousLanguage []LanguageTerm `json:"ubiquitous_language"`
}
