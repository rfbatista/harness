package domain

import (
	"encoding/json"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// ArtifactKind is how the Design View renders an artifact.
type ArtifactKind string

const (
	ArtifactPage  ArtifactKind = "page"
	ArtifactImage ArtifactKind = "image"
	ArtifactVideo ArtifactKind = "video"
	ArtifactURL   ArtifactKind = "url"
	ArtifactFile  ArtifactKind = "file"
)

// ParseArtifactKind reads a kind as a publisher sends it; empty means infer.
func ParseArtifactKind(s string) (ArtifactKind, error) {
	switch k := ArtifactKind(s); k {
	case "", ArtifactPage, ArtifactImage, ArtifactVideo, ArtifactURL, ArtifactFile:
		return k, nil
	default:
		return "", &StructuredError{Code: "INVALID_INPUT", Message: "unknown artifact kind " + s + ": use page, image, video, url or file"}
	}
}

// ArtifactScope says who an Artifact is for, the way DocumentScope does for
// documents. A task artifact belongs to the session that published it and
// goes with it. A project artifact is a design asset the project keeps: the
// harness holds its own copy of the bytes, so it outlives the session and
// its worktree.
type ArtifactScope string

const (
	ArtifactScopeTask    ArtifactScope = "task"
	ArtifactScopeProject ArtifactScope = "project"
)

// Valid reports whether s is one of the two scopes. The empty string is not
// valid: callers that mean "default" or "every scope" handle it before asking.
func (s ArtifactScope) Valid() bool {
	return s == ArtifactScopeTask || s == ArtifactScopeProject
}

// Artifact is something a session produced and explicitly published for the
// user to look at: a file in the session worktree, or a URL to a dev server
// on this machine. One artifact per (session, path) or (session, url);
// re-publishing bumps Revision and keeps the ID.
type Artifact struct {
	ID        string
	SessionID string
	TicketID  string
	ProjectID string
	Kind      ArtifactKind
	Title     string
	Note      string // what changed in this revision; may be empty
	Path      string // worktree-relative, POSIX separators; empty when Kind == ArtifactURL
	URL       string // only when Kind == ArtifactURL
	Mime      string // "" when Kind == ArtifactURL
	SizeBytes int64  // 0 when Kind == ArtifactURL
	Revision  int    // starts at 1
	Scope     ArtifactScope
	// Snapshot is set once the harness holds its own copy of the artifact's
	// directory; its bytes are served from that copy from then on. Not on the wire.
	Snapshot  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// artifactJSON is the wire shape of the Artifacts contract: path and url are
// null, never "", when absent.
type artifactJSON struct {
	ID        string        `json:"id"`
	SessionID string        `json:"session_id"`
	TicketID  string        `json:"ticket_id"`
	ProjectID string        `json:"project_id"`
	Kind      ArtifactKind  `json:"kind"`
	Title     string        `json:"title"`
	Note      string        `json:"note"`
	Path      *string       `json:"path"`
	URL       *string       `json:"url"`
	Mime      string        `json:"mime"`
	SizeBytes int64         `json:"size_bytes"`
	Revision  int           `json:"revision"`
	Scope     ArtifactScope `json:"scope"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func (a Artifact) MarshalJSON() ([]byte, error) {
	w := artifactJSON{ID: a.ID, SessionID: a.SessionID, TicketID: a.TicketID, ProjectID: a.ProjectID, Kind: a.Kind,
		Title: a.Title, Note: a.Note, Mime: a.Mime, SizeBytes: a.SizeBytes, Revision: a.Revision, Scope: a.Scope, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
	if w.Scope == "" {
		w.Scope = ArtifactScopeTask
	}
	if a.Path != "" {
		w.Path = &a.Path
	}
	if a.URL != "" {
		w.URL = &a.URL
	}
	return json.Marshal(w)
}

func (a *Artifact) UnmarshalJSON(b []byte) error {
	var w artifactJSON
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*a = Artifact{ID: w.ID, SessionID: w.SessionID, TicketID: w.TicketID, ProjectID: w.ProjectID, Kind: w.Kind,
		Title: w.Title, Note: w.Note, Mime: w.Mime, SizeBytes: w.SizeBytes, Revision: w.Revision, Scope: w.Scope, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}
	if a.Scope == "" {
		a.Scope = ArtifactScopeTask
	}
	if w.Path != nil {
		a.Path = *w.Path
	}
	if w.URL != nil {
		a.URL = *w.URL
	}
	return nil
}

// artifactMIMEs is consulted before the runtime's table: it pins the types
// the Design View depends on, which differ between machines otherwise
// (.mov, .mp4, .js, .md).
var artifactMIMEs = map[string]string{
	".html": "text/html", ".htm": "text/html", ".css": "text/css", ".js": "text/javascript", ".mjs": "text/javascript",
	".json": "application/json", ".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif", ".bmp": "image/bmp", ".ico": "image/x-icon",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime",
	".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf",
	".txt": "text/plain", ".md": "text/markdown", ".csv": "text/csv", ".pdf": "application/pdf",
}

// ArtifactMIME is a file's media type: by extension first (the publisher
// named the file), then by its first bytes, then octet-stream. Parameters
// such as charset are stripped; the HTTP adapter adds its own.
func ArtifactMIME(name string, head []byte) string {
	ext := strings.ToLower(path.Ext(name))
	if m, ok := artifactMIMEs[ext]; ok {
		return m
	}
	if ext != "" {
		if m := mime.TypeByExtension(ext); m != "" {
			return stripParams(m)
		}
	}
	if len(head) > 0 {
		return stripParams(http.DetectContentType(head))
	}
	return "application/octet-stream"
}

func stripParams(m string) string {
	if i := strings.Index(m, ";"); i >= 0 {
		return strings.TrimSpace(m[:i])
	}
	return m
}

// InferArtifactKind maps a media type to the kind the viewer renders it as.
func InferArtifactKind(mimeType string) ArtifactKind {
	switch {
	case mimeType == "text/html":
		return ArtifactPage
	case strings.HasPrefix(mimeType, "image/"):
		return ArtifactImage
	case strings.HasPrefix(mimeType, "video/"):
		return ArtifactVideo
	}
	return ArtifactFile
}

// ArtifactKindMatches reports whether a declared kind agrees with the file's
// inferred one. Omitted means infer; file accepts anything (a download card).
func ArtifactKindMatches(declared, inferred ArtifactKind) bool {
	return declared == "" || declared == ArtifactFile || declared == inferred
}

// IsLoopbackURL reports whether raw is an http(s) URL to this machine:
// localhost or a loopback IP. The only URLs an artifact may point at.
func IsLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
