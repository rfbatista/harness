package tooling

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// SessionTaskToolNames lists the tools SessionTaskTools returns, in order. It is
// the single source of truth for the orchestration's allow-list.
var SessionTaskToolNames = []string{
	"get_task",
	"list_task_documents",
	"read_task_document",
	"create_task_document",
	"update_task_document",
	"list_project_documents",
	"read_project_document",
	"update_project_document",
	"move_document_to_project",
	"move_document_to_task",
	"update_task_status",
	"list_task_sessions",
	"start_task_session",
	"list_project_repositories",
	"list_bounded_contexts",
	"list_agents",
	"publish_artifact",
	"list_task_artifacts",
	"unpublish_artifact",
	"list_project_artifacts",
	"move_artifact_to_project",
	"move_artifact_to_task",
	"message_architect",
	"reply_to_session",
	"list_task_messages",
	"request_user_review",
	"withdraw_user_review",
	"list_review_requests",
	"set_status_check",
	"list_status_checks",
}

// MaxLiveTaskSessions caps the sessions running on one task at once, so agents
// starting peers cannot fan out without bound.
const MaxLiveTaskSessions = 8

// PeerStarter is what start_task_session needs to start a session: the
// interactive sessions port and the project's repositories. The zero value
// leaves the tool answering that it is unavailable.
type PeerStarter struct {
	Sessions     ports.InteractiveSessions
	Repositories ports.RepositoryLister
}

// ArtifactTooling is what the artifact tools need: the publisher, and how to
// name an artifact's view route, which belongs to the HTTP adapter. The zero
// value leaves the tools answering that artifacts are unavailable.
type ArtifactTooling struct {
	Publisher ports.ArtifactPublisher
	ViewURL   func(artifactID string) string
}

// SessionTaskTools exposes the task a session was spawned into (to read, and
// to move between statuses), the documents linked to that task, the project's
// own documents, the other sessions working on it, the artifacts those
// sessions published, and the project's design assets, as MCP tools.
//
// The tools take no project or ticket id: the session id travels in the context
// (see WithSessionID) and every handler resolves the scope from it, so a session
// can only ever reach its own task, the documents linked to it, and the
// sessions sharing it. agents, when set, names the agents those sessions run
// and lists them to delegate to; arch, when set, maps the task's project into
// bounded contexts and zones; start lets a session start peers on its task;
// artifacts lets it publish what it made to the Design tab; channel connects
// the task's delegates to its architect and makes the architect the owner of
// the task status (nil: no channel, every session a peer).
func SessionTaskTools(planningSvc ports.Planning, sessions ports.SessionRepository, agents ports.AgentLister, arch ports.ArchitectureMap, start PeerStarter, artifacts ArtifactTooling, channel ports.TaskChannel) []domain.Tool {
	tools := append([]domain.Tool{
		{
			Name: "start_task_session",
			Description: "Start another agent session on this session's task, to hand off or parallelise part of the work. " +
				"It runs on the server in its own git worktree and branch, with the task's brief, and sees you through " +
				"list_task_sessions. Give it a prompt saying exactly what to do and what not to touch. Optionally pick the " +
				"agent (by name), the repository (by name; default yours) and the branch to cut its worktree from " +
				"(default the repository's default branch; pass your own branch to build on what you have committed). " +
				"It gets your permission mode. At most " + strconv.Itoa(MaxLiveTaskSessions) + " sessions run on a task at once. " +
				"If you are the task's architect, a status-check loop wakes you every status_check_minutes (default 10, 0 for " +
				"none) to check on the new session. A session you start while you are the architect or one of its delegates " +
				"reports to the architect.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["prompt"],"properties":{` +
				`"prompt":{"type":"string","description":"What the new session should do: its first message."},` +
				`"agent":{"type":"string","description":"The agent to run, by name or id. Default: none (plain claude)."},` +
				`"repository":{"type":"string","description":"The repository to work in, by name or id. Default: yours."},` +
				`"base_branch":{"type":"string","description":"The branch its worktree branches off. Default: the repository's default branch."},` +
				`"mode":{"type":"string","enum":["","architect","design"],"description":"architect: the session shapes its prompt into per-application specs and delegates them. design: the session produces components, images and videos and publishes each to the Design tab. Default: none."},` +
				`"status_check_minutes":{"type":"integer","minimum":0,"maximum":240,"description":"Architect only: minutes between the checks that wake you about this session, 2–240; 0 for no loop. Default: 10."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				if start.Sessions == nil {
					return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "starting sessions is not available on this server"}
				}
				return startPeer(ctx, scope, sessions, agents, start, channel, args)
			},
		},
		{
			Name: "list_task_sessions",
			Description: "List the other agent sessions working on this session's task: which agent, what it was asked, " +
				"its status and last action, the branch and worktree it works in, and its role (architect, delegate, or \"\" for a " +
				"peer); the task's architect_session_id says who the architect is. Use it to coordinate with them and " +
				"avoid duplicated or conflicting work. Only sessions still running are listed unless include_ended is true. " +
				"Your own session is not in the list; it is reported as `you`.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"include_ended":{"type":"boolean","description":"Also list sessions that have finished, failed or been stopped (default false)."}}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				includeEnded, _ := args["include_ended"].(bool)
				roles := taskRoles(ctx, scope, channel)
				peers := taskPeers(ctx, scope, sessions.List(ports.SessionFilter{ProjectID: scope.session.ProjectID, TicketID: scope.ticket.ID}), includeEnded, agents, roles.of)
				return map[string]any{
					"task":                 map[string]string{"id": scope.ticket.ID, "title": scope.ticket.Title},
					"you":                  map[string]string{"session_id": scope.session.ID, "branch": scope.session.Branch, "role": string(roles.of(scope.session.ID))},
					"architect_session_id": roles.architectID,
					"count":                len(peers),
					"sessions":             peers,
				}, nil
			},
		},
		{
			Name:        "get_task",
			Description: "Get the task this session is working on, with the documents linked to it.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"task":      scope.ticket,
					"documents": summarizeDocuments(planningSvc.ListTicketDocuments(scope.ticket.ID)),
				}, nil
			},
		},
		{
			Name:        "list_task_documents",
			Description: "List the documents linked to this session's task. Titles only — use read_task_document for the body.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"documents": summarizeDocuments(planningSvc.ListTicketDocuments(scope.ticket.ID)),
				}, nil
			},
		},
		{
			Name:        "read_task_document",
			Description: "Read one document linked to this session's task, including its content and format (html for pages written by task sessions, markdown for older documents).",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"document_id":{"type":"string","description":"Document ID"}},"required":["document_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				doc, err := scope.document(planningSvc, getString(args, "document_id", ""))
				if err != nil {
					return nil, err
				}
				return map[string]any{"document": doc}, nil
			},
		},
		{
			Name: "create_task_document",
			Description: "Write a new document as a complete HTML page and link it to this session's task. Use it to hand plans, findings and decisions " +
				"to whoever picks the task up next. content must be an HTML document (<!doctype html>, <html>, <head> with <title> and <meta charset>, <body>; " +
				"styles inline or in <style>; no external resources). Markdown is refused with DOCUMENT_NOT_HTML. It renders on the task's documents page in a sandboxed frame.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"title":{"type":"string","description":"Document title"},"content":{"type":"string","description":"A complete HTML document: doctype, html, head (title, meta charset), body. Markdown is refused."}},"required":["title","content"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				content := getString(args, "content", "")
				if !domain.IsHTMLDocument(content) {
					return nil, notHTML()
				}
				doc, err := planningSvc.CreateDocument(scope.session.ProjectID, getString(args, "title", ""), content, domain.DocumentFormatHTML, domain.DocumentScopeTask)
				if err != nil {
					return nil, err
				}
				if err := planningSvc.LinkDocument(scope.ticket.ID, doc.ID); err != nil {
					return nil, err
				}
				return map[string]any{"document": doc}, nil
			},
		},
		{
			Name:        "update_task_document",
			Description: "Revise a document linked to this session's task. Omitted fields keep their current value. New content must be a complete HTML document (Markdown is refused with DOCUMENT_NOT_HTML); it makes an older markdown document an html one.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"document_id":{"type":"string","description":"Document ID"},"title":{"type":"string","description":"New title; omit to keep the current one"},"content":{"type":"string","description":"New content as a complete HTML document; omit to keep the current one"}},"required":["document_id"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				current, err := scope.document(planningSvc, getString(args, "document_id", ""))
				if err != nil {
					return nil, err
				}
				return reviseAsHTML(planningSvc, current, args)
			},
		},
		{
			Name: "update_task_status",
			Description: "Set the status of this session's task so the board stays true as the work moves. Move it to in_progress " +
				"when you pick the work up, to review when it is ready for a person to look at, and to done when you are told it " +
				"is accepted. Only the status changes. The same status again is fine. Give a reason when you change it. If this " +
				"task has an architect and you are its delegate, this fails with TASK_STATUS_OWNED_BY_ARCHITECT: report to the " +
				"architect with message_architect instead.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["status"],"properties":{"status":{"type":"string","enum":["backlog","todo","in_progress","review","done"],"description":"The task's new status."},` +
				`"reason":{"type":"string","maxLength":` + strconv.Itoa(domain.MaxStatusReasonLen) + `,"description":"Why the status moves, in a sentence."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				status := domain.TicketStatus(getString(args, "status", ""))
				if status == "" {
					return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "status is required: one of backlog, todo, in_progress, review, done"}
				}
				reason := strings.TrimSpace(getString(args, "reason", ""))
				if utf8.RuneCountInString(reason) > domain.MaxStatusReasonLen {
					return nil, invalidInput("reason is limited to " + strconv.Itoa(domain.MaxStatusReasonLen) + " characters")
				}
				// The task comes from the session, never from the arguments.
				var tk *domain.Ticket
				if channel != nil {
					// The channel enforces that only the architect moves a task that has one.
					tk, err = channel.SetTaskStatus(ctx, scope.session.ID, status, reason)
				} else {
					tk, err = planningSvc.PatchTicket(ctx, scope.ticket.ID, ports.TicketPatch{Status: &status})
				}
				if err != nil {
					return nil, err
				}
				return map[string]any{"task": tk}, nil
			},
		},
	}, sessionDocumentTools(planningSvc, sessions)...)
	tools = append(tools, sessionChannelTools(planningSvc, sessions, channel)...)
	tools = append(tools, sessionProjectTools(planningSvc, sessions, agents, arch, start.Repositories)...)
	return append(tools, sessionArtifactTools(planningSvc, sessions, artifacts)...)
}

// peerSession is another session on the same task, as list_task_sessions
// reports it: enough to know who is doing what, and where.
type peerSession struct {
	SessionID   string               `json:"session_id"`
	AgentID     string               `json:"agent_id,omitempty"`
	AgentName   string               `json:"agent_name"`
	Status      domain.SessionStatus `json:"status"`
	Running     bool                 `json:"running"`
	Brief       string               `json:"brief,omitempty"`
	LastAction  string               `json:"last_action,omitempty"`
	Branch      string               `json:"branch,omitempty"`
	Worktree    string               `json:"worktree,omitempty"`
	Interactive bool                 `json:"interactive"`
	Role        domain.SessionRole   `json:"role"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

// taskPeers is every session on the scoped task but the caller's own, live
// ones first, then the most recently active. Ended sessions are left out
// unless includeEnded.
func taskPeers(ctx context.Context, scope *taskScope, list []*domain.Session, includeEnded bool, agents ports.AgentLister, roleOf func(string) domain.SessionRole) []peerSession {
	names := map[string]string{}
	if agents != nil {
		if all, err := agents.ListAgents(ctx); err == nil {
			for _, a := range all {
				names[a.ID] = a.Name
			}
		}
	}

	peers := make([]peerSession, 0, len(list))
	for _, s := range list {
		if s.ID == scope.session.ID || s.TicketID != scope.ticket.ID {
			continue
		}
		running := !s.Status.IsTerminal()
		if !running && !includeEnded {
			continue
		}
		name := "plain claude"
		if s.AgentID != "" {
			name = cmp.Or(names[s.AgentID], s.AgentID)
		}
		peers = append(peers, peerSession{
			SessionID:   s.ID,
			AgentID:     s.AgentID,
			AgentName:   name,
			Status:      s.Status,
			Running:     running,
			Brief:       s.Task,
			LastAction:  s.LastAction,
			Branch:      s.Branch,
			Worktree:    s.WorkingDir,
			Interactive: s.Interactive,
			Role:        roleOf(s.ID),
			UpdatedAt:   s.UpdatedAt,
		})
	}
	slices.SortStableFunc(peers, func(a, b peerSession) int {
		if a.Running != b.Running {
			if a.Running {
				return -1
			}
			return 1
		}
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})
	return peers
}

// sessionRoles is who is who on a task: its architect, and each session's
// role. The channel derives both; without one, every session is a peer.
type sessionRoles struct {
	architectID *string
	of          func(sessionID string) domain.SessionRole
}

// taskRoles reads the roles from the channel. A lookup that fails reads as a
// peer, so a listing never fails on the channel's account.
func taskRoles(ctx context.Context, scope *taskScope, channel ports.TaskChannel) sessionRoles {
	roles := sessionRoles{of: func(string) domain.SessionRole { return domain.RolePeer }}
	if channel == nil {
		return roles
	}
	if a, err := channel.TaskArchitect(ctx, scope.ticket.ID); err == nil && a != nil {
		roles.architectID = &a.ID
	}
	roles.of = func(id string) domain.SessionRole {
		role, err := channel.SessionRole(ctx, id)
		if err != nil {
			return domain.RolePeer
		}
		return role
	}
	return roles
}

// taskScope is the task a session is allowed to act on: resolved once per call
// from the session id in the context, never from the tool arguments.
type taskScope struct {
	session *domain.Session
	ticket  *domain.Ticket
}

func resolveTaskScope(ctx context.Context, planningSvc ports.Planning, sessions ports.SessionRepository) (*taskScope, error) {
	id := SessionIDFrom(ctx)
	if id == "" {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "no session in context"}
	}
	sess := sessions.Get(id)
	if sess == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	if sess.TicketID == "" {
		return nil, &domain.StructuredError{Code: "SESSION_HAS_NO_TASK", Message: "this session is not working on a task"}
	}
	tk, err := planningSvc.GetTicket(ctx, sess.TicketID)
	if err != nil {
		return nil, err
	}
	return &taskScope{session: sess, ticket: tk}, nil
}

// document returns the document only when it is linked to the scoped task, so a
// session cannot read or edit a document belonging to another task.
func (s *taskScope) document(planningSvc ports.Planning, documentID string) (*domain.Document, error) {
	if documentID == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "document_id is required"}
	}
	for _, d := range planningSvc.ListTicketDocuments(s.ticket.ID) {
		if d.ID == documentID {
			return d, nil
		}
	}
	return nil, &domain.StructuredError{Code: "DOCUMENT_NOT_ON_TASK", Message: "document is not linked to this session's task"}
}

// documentSummary is a document without its body, for listings that would
// otherwise flood the agent's context with pages.
type documentSummary struct {
	ID        string                `json:"id"`
	Title     string                `json:"title"`
	Format    domain.DocumentFormat `json:"format"`
	Scope     domain.DocumentScope  `json:"scope"`
	UpdatedAt time.Time             `json:"updated_at"`
}

func summarizeDocuments(docs []*domain.Document) []documentSummary {
	out := make([]documentSummary, 0, len(docs))
	for _, d := range docs {
		out = append(out, documentSummary{ID: d.ID, Title: d.Title, Format: d.Format, Scope: d.Scope, UpdatedAt: d.UpdatedAt})
	}
	return out
}

// notHTML is the refusal for a task document body that is not an HTML page.
// It says what to send instead, so an agent learns the rule on first use.
func notHTML() error {
	return &domain.StructuredError{Code: "DOCUMENT_NOT_HTML", Message: "content must be a complete HTML document: start with <!doctype html>, " +
		"then <html> with a <head> (title, meta charset) and a <body>. Markdown and HTML fragments are refused; " +
		"rewrite the body as an HTML page and send it again."}
}
