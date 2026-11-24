package domain

import (
	"context"
	"time"
)

// AuthorKind says who made a change to a task or a document.
type AuthorKind string

const (
	// AuthorPerson is the person, through the web UI or the HTTP API.
	AuthorPerson AuthorKind = "person"
	// AuthorSession is an agent session, through its tools or its virtual shell.
	AuthorSession AuthorKind = "session"
	// AuthorProductOwner is a product-owner thread (ADR 0110).
	AuthorProductOwner AuthorKind = "product_owner"
	// AuthorUnknown is a task or document made before attribution was recorded.
	// The product owner treats it as the person's.
	AuthorUnknown AuthorKind = "unknown"
)

// Author is who created or last changed a task or a document, and when.
type Author struct {
	Kind      AuthorKind `json:"kind"`
	SessionID string     `json:"session_id,omitempty"`
	ThreadID  string     `json:"thread_id,omitempty"`
	At        time.Time  `json:"at"`
}

// ByThePerson reports whether a change by this author is the person's own:
// the person, or an author nobody recorded.
func (a *Author) ByThePerson() bool {
	return a == nil || a.Kind == AuthorPerson || a.Kind == AuthorUnknown || a.Kind == ""
}

type authorKey struct{}

// WithAuthor tags ctx with the author whose writes it carries. A write with
// no author in its context is the person's.
func WithAuthor(ctx context.Context, a Author) context.Context {
	return context.WithValue(ctx, authorKey{}, a)
}

// AuthorFrom is the author tagged onto ctx, if any.
func AuthorFrom(ctx context.Context) (Author, bool) {
	a, ok := ctx.Value(authorKey{}).(Author)
	return a, ok
}
