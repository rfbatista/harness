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

// ResourceTooling is what the resource and comment tools need: what a person
// added to the session's task, read on this machine. The zero value leaves
// the tools answering that they are unavailable.
type ResourceTooling struct {
	Files    ports.TaskResourceFiles
	Comments ports.TaskCommentReader
}

// maxInlineResource is the largest text resource read_task_resource returns
// inline; anything bigger goes by its path.
const maxInlineResource = 256 << 10

// pathNote ends every note on an upload: the path is on the server's machine.
const pathNote = " local_path is on the machine the harness server runs on; a session on another machine cannot open it."

var errResourceToolsOff = &domain.StructuredError{Code: "UNAVAILABLE", Message: "task resources are not available on this server"}

// sessionResourceTools let a session read what the person added to its task:
// resources (files, images, audio, links) and comments. They are read-only.
func sessionResourceTools(planningSvc ports.Planning, sessions ports.SessionRepository, rt ResourceTooling) []domain.Tool {
	return []domain.Tool{
		{
			Name: "list_task_resources",
			Description: "List what the person added to this session's task for context: files, images, audio and links, oldest first. " +
				"Use read_task_resource to read one.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				if rt.Files == nil {
					return nil, errResourceToolsOff
				}
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				list, err := rt.Files.ListTicketResources(ctx, scope.ticket.ID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"resources": list, "count": len(list)}, nil
			},
		},
		{
			Name: "read_task_resource",
			Description: "Read one resource the person added to this session's task. A text file up to 256 KiB comes back as content; " +
				"every upload comes with local_path, an absolute path to its bytes that your own tools can open (the Read tool reads " +
				"images and PDFs). Audio is by path only, with no transcription. A link comes back as its url; the server never fetches it. " +
				"RESOURCE_NOT_ON_TASK when the id belongs to another task.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"resource_id":{"type":"string","description":"Resource ID, from list_task_resources or a [task update · resource added] turn"}},"required":["resource_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				if rt.Files == nil {
					return nil, errResourceToolsOff
				}
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				id := getString(args, "resource_id", "")
				if id == "" {
					return nil, invalidInput("resource_id is required")
				}
				r, err := rt.Files.GetTicketResource(ctx, id)
				if err != nil {
					return nil, err
				}
				if r.TicketID != scope.ticket.ID {
					return nil, &domain.StructuredError{Code: "RESOURCE_NOT_ON_TASK", Message: "resource belongs to another task"}
				}
				return readResource(ctx, rt.Files, r)
			},
		},
		{
			Name: "list_task_comments",
			Description: "List the person's comments on this session's task, oldest first. since (RFC 3339) returns only those created " +
				"strictly after it.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"since":{"type":"string","description":"Only comments created after this time, RFC 3339 (e.g. 2026-10-08T14:00:00Z)."}}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				if rt.Comments == nil {
					return nil, errResourceToolsOff
				}
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				var since time.Time
				if s := strings.TrimSpace(getString(args, "since", "")); s != "" {
					if since, err = time.Parse(time.RFC3339Nano, s); err != nil {
						return nil, invalidInput("since must be an RFC 3339 time, e.g. 2026-10-08T14:00:00Z")
					}
				}
				list, err := rt.Comments.ListTicketComments(ctx, scope.ticket.ID, since)
				if err != nil {
					return nil, err
				}
				return map[string]any{"comments": list, "count": len(list)}, nil
			},
		},
	}
}

// readResource is read_task_resource's answer for a resource of the task.
func readResource(ctx context.Context, files ports.TaskResourceFiles, r *domain.TicketResource) (any, error) {
	out := map[string]any{"resource": r}
	if !r.IsUpload() {
		out["note"] = "A link: the server never fetches it. Open url yourself if it bears on your work and you can reach it."
		return out, nil
	}
	path, err := files.TicketResourcePath(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	out["local_path"] = path
	base := domain.MediaType(r.Mime)
	switch {
	case textLike(base) && r.SizeBytes <= maxInlineResource:
		if text, ok := inlineText(ctx, files, r.ID); ok {
			out["content"] = text
			out["note"] = "The text is in content; local_path holds the same bytes." + pathNote
			return out, nil
		}
		out["note"] = "Not valid UTF-8 text: open local_path with your tools." + pathNote
	case textLike(base):
		out["note"] = "Too large to return inline (over 256 KiB): read local_path with your Read tool, in parts." + pathNote
	case strings.HasPrefix(base, "image/") || base == "application/pdf":
		out["note"] = "Open local_path with your Read tool: it reads images and PDFs." + pathNote
	case strings.HasPrefix(base, "audio/"):
		out["note"] = "Audio is given by path only; there is no transcription." + pathNote
	default:
		out["note"] = "Open local_path with your tools if you can read a " + base + " file." + pathNote
	}
	return out, nil
}

// textLike says which media types read_task_resource returns as text.
func textLike(base string) bool {
	return strings.HasPrefix(base, "text/") || base == "application/json" || base == "application/xml" ||
		strings.HasSuffix(base, "+json") || strings.HasSuffix(base, "+xml")
}

func inlineText(ctx context.Context, files ports.TaskResourceFiles, id string) (string, bool) {
	rc, err := files.OpenTicketResource(ctx, id)
	if err != nil {
		return "", false
	}
	defer rc.Content.Close()
	b, err := io.ReadAll(io.LimitReader(rc.Content, maxInlineResource+1))
	if err != nil || len(b) > maxInlineResource || !utf8.Valid(b) {
		return "", false
	}
	return string(b), true
}
