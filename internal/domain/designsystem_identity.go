package domain

import (
	"time"
	"unicode/utf8"
)

// MaxDesignSystemNameLength is the longest name of a design system.
const MaxDesignSystemNameLength = 60

// DesignSystemIdentity is what names a design system in a project (ADR 0060):
// a name and a slug that are unique in the project, and a one-line
// description. The slug is chosen at creation and never changes.
type DesignSystemIdentity struct {
	ID          string
	ProjectID   string
	Name        string
	Slug        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// DesignSystemSummary is a design system as the project's list shows it: its
// latest version, and UpdatedAt as the later of the identity's last change
// and that version.
type DesignSystemSummary struct {
	DesignSystemIdentity
	Version int
}

// ValidateDesignSystemName lists what is wrong with a name (already trimmed).
func ValidateDesignSystemName(name string) []DesignFieldError {
	if n := utf8.RuneCountInString(name); n < 1 || n > MaxDesignSystemNameLength {
		return []DesignFieldError{{Path: "/name", Code: DesignBadName, Message: "the name must be 1 to 60 characters"}}
	}
	return nil
}

// ValidateDesignSystemSlug lists what is wrong with a slug.
func ValidateDesignSystemSlug(slug string) []DesignFieldError {
	if !designNamePattern.MatchString(slug) {
		return []DesignFieldError{{Path: "/slug", Code: DesignBadName, Message: "the slug must match ^[a-z][a-z0-9-]*$"}}
	}
	return nil
}
