package domain

// Prompt represents a reusable prompt template that can be referenced by agents.
type Prompt struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Content     string `json:"content,omitempty"`
}
