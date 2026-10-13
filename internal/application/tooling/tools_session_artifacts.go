package tooling

import (
	"context"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// sessionArtifactTools let a session publish the loopback URL of a dev server
// it started to the Design tab of the web UI, then attach and detach project
// assets. Files are published by writing them into task/designs/ in the
// session's virtual shell. Scoped by the session id in the context, like the
// other task tools.
func sessionArtifactTools(planningSvc ports.Planning, sessions ports.SessionRepository, art ArtifactTooling) []domain.Tool {
	return append([]domain.Tool{
		{
			Name: "publish_artifact",
			Description: "Publish the loopback URL of a dev server you started, so it appears in the Design tab of the web UI while the " +
				"conversation continues. Publishing the same url again refreshes the same card (same artifact_id, revision + 1); " +
				"say what changed in note. To publish a file (a page, a component, an image, a video), write it into " +
				"task/designs/ in your virtual shell instead: a path is refused with PUBLISH_URL_ONLY.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["url","title"],"properties":{` +
				`"url":{"type":"string","description":"http://localhost:<port>/... or http://127.0.0.1:<port>/... of a dev server started from the worktree."},` +
				`"title":{"type":"string","description":"Short, human; shown on the card."},` +
				`"note":{"type":"string","description":"What changed in this revision, one or two lines."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				if art.Publisher == nil {
					return nil, artifactsUnavailable()
				}
				if strings.TrimSpace(getString(args, "path", "")) != "" {
					return nil, coded("PUBLISH_URL_ONLY", "publish_artifact takes only a dev server's loopback url. To publish a file, "+
						"write it into task/designs/ in your virtual shell ($HARNESS_SHELL); changing it there publishes a new revision.")
				}
				url := strings.TrimSpace(getString(args, "url", ""))
				if url == "" {
					return nil, coded("INVALID_INPUT", "url is required: the loopback url of a dev server you started, e.g. http://127.0.0.1:5173/")
				}
				a, err := art.Publisher.Publish(ctx, ports.PublishArtifactRequest{
					SessionID: scope.session.ID,
					URL:       url,
					Title:     getString(args, "title", ""),
					Kind:      string(domain.ArtifactURL),
					Note:      getString(args, "note", ""),
				})
				if err != nil {
					return nil, codedMessage(err)
				}
				return map[string]any{"artifact_id": a.ID, "revision": a.Revision, "kind": a.Kind, "view_url": viewURL(art, a)}, nil
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

// attachedIDs is the tasks a project asset is attached to, never nil.
func attachedIDs(a *domain.Artifact) []string {
	if a.AttachedTicketIDs == nil {
		return []string{}
	}
	return a.AttachedTicketIDs
}

// coded is a refusal whose message starts with its code. The MCP bridge
// hands the agent the message alone, so the code has to be in it.
func coded(code, message string) error {
	return &domain.StructuredError{Code: code, Message: code + ": " + message}
}

// codedMessage puts a session tool's refusal code in front of its message,
// unless the message starts with it already, so the agent can correct itself
// (DOCUMENT_NOT_ON_TASK, ARTIFACT_PATH_OUTSIDE_WORKTREE, …). Errors without a
// code, and coded errors that carry more than a code (DetailedError), pass
// through unchanged.
func codedMessage(err error) error {
	se, ok := err.(*domain.StructuredError)
	if !ok || se.Code == "" || strings.HasPrefix(se.Message, se.Code) {
		return err
	}
	return &domain.StructuredError{Code: se.Code, Message: se.Code + ": " + se.Message}
}

// codedTools makes every tool's refusals coded in their message.
func codedTools(tools []domain.Tool) []domain.Tool {
	for i := range tools {
		h := tools[i].Handler
		tools[i].Handler = func(ctx context.Context, args map[string]any) (any, error) {
			out, err := h(ctx, args)
			if err != nil {
				return nil, codedMessage(err)
			}
			return out, nil
		}
	}
	return tools
}
