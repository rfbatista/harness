package tooling

import (
	"context"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const (
	// maxArtifactText is the largest artifact read as text; a larger one is
	// answered with its metadata and view_url.
	maxArtifactText = 2 << 20
	// maxSearchedArtifacts caps the artifacts one search opens.
	maxSearchedArtifacts = 50
)

// artifactMeta is an artifact as read_artifact reports it.
type artifactMeta struct {
	ID        string               `json:"id"`
	Title     string               `json:"title"`
	Kind      domain.ArtifactKind  `json:"kind"`
	Scope     domain.ArtifactScope `json:"scope"`
	Mime      string               `json:"mime,omitempty"`
	Revision  int                  `json:"revision"`
	Bytes     int64                `json:"bytes"`
	Lines     *int                 `json:"lines,omitempty"`
	Version   string               `json:"version,omitempty"`
	UpdatedAt time.Time            `json:"updated_at"`
}

// artifactWhole is a text artifact under the guard, content included.
type artifactWhole struct {
	*artifactMeta
	Content string `json:"content"`
}

func readArtifactTool(planningSvc ports.Planning, sessions ports.SessionRepository, art ArtifactTooling) domain.Tool {
	return domain.Tool{
		Name: "read_artifact",
		Description: "Read a design asset's content: one this task produced, one attached to it, or any of the project's assets. " +
			"Text assets (html, svg, css, json, md, txt) up to 2 MiB read like documents: whole under 24 KiB and 400 lines, " +
			"otherwise the outline and first 200 lines, then a section or a window with offset/limit. " +
			"Binary or larger assets answer their metadata and view_url; a dev-server artifact answers its url.",
		InputSchema: schemaFromJSON(`{"type":"object","required":["artifact_id"],"properties":{"artifact_id":{"type":"string","description":"The artifact_id from list_task_artifacts or list_project_artifacts."},` + readSchemaProps + `}}`),
		Source:      "code",
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			scope, err := resolveTaskScope(ctx, planningSvc, sessions)
			if err != nil {
				return nil, err
			}
			if art.Publisher == nil || art.Reader == nil {
				return nil, artifactsUnavailable()
			}
			a, err := scope.visibleArtifact(ctx, art, getString(args, "artifact_id", ""))
			if err != nil {
				return nil, err
			}
			meta := &artifactMeta{ID: a.ID, Title: a.Title, Kind: a.Kind, Scope: scopeOf(a), Mime: a.Mime,
				Revision: a.Revision, Bytes: a.SizeBytes, UpdatedAt: a.UpdatedAt}
			if a.Kind == domain.ArtifactURL {
				return map[string]any{"artifact": meta, "url": a.URL,
					"note": "A dev-server artifact: open url yourself if you can reach it; there is no file to read."}, nil
			}
			kind, isText := artifactTextKind(a)
			if !isText {
				return notText(art, a, meta, "Not a text asset, so it is not read as text."), nil
			}
			content, fits, err := artifactText(ctx, art.Reader, a.ID)
			if err != nil {
				return nil, err
			}
			if !fits {
				return notText(art, a, meta, "Over 2 MiB, or not UTF-8 text, so it is not read as text."), nil
			}
			tv := newTextView(content, kind)
			lines := len(tv.lines)
			meta.Bytes, meta.Lines, meta.Version = int64(len(content)), &lines, domain.DocumentVersion(content)
			return readView(content, tv, args, "artifact", &artifactWhole{artifactMeta: meta, Content: content}, meta)
		},
	}
}

func notText(art ArtifactTooling, a *domain.Artifact, meta *artifactMeta, why string) map[string]any {
	return map[string]any{"artifact": meta, "view_url": viewURL(art, a), "note": why + " Open view_url to see it."}
}

// artifactTextKind says whether an artifact is read as text, and how.
func artifactTextKind(a *domain.Artifact) (textKind, bool) {
	base := domain.MediaType(a.Mime)
	switch {
	case a.Kind == domain.ArtifactPage || base == "text/html" || base == "application/xhtml+xml":
		return textHTML, true
	case base == "text/markdown":
		return textMarkdown, true
	case textLike(base):
		return textPlain, true
	}
	return textPlain, false
}

// artifactText reads an artifact's file as text: fits is false when it is
// over maxArtifactText or not valid UTF-8.
func artifactText(ctx context.Context, reader ports.ArtifactReader, id string) (content string, fits bool, err error) {
	f, err := reader.OpenArtifactFile(ctx, id, "")
	if err != nil {
		return "", false, err
	}
	defer f.Content.Close()
	b, err := io.ReadAll(io.LimitReader(f.Content, maxArtifactText+1))
	if err != nil {
		return "", false, coded("ARTIFACT_NOT_FOUND", "the artifact's file cannot be read: "+err.Error())
	}
	if len(b) > maxArtifactText || !utf8.Valid(b) {
		return "", false, nil
	}
	return string(b), true, nil
}

// visibleArtifacts is every artifact a session of this task may read: what
// the task produced (either scope), the project assets attached to it, and
// the project's assets, each once, task ones first.
func (s *taskScope) visibleArtifacts(ctx context.Context, pub ports.ArtifactPublisher) ([]*domain.Artifact, error) {
	onTask, err := pub.ListTaskArtifacts(ctx, s.ticket.ID)
	if err != nil {
		return nil, err
	}
	inProject, err := pub.ListProjectArtifacts(ctx, s.session.ProjectID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []*domain.Artifact
	for _, a := range append(onTask, inProject...) {
		if !seen[a.ID] {
			seen[a.ID] = true
			out = append(out, a)
		}
	}
	return out, nil
}

// visibleArtifact is one of visibleArtifacts by id. Another project's
// artifact is ARTIFACT_PROJECT_MISMATCH; anything else out of sight is
// ARTIFACT_NOT_FOUND.
func (s *taskScope) visibleArtifact(ctx context.Context, art ArtifactTooling, id string) (*domain.Artifact, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, coded("INVALID_INPUT", "artifact_id is required")
	}
	list, err := s.visibleArtifacts(ctx, art.Publisher)
	if err != nil {
		return nil, err
	}
	for _, a := range list {
		if a.ID == id {
			return a, nil
		}
	}
	if a, err := art.Reader.GetArtifact(ctx, id); err == nil && a != nil && a.ProjectID != s.session.ProjectID {
		return nil, coded("ARTIFACT_PROJECT_MISMATCH", "artifact "+id+" belongs to another project")
	}
	return nil, coded("ARTIFACT_NOT_FOUND", "no artifact "+id+" this session can read: list_task_artifacts and list_project_artifacts show the ones it can")
}

// artifactSources is the visible text artifacts a search reads, at most
// maxSearchedArtifacts of them; skipped counts the text ones left out (over
// the cap, over 2 MiB, or unreadable).
func artifactSources(ctx context.Context, scope *taskScope, art ArtifactTooling) (out []searchSource, skipped int, err error) {
	if art.Publisher == nil || art.Reader == nil {
		return nil, 0, nil
	}
	list, err := scope.visibleArtifacts(ctx, art.Publisher)
	if err != nil {
		return nil, 0, err
	}
	opened := 0
	for _, a := range list {
		kind, isText := artifactTextKind(a)
		if a.Kind == domain.ArtifactURL || !isText {
			continue
		}
		if opened == maxSearchedArtifacts || a.SizeBytes > maxArtifactText {
			skipped++
			continue
		}
		opened++
		content, fits, err := artifactText(ctx, art.Reader, a.ID)
		if err != nil || !fits {
			skipped++
			continue
		}
		out = append(out, searchSource{source: "artifact", id: a.ID, title: a.Title, content: content, kind: kind})
	}
	return out, skipped, nil
}
