package domain

import "time"

// Document is a titled markdown body stored in the database, scoped to a Project.
// Agents read it as context; humans author it. A document links to zero or more
// Tickets via the ticket_documents join table and can exist with no ticket at all.
type Document struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Title     string    `json:"title"`
	Content   string    `json:"content,omitempty"` // markdown
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
