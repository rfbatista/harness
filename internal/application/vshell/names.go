package vshell

import (
	"mime"
	"path"
	"strings"

	"operators-mcp/internal/domain"
)

// The names the harness gives the files it writes (contract §2).

// DocumentFileName is <slug of title>.<ext>, the extension from the format:
// html .html, markdown .md, anything else .txt.
func DocumentFileName(title string, format domain.DocumentFormat) string {
	return slugOr(title, "untitled") + DocumentExt(format)
}

// DocumentExt is the extension a document of format is written with.
func DocumentExt(format domain.DocumentFormat) string {
	switch format {
	case domain.DocumentFormatHTML:
		return ".html"
	case domain.DocumentFormatMarkdown:
		return ".md"
	}
	return ".txt"
}

// DocumentFormatOf is the format a document file at p is in, by its
// extension: .html/.htm html, .md markdown, anything else text.
func DocumentFormatOf(p string) domain.DocumentFormat {
	switch strings.ToLower(path.Ext(p)) {
	case ".html", ".htm":
		return domain.DocumentFormatHTML
	case ".md":
		return domain.DocumentFormatMarkdown
	}
	return domain.DocumentFormatText
}

// CommentFileName is <yyyymmdd-hhmmss>-<author>-<short id>.md, the time
// the comment was written, in UTC.
func CommentFileName(c *domain.TicketComment) string {
	return c.CreatedAt.UTC().Format("20060102-150405") + "-" + CommentAuthor(c) + "-" + ShortID(c.ID) + ".md"
}

// CommentAuthor is person, or the slug of the writing session's agent name
// (claude for a session with no agent).
func CommentAuthor(c *domain.TicketComment) string {
	if c.Author != domain.CommentBySession {
		return string(domain.CommentByPerson)
	}
	return slugOr(c.AgentName, "claude")
}

// ResourceFileName is the upload's name as the person gave it, made safe
// for a file name, with the extension its type has when it has none.
func ResourceFileName(r *domain.TicketResource) string {
	name := strings.TrimSpace(strings.NewReplacer("/", "-", "\\", "-", "\x00", "").Replace(r.Title))
	name = strings.TrimLeft(name, ".")
	if name == "" {
		name = "file"
	}
	if path.Ext(name) == "" {
		if exts, _ := mime.ExtensionsByType(domain.MediaType(r.Mime)); len(exts) > 0 {
			name += exts[0]
		}
	}
	return name
}

// ArtifactFileName is the artifact's path in the worktree it was published
// from, kept relative; one without a usable path is named after its title.
func ArtifactFileName(a *domain.Artifact) string {
	if p := cleanRel(a.Path); p != "" {
		return p
	}
	return slugOr(a.Title, "artifact")
}

// ShortID is the first 8 characters of id.
func ShortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// Unique is p, or, when taken says p is in use, p with -<short id> before
// its extension (the whole id if that is taken too).
func Unique(p, id string, taken func(string) bool) string {
	if !taken(p) {
		return p
	}
	ext := path.Ext(p)
	base := strings.TrimSuffix(p, ext)
	if q := base + "-" + ShortID(id) + ext; !taken(q) {
		return q
	}
	return base + "-" + id + ext
}

func slugOr(s, fallback string) string {
	if slug := domain.Slug(s); slug != "" {
		return slug
	}
	return fallback
}
