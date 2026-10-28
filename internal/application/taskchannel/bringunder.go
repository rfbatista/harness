package taskchannel

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.BringUnder = (*Service)(nil)

// Bringing sessions under an architect (ADR 0045): the person moves peer
// sessions of a task under its architect. Each is moved, never copied: its
// parent becomes the architect, which makes it a delegate by the role rule.

var errNothingToMove = func(skipped []ports.BringUnderSkipped) error {
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: "NOTHING_TO_MOVE", Message: "NOTHING_TO_MOVE: none of the sessions can be brought under the architect"},
		Details:         skipped,
	}
}

// runningArchitect is the task's architect when it is still running.
func runningArchitect(sessions []*domain.Session) *domain.Session {
	if a := domain.TaskArchitect(sessions); a != nil && !a.Status.IsTerminal() {
		return a
	}
	return nil
}

// split sorts the requested ids into the sessions eligible to move under arch
// (nil: an architect about to start) and those left out, both in request
// order. A repeated id counts once.
func (s *Service) split(ctx context.Context, ticketID string, arch *domain.Session, onTask []*domain.Session, ids []string) (*ports.BringUnderPlan, error) {
	plan := &ports.BringUnderPlan{}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		target, err := s.sessions.Find(ctx, id)
		if err != nil {
			return nil, err
		}
		if reason := domain.BringUnderEligibility(ticketID, arch, onTask, target); reason != "" {
			plan.Skipped = append(plan.Skipped, ports.BringUnderSkipped{SessionID: id, Reason: reason})
			continue
		}
		plan.Eligible = append(plan.Eligible, target)
	}
	if len(plan.Eligible) == 0 {
		return nil, errNothingToMove(plan.Skipped)
	}
	return plan, nil
}

// BringSessionsUnder moves the sessions under the task's running architect.
func (s *Service) BringSessionsUnder(ctx context.Context, taskID string, sessionIDs []string) (*ports.BringUnderResult, error) {
	tk, err := s.tickets.GetTicket(ctx, taskID)
	if err != nil {
		return nil, err
	}
	onTask := s.taskSessions(ctx, taskID)
	arch := runningArchitect(onTask)
	if arch == nil {
		return nil, errNoArchitect
	}
	plan, err := s.split(ctx, taskID, arch, onTask, sessionIDs)
	if err != nil {
		return nil, err
	}
	return s.move(tk, arch, plan, true)
}

// PlanBringUnder judges the sessions for an architect about to start.
func (s *Service) PlanBringUnder(ctx context.Context, taskID string, sessionIDs []string) (*ports.BringUnderPlan, error) {
	if _, err := s.tickets.GetTicket(ctx, taskID); err != nil {
		return nil, err
	}
	onTask := s.taskSessions(ctx, taskID)
	if runningArchitect(onTask) != nil {
		return nil, &domain.StructuredError{Code: "ARCHITECT_EXISTS", Message: "ARCHITECT_EXISTS: this task already has a running architect: bring the sessions under it instead"}
	}
	return s.split(ctx, taskID, nil, onTask, sessionIDs)
}

// BringUnderNewArchitect moves the sessions under an architect just started.
func (s *Service) BringUnderNewArchitect(ctx context.Context, architectID string, sessionIDs []string) (*ports.BringUnderResult, error) {
	arch := s.sessions.Get(architectID)
	if arch == nil {
		return nil, notFound("SESSION_NOT_FOUND", "session not found")
	}
	tk, err := s.tickets.GetTicket(ctx, arch.TicketID)
	if err != nil {
		return nil, err
	}
	plan, err := s.split(ctx, tk.ID, arch, s.taskSessions(ctx, tk.ID), sessionIDs)
	if err != nil {
		return nil, err
	}
	return s.move(tk, arch, plan, false)
}

// move does the move: it stores each session's new parent, starts a loop on
// each running one, announces the sessions, then the loops, then the
// architect, and delivers the turns. tellArchitect is false when the
// architect's opening already says what it supervises.
func (s *Service) move(tk *domain.Ticket, arch *domain.Session, plan *ports.BringUnderPlan, tellArchitect bool) (*ports.BringUnderResult, error) {
	ctx := context.Background()
	for _, m := range plan.Eligible {
		if err := s.sessions.UpdateParent(m.ID, arch.ID, true); err != nil {
			return nil, err
		}
	}
	// Re-read, so the answer and the feed carry what is stored.
	moved := make([]*domain.Session, 0, len(plan.Eligible))
	var loops []*domain.StatusCheck
	for _, m := range plan.Eligible {
		cur := s.sessions.Get(m.ID)
		if cur == nil {
			continue
		}
		moved = append(moved, cur)
		if cur.Status.IsTerminal() {
			continue
		}
		c, err := s.startLoop(tk.ID, tk.ProjectID, arch.ID, cur.ID, domain.StatusCheckDefaultMinutes)
		if err != nil {
			return nil, err
		}
		loops = append(loops, c)
	}
	s.DecorateSessions(ctx, moved...)
	for _, m := range moved {
		s.feed(ports.ProjectChange{Session: m})
	}
	for _, c := range loops {
		ev := &ports.StatusCheckEvent{StatusCheck: *c}
		s.feed(ports.ProjectChange{StatusCheck: ev})
		s.tell(ports.SessionEvent{Type: "status_check", StatusCheck: ev, At: s.now()}, c.ArchitectSessionID, c.DelegateSessionID)
	}
	archNow := s.sessions.Get(arch.ID)
	if archNow == nil {
		archNow = arch
	}
	s.DecorateSessions(ctx, archNow)
	s.feed(ports.ProjectChange{Session: archNow})

	if s.Delivery != nil {
		if tellArchitect {
			s.deliverBroughtUnder(archNow.ID, s.architectTurn(moved))
		}
		for _, m := range moved {
			if !m.Status.IsTerminal() {
				s.deliverBroughtUnder(m.ID, s.delegateTurn(archNow))
			}
		}
	}
	return &ports.BringUnderResult{Architect: archNow, Moved: moved, Skipped: plan.Skipped}, nil
}

// deliverBroughtUnder puts a bring-under turn into a running session, now or
// when its current turn ends. A session that stopped meanwhile gets nothing.
func (s *Service) deliverBroughtUnder(sessionID, text string) {
	_, _ = s.Delivery.Deliver(sessionID, "brought-under", text, nil)
}

// architectTurn is what an architect already running is told it now
// supervises. See "Contract: Harness server ↔ Agent sessions — turns when
// sessions are brought under", §1.
func (s *Service) architectTurn(moved []*domain.Session) string {
	now := s.now()
	noun := "sessions"
	if len(moved) == 1 {
		noun = "session"
	}
	var b strings.Builder
	b.WriteString("[task update · sessions brought under you] The person brought " + strconv.Itoa(len(moved)) + " " + noun +
		" of this task under you. Each is now your delegate: it reports to you with task messages, it cannot move the task status, and the ones still running have a status-check loop at the default interval.\n\n")
	for _, m := range moved {
		b.WriteString(domain.BroughtUnderLine(s.agentName(m), m, now) + "\n")
	}
	b.WriteString("\nRead their messages so far with list_task_messages and their branches with list_task_sessions. Supervise them as you do a delegate you started: answer what they report with reply_to_session, tune or pause each loop with set_status_check, ask the person for review with request_user_review when their work is ready, and move the task status as the work moves. Do not restart or duplicate their work.")
	return b.String()
}

// delegateTurn is what a running session brought under an architect is told:
// the delegate brief's status section behind a new first line, and a request
// for a report. §2 of the same contract.
func (s *Service) delegateTurn(arch *domain.Session) string {
	_, rest, _ := strings.Cut(domain.DelegateStatusSection, "\n")
	return fmt.Sprintf("[task update · architect] This task now has an architect: %s session %s. You are its delegate from now on.\n%s Send it a status_report now, saying where your work stands.",
		s.agentName(arch), arch.ID, rest)
}
