// Package rollup folds sessions into the task-level statuses, counts and
// filters the dashboard, task detail and nav badges display. It mirrors the
// Flutter rules in lib/features/tasks/domain/services/task_rollup.dart so the
// two front-ends agree on what "in flight", "needs review" and "blocked" mean.
package rollup

import (
	"operators-mcp/internal/domain"
)

// AgentStatus is the UI-level state of one session (the "agent" the user sees).
type AgentStatus int

const (
	Run AgentStatus = iota
	Think
	Review
	Block
	Done
	Paused
)

// Label is the human wording for an agent status.
func (s AgentStatus) Label() string {
	switch s {
	case Run:
		return "Running"
	case Think:
		return "Thinking"
	case Review:
		return "Needs review"
	case Block:
		return "Blocked"
	case Done:
		return "Done"
	case Paused:
		return "Paused"
	}
	return "Unknown"
}

// IsLive reports whether the agent is actively working.
func (s AgentStatus) IsLive() bool { return s == Run || s == Think }

// AgentStatusOf maps a backend session status onto the UI status.
func AgentStatusOf(st domain.SessionStatus) AgentStatus {
	switch st {
	case domain.SessionRunning, domain.SessionIdle:
		return Run
	case domain.SessionThinking, domain.SessionStarting:
		return Think
	case domain.SessionWaitingApproval:
		return Review
	case domain.SessionPaused:
		return Paused
	case domain.SessionDone, domain.SessionStopped:
		return Done
	case domain.SessionFailed:
		return Block
	}
	return Think
}

// TaskStatus is the aggregate status of a task, folded from its sessions.
type TaskStatus int

const (
	Empty TaskStatus = iota
	Running
	NeedsReview
	Blocked
	PausedStatus
	DoneStatus
)

// Label is the human wording for a task status.
func (s TaskStatus) Label() string {
	switch s {
	case Empty:
		return "No agents"
	case Running:
		return "In flight"
	case NeedsReview:
		return "Needs review"
	case Blocked:
		return "Blocked"
	case PausedStatus:
		return "Paused"
	case DoneStatus:
		return "Done"
	}
	return "Unknown"
}

// Rollup is everything the task views derive from a task's sessions.
type Rollup struct {
	Sessions     []*domain.Session
	Status       TaskStatus
	RunningCount int
	ReviewCount  int
	BlockedCount int
	DoneCount    int
	CostUSD      float64
	Repos        []string
}

// Fold aggregates sessions with the precedence
// blocked > needs review > running > paused > done; no sessions is Empty.
func Fold(sessions []*domain.Session) Rollup {
	r := Rollup{Sessions: sessions}
	var hasBlock, hasReview, hasLive, hasPaused bool
	repos := map[string]bool{}
	for _, s := range sessions {
		st := AgentStatusOf(s.Status)
		switch st {
		case Block:
			hasBlock = true
			r.BlockedCount++
		case Review:
			hasReview = true
			r.ReviewCount++
		case Run, Think:
			hasLive = true
			r.RunningCount++
		case Paused:
			hasPaused = true
		case Done:
			r.DoneCount++
		}
		r.CostUSD += s.CostUSD
		if s.RepositoryID != "" && !repos[s.RepositoryID] {
			repos[s.RepositoryID] = true
			r.Repos = append(r.Repos, s.RepositoryID)
		}
	}
	switch {
	case len(sessions) == 0:
		r.Status = Empty
	case hasBlock:
		r.Status = Blocked
	case hasReview:
		r.Status = NeedsReview
	case hasLive:
		r.Status = Running
	case hasPaused:
		r.Status = PausedStatus
	default:
		r.Status = DoneStatus
	}
	return r
}

// Column is a kanban column; every TaskStatus lands in exactly one.
type Column int

const (
	InFlight Column = iota
	ReviewColumn
	BlockedColumn
	DoneColumn
)

// Label is the column heading.
func (c Column) Label() string {
	switch c {
	case InFlight:
		return "In flight"
	case ReviewColumn:
		return "Needs review"
	case BlockedColumn:
		return "Blocked"
	case DoneColumn:
		return "Done"
	}
	return ""
}

// ColumnFor places a task status: In flight also hosts paused and empty tasks.
func ColumnFor(st TaskStatus) Column {
	switch st {
	case Blocked:
		return BlockedColumn
	case NeedsReview:
		return ReviewColumn
	case DoneStatus:
		return DoneColumn
	}
	return InFlight
}

// Filter is a dashboard status filter.
type Filter int

const (
	FilterAll       Filter = iota
	FilterAttention        // needs review or blocked: what the Inbox badge counts
	FilterInFlight         // running or paused
	FilterReview
	FilterBlocked
	FilterDone
)

// Label is the chip wording for a filter.
func (f Filter) Label() string {
	switch f {
	case FilterAll:
		return "All"
	case FilterAttention:
		return "Attention"
	case FilterInFlight:
		return "In flight"
	case FilterReview:
		return "Needs review"
	case FilterBlocked:
		return "Blocked"
	case FilterDone:
		return "Done"
	}
	return ""
}

// Matches reports whether a task status passes the filter.
func (f Filter) Matches(st TaskStatus) bool {
	switch f {
	case FilterAttention:
		return st == NeedsReview || st == Blocked
	case FilterInFlight:
		return st == Running || st == PausedStatus
	case FilterReview:
		return st == NeedsReview
	case FilterBlocked:
		return st == Blocked
	case FilterDone:
		return st == DoneStatus
	}
	return true
}

// Row is one ticket joined with its rollup: the row type every task view consumes.
type Row struct {
	Ticket *domain.Ticket
	Rollup Rollup
}

// ByTicket groups sessions by ticket id; unlinked sessions key under "".
func ByTicket(sessions []*domain.Session) map[string][]*domain.Session {
	out := map[string][]*domain.Session{}
	for _, s := range sessions {
		out[s.TicketID] = append(out[s.TicketID], s)
	}
	return out
}

// Join pairs tickets with their sessions and applies the project and status
// filters. An empty projectID means every project.
func Join(tickets []*domain.Ticket, sessions []*domain.Session, projectID string, f Filter) []Row {
	by := ByTicket(sessions)
	rows := make([]Row, 0, len(tickets))
	for _, t := range tickets {
		if projectID != "" && t.ProjectID != projectID {
			continue
		}
		r := Fold(by[t.ID])
		if !f.Matches(r.Status) {
			continue
		}
		rows = append(rows, Row{Ticket: t, Rollup: r})
	}
	return rows
}

// Summary feeds the dashboard stat tiles.
type Summary struct {
	TasksInFlight int
	AgentsWorking int
	NeedsReview   int
	Blocked       int
	SpendUSD      float64
}

// Stats counts tasks in flight over the rows and agents over every session,
// so unlinked sessions still show up in the agent, review and blocked tiles.
func Stats(rows []Row, sessions []*domain.Session) Summary {
	var s Summary
	for _, r := range rows {
		if r.Rollup.Status == Running {
			s.TasksInFlight++
		}
	}
	for _, sess := range sessions {
		switch AgentStatusOf(sess.Status) {
		case Run, Think:
			s.AgentsWorking++
		case Review:
			s.NeedsReview++
		case Block:
			s.Blocked++
		}
		s.SpendUSD += sess.CostUSD
	}
	return s
}
