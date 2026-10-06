package domain

import (
	"strings"
	"time"
)

// DocumentFormat says what a Document's body is: Markdown (what people and
// the project-level API write by default) or a complete HTML page (what
// task sessions write). Rendering depends on it.
type DocumentFormat string

const (
	DocumentFormatMarkdown DocumentFormat = "markdown"
	DocumentFormatHTML     DocumentFormat = "html"
)

// Valid reports whether f is one of the two formats. The empty string is not
// valid: callers that mean "default" or "keep" handle it before asking.
func (f DocumentFormat) Valid() bool {
	return f == DocumentFormatMarkdown || f == DocumentFormatHTML
}

// DocumentScope says who a Document is for. A task document belongs to the
// task(s) it is linked to: what task sessions write by default. A project
// document is the project's own: architecture, conventions, decisions, specs
// and contracts that outlive one task. Every session of the project can read
// it, and it shows on the project's documents page. Moving a document between
// the two never touches its ticket links, so the task it came from keeps it.
type DocumentScope string

const (
	DocumentScopeTask    DocumentScope = "task"
	DocumentScopeProject DocumentScope = "project"
)

// Valid reports whether s is one of the two scopes. The empty string is not
// valid: callers that mean "default" or "every scope" handle it before asking.
func (s DocumentScope) Valid() bool {
	return s == DocumentScopeTask || s == DocumentScopeProject
}

// Document is a titled body stored in the database, scoped to a Project, in
// the format Format says. Scope says whether it is the task's or the project's. Agents read it as context; task sessions write it
// as HTML pages, people and API clients as Markdown. A document links to zero
// or more Tickets via the ticket_documents join table and can exist with no
// ticket at all.
type Document struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id"`
	Title     string         `json:"title"`
	Format    DocumentFormat `json:"format"`
	Scope     DocumentScope  `json:"scope"`
	Content   string         `json:"content,omitempty"` // in Format
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// IsHTMLDocument reports whether content is a complete HTML document rather
// than plain text, Markdown or a bare fragment: after optional whitespace or
// a byte-order mark it opens with a doctype or <html>, and it has <html> and
// <body> elements. It is deliberately a shape check, not a parse: the page
// is rendered by a browser inside a sandbox, which copes with the rest.
func IsHTMLDocument(content string) bool {
	s := strings.ToLower(strings.TrimLeft(content, "\uFEFF \t\r\n"))
	if !strings.HasPrefix(s, "<!doctype") && !strings.HasPrefix(s, "<html") {
		return false
	}
	return strings.Contains(s, "<html") && strings.Contains(s, "<body")
}
