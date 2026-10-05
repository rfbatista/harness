package tooling

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"time"

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
	"list_task_sessions",
	"start_task_session",
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

// SessionTaskTools exposes the task a session was spawned into, the documents
// linked to that task, and the other sessions working on it, as MCP tools.
//
// The tools take no project or ticket id: the session id travels in the context
// (see WithSessionID) and every handler resolves the scope from it, so a session
// can only ever reach its own task, the documents linked to it, and the
// sessions sharing it. agents, when set, names the agents those sessions run;
// start lets a session start peers on its task.
func SessionTaskTools(planningSvc ports.Planning, sessions ports.SessionRepository, agents ports.AgentLister, start PeerStarter) []domain.Tool {
	return []domain.Tool{
		{
			Name: "start_task_session",
			Description: "Start another agent session on this session's task, to hand off or parallelise part of the work. " +
				"It runs on the server in its own git worktree and branch, with the task's brief, and sees you through " +
				"list_task_sessions. Give it a prompt saying exactly what to do and what not to touch. Optionally pick the " +
				"agent (by name), the repository (by name; default yours) and the branch to cut its worktree from " +
				"(default the repository's default branch; pass your own branch to build on what you have committed). " +
				"It gets your permission mode. At most " + strconv.Itoa(MaxLiveTaskSessions) + " sessions run on a task at once.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["prompt"],"properties":{` +
				`"prompt":{"type":"string","description":"What the new session should do: its first message."},` +
				`"agent":{"type":"string","description":"The agent to run, by name or id. Default: none (plain claude)."},` +
				`"repository":{"type":"string","description":"The repository to work in, by name or id. Default: yours."},` +
				`"base_branch":{"type":"string","description":"The branch its worktree branches off. Default: the repository's default branch."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				if start.Sessions == nil {
					return nil, &domain.StructuredError{Code: "UNAVAILABLE", Message: "starting sessions is not available on this server"}
				}
				return startPeer(ctx, scope, sessions, agents, start, args)
			},
		},
		{
			Name: "list_task_sessions",
			Description: "List the other agent sessions working on this session's task: which agent, what it was asked, " +
				"its status and last action, and the branch and worktree it works in. Use it to coordinate with them and " +
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
				peers := taskPeers(ctx, scope, sessions.List(ports.SessionFilter{ProjectID: scope.session.ProjectID, TicketID: scope.ticket.ID}), includeEnded, agents)
				return map[string]any{
					"task":     map[string]string{"id": scope.ticket.ID, "title": scope.ticket.Title},
					"you":      map[string]string{"session_id": scope.session.ID, "branch": scope.session.Branch},
					"count":    len(peers),
					"sessions": peers,
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
			Description: "Read one document linked to this session's task, including its markdown content.",
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
			Name:        "create_task_document",
			Description: "Write a new markdown document and link it to this session's task. Use it to hand plans, findings and decisions to whoever picks the task up next.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"title":{"type":"string","description":"Document title"},"content":{"type":"string","description":"Markdown content"}},"required":["title"]}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				scope, err := resolveTaskScope(ctx, planningSvc, sessions)
				if err != nil {
					return nil, err
				}
				doc, err := planningSvc.CreateDocument(
					scope.session.ProjectID,
					getString(args, "title", ""),
					getString(args, "content", ""),
				)
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
			Description: "Revise a document linked to this session's task. Omitted fields keep their current value.",
			InputSchema: schemaFromJSON(`{"type":"object","properties":{"document_id":{"type":"string","description":"Document ID"},"title":{"type":"string","description":"New title; omit to keep the current one"},"content":{"type":"string","description":"New markdown content; omit to keep the current one"}},"required":["document_id"]}`),
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
				// Partial edit: the underlying use case replaces both fields, so
				// anything the agent left out is filled back in from the stored doc.
				doc, err := planningSvc.UpdateDocument(
					current.ID,
					getString(args, "title", current.Title),
					getString(args, "content", current.Content),
				)
				if err != nil {
					return nil, err
				}
				return map[string]any{"document": doc}, nil
			},
		},
	}
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
	UpdatedAt   time.Time            `json:"updated_at"`
}

// taskPeers is every session on the scoped task but the caller's own, live
// ones first, then the most recently active. Ended sessions are left out
// unless includeEnded.
func taskPeers(ctx context.Context, scope *taskScope, list []*domain.Session, includeEnded bool, agents ports.AgentLister) []peerSession {
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
// otherwise flood the agent's context with markdown.
type documentSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updated_at"`
}

func summarizeDocuments(docs []*domain.Document) []documentSummary {
	out := make([]documentSummary, 0, len(docs))
	for _, d := range docs {
		out = append(out, documentSummary{ID: d.ID, Title: d.Title, UpdatedAt: d.UpdatedAt})
	}
	return out
}
