package tooling

import (
	"context"
	"errors"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// sessionArtifactTools let a session publish what it made — a page, an
// image, a video, a file from its worktree, or a loopback dev-server URL —
// to the Design tab of the web UI, see what the task's sessions published,
// and take its own down; then the project's design assets. Scoped by the
// session id in the context, like the other task tools.
func sessionArtifactTools(planningSvc ports.Planning, sessions ports.SessionRepository, art ArtifactTooling) []domain.Tool {
	unavailable := artifactsUnavailable()
	return append([]domain.Tool{
		{
			Name: "publish_artifact",
			Description: "Publish a file from your worktree — a self-contained HTML page or component, an image, a video, any file — " +
				"or the loopback URL of a dev server you started, so it appears in the Design tab of the web UI while the " +
				"conversation continues. Re-publishing the same path or url refreshes the same card (same artifact_id, revision + 1); " +
				"say what changed in note. Files must be inside the worktree and under the server's size cap.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["title"],"properties":{` +
				`"path":{"type":"string","description":"File inside the session worktree: relative to it, or absolute as long as it resolves inside it. Give path or url, not both."},` +
				`"url":{"type":"string","description":"http://localhost:<port>/... or http://127.0.0.1:<port>/... of a dev server started from the worktree."},` +
				`"title":{"type":"string","description":"Short, human; shown on the card."},` +
				`"kind":{"type":"string","enum":["page","image","video","url","file"],"description":"Inferred from the file when omitted; url is implied when url is given."},` +
				`"note":{"type":"string","description":"What changed in this revision, one or two lines."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				if art.Publisher == nil {
					return nil, unavailable
				}
				a, err := art.Publisher.Publish(ctx, ports.PublishArtifactRequest{
					SessionID: scope.session.ID,
					Path:      getString(args, "path", ""),
					URL:       getString(args, "url", ""),
					Title:     getString(args, "title", ""),
					Kind:      getString(args, "kind", ""),
					Note:      getString(args, "note", ""),
				})
				if err != nil {
					return nil, codedMessage(err)
				}
				return map[string]any{"artifact_id": a.ID, "revision": a.Revision, "kind": a.Kind, "view_url": viewURL(art, a)}, nil
			},
		},
		{
			Name: "list_task_artifacts",
			Description: "List what every session on this task has published to the Design tab, newest first: " +
				"pages, images, videos, files and dev-server URLs, each with its scope (task, or project once it is kept as a " +
				"project design asset). Use it to see what earlier sessions produced before making more.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				if art.Publisher == nil {
					return nil, unavailable
				}
				list, err := art.Publisher.ListTaskArtifacts(ctx, scope.ticket.ID)
				if err != nil {
					return nil, err
				}
				out := make([]taskArtifact, 0, len(list))
				for _, a := range list {
					out = append(out, taskArtifact{ArtifactID: a.ID, SessionID: a.SessionID, Kind: a.Kind, Title: a.Title,
						Path: a.Path, URL: a.URL, Scope: scopeOf(a), Revision: a.Revision, UpdatedAt: a.UpdatedAt})
				}
				return map[string]any{"count": len(out), "artifacts": out}, nil
			},
		},
		{
			Name: "unpublish_artifact",
			Description: "Remove one of your artifacts from the Design tab. The record goes; the file stays. Only artifacts you published can be removed. " +
				"A project design asset must be moved back with move_artifact_to_task first (ARTIFACT_IN_PROJECT).",
			InputSchema: schemaFromJSON(`{"type":"object","required":["artifact_id"],"properties":{"artifact_id":{"type":"string","description":"The artifact_id publish_artifact returned."}}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				if art.Publisher == nil {
					return nil, unavailable
				}
				id := strings.TrimSpace(getString(args, "artifact_id", ""))
				if id == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "artifact_id is required"}
				}
				if err := art.Publisher.Unpublish(ctx, scope.session.ID, id); err != nil {
					var se *domain.StructuredError
					if errors.As(err, &se) && se.Code == "ARTIFACT_IN_PROJECT" {
						return nil, &domain.StructuredError{Code: se.Code, Message: "ARTIFACT_IN_PROJECT: this artifact is a project design asset that other tasks may reuse; " +
							"move it back with move_artifact_to_task first, then unpublish it (or leave it for a person to delete in the web UI)"}
					}
					return nil, codedMessage(err)
				}
				return map[string]any{"artifact_id": id, "removed": true}, nil
			},
		},
	}, sessionArtifactScopeTools(planningSvc, sessions, art)...)
}

func artifactsUnavailable() error {
	return &domain.StructuredError{Code: "UNAVAILABLE", Message: "artifacts are not available on this server"}
}

// viewURL is where a person opens the artifact: the dev server itself for a
// url artifact, the harness's view route otherwise.
func viewURL(art ArtifactTooling, a *domain.Artifact) string {
	if a.Kind != domain.ArtifactURL && art.ViewURL != nil {
		return art.ViewURL(a.ID)
	}
	return a.URL
}

// taskArtifact is an artifact as list_task_artifacts reports it.
type taskArtifact struct {
	ArtifactID string               `json:"artifact_id"`
	SessionID  string               `json:"session_id"`
	Kind       domain.ArtifactKind  `json:"kind"`
	Title      string               `json:"title"`
	Path       string               `json:"path,omitempty"`
	URL        string               `json:"url,omitempty"`
	Scope      domain.ArtifactScope `json:"scope"`
	Revision   int                  `json:"revision"`
	UpdatedAt  time.Time            `json:"updated_at"`
}

// codedMessage puts an artifact error's code in front of its message. The
// MCP bridge hands the agent the message alone, and these errors are coded
// so the agent can correct itself (ARTIFACT_PATH_OUTSIDE_WORKTREE, …).
func codedMessage(err error) error {
	var se *domain.StructuredError
	if errors.As(err, &se) && strings.HasPrefix(se.Code, "ARTIFACT_") {
		return &domain.StructuredError{Code: se.Code, Message: se.Code + ": " + se.Message}
	}
	return err
}
