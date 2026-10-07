package taskchannel

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Status checks: one loop per delegate the architect started directly. One
// scheduler goroutine serves them all (Run); each firing wakes the architect
// with the delegate's state, through the same delivery as messages, keyed by
// the delegate so overdue firings collapse into one turn.

// SetStatusCheck sets the interval of the loop on one of the architect's
// delegates.
func (s *Service) SetStatusCheck(ctx context.Context, architectID, delegateID string, everyMinutes int) (*domain.StatusCheck, error) {
	if err := domain.ValidStatusCheckMinutes(everyMinutes); err != nil {
		return nil, err
	}
	sc, err := s.architectScope(ctx, architectID)
	if err != nil {
		return nil, err
	}
	d := sc.member(delegateID)
	if d == nil || d.ID == architectID {
		return nil, notFound("SESSION_NOT_ON_TASK", "session "+delegateID+" is not one of your delegates on this task")
	}
	s.mu.Lock()
	c := s.checks.Get(delegateID)
	if c == nil {
		// Only a direct delegate may have one, and only a live one.
		switch {
		case d.ParentSessionID != architectID:
			s.mu.Unlock()
			return nil, notFound("STATUS_CHECK_NOT_FOUND", "only a session you started directly can have a status check")
		case d.Status.IsTerminal():
			s.mu.Unlock()
			return nil, invalid("session " + delegateID + " has ended")
		case everyMinutes == 0:
			s.mu.Unlock()
			return nil, notFound("STATUS_CHECK_NOT_FOUND", "there is no status check on this session to pause")
		}
		c = &domain.StatusCheck{TaskID: sc.ticket.ID, ProjectID: sc.ticket.ProjectID, ArchitectSessionID: architectID, DelegateSessionID: delegateID}
	}
	c, err = s.retune(c, everyMinutes)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	s.emitCheck(c, nil, nil)
	return c, nil
}

// SetStatusCheckByPerson pauses, resumes or retunes an existing loop.
func (s *Service) SetStatusCheckByPerson(_ context.Context, delegateID string, everyMinutes int) (*domain.StatusCheck, error) {
	if err := domain.ValidStatusCheckMinutes(everyMinutes); err != nil {
		return nil, err
	}
	s.mu.Lock()
	c := s.checks.Get(delegateID)
	if c == nil {
		s.mu.Unlock()
		return nil, notFound("STATUS_CHECK_NOT_FOUND", "this session has no status check")
	}
	c, err := s.retune(c, everyMinutes)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	s.emitCheck(c, nil, nil)
	return c, nil
}

// retune applies an interval to c and saves it: 0 pauses, any other value
// (re)starts the clock. Callers hold s.mu.
func (s *Service) retune(c *domain.StatusCheck, everyMinutes int) (*domain.StatusCheck, error) {
	if c.State == domain.StatusCheckEnded {
		return nil, invalid("this status check has ended with its session")
	}
	c.EveryMinutes = everyMinutes
	if everyMinutes == 0 {
		c.State, c.NextAt = domain.StatusCheckPaused, nil
	} else {
		next := s.now().Add(c.Every())
		c.State, c.NextAt = domain.StatusCheckActive, &next
	}
	if err := s.checks.Save(c); err != nil {
		return nil, err
	}
	s.wake()
	return c, nil
}

// ListStatusChecks is every loop on the task.
func (s *Service) ListStatusChecks(_ context.Context, taskID string) ([]*domain.StatusCheck, error) {
	return s.checks.ListByTask(taskID), nil
}

// pushBack moves an active loop's next firing a full interval away: a
// delegate that keeps reporting is not checked on.
func (s *Service) pushBack(delegateID string) {
	s.mu.Lock()
	c := s.checks.Get(delegateID)
	if c == nil || c.State != domain.StatusCheckActive {
		s.mu.Unlock()
		return
	}
	next := s.now().Add(c.Every())
	c.NextAt = &next
	err := s.checks.Save(c)
	s.mu.Unlock()
	if err == nil {
		s.wake()
		s.emitCheck(c, nil, nil)
	}
}

// --- lifecycle ---

// Subscribe follows the session lifecycle: loops start with a delegate the
// architect starts, end with the delegate or when another architect takes
// over, and task status moves are announced.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(_ context.Context, ev domain.SessionStarted) error { return s.sessionStarted(ev) })
	ports.On(sub, func(_ context.Context, ev domain.SessionEnded) error {
		s.endLoop(ev.SessionID, ev.Status, true)
		return nil
	})
	ports.On(sub, func(_ context.Context, ev domain.SessionDeleted) error {
		s.endLoop(ev.SessionID, "", false)
		return nil
	})
	ports.On(sub, func(_ context.Context, ev domain.TicketStatusChanged) error { s.emitStatus(ev.Change); return nil })
}

func (s *Service) sessionStarted(ev domain.SessionStarted) error {
	if ev.TicketID == "" {
		return nil
	}
	if ev.Mode == domain.SessionModeArchitect {
		// A new architect: the previous one's loops end, without a last turn.
		for _, c := range s.checks.ListByTask(ev.TicketID) {
			if c.ArchitectSessionID != ev.SessionID && c.State != domain.StatusCheckEnded {
				s.finish(c.DelegateSessionID)
			}
		}
		return nil
	}
	if ev.ParentSessionID == "" {
		return nil
	}
	arch := domain.TaskArchitect(s.taskSessions(ev.TicketID))
	if arch == nil || arch.ID != ev.ParentSessionID {
		return nil // a delegate's or a peer's session gets no loop
	}
	minutes := domain.StatusCheckDefaultMinutes
	if ev.StatusCheckMinutes != nil {
		minutes = *ev.StatusCheckMinutes
	}
	if err := domain.ValidStatusCheckMinutes(minutes); err != nil {
		return err
	}
	if minutes == 0 {
		return nil
	}
	next := s.now().Add(time.Duration(minutes) * time.Minute)
	c := &domain.StatusCheck{
		TaskID: ev.TicketID, ProjectID: ev.ProjectID, ArchitectSessionID: arch.ID, DelegateSessionID: ev.SessionID,
		EveryMinutes: minutes, NextAt: &next, State: domain.StatusCheckActive,
	}
	s.mu.Lock()
	err := s.checks.Save(c)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.wake()
	s.emitCheck(c, nil, nil)
	return nil
}

// finish ends a loop and announces it; it reports whether it ended one.
func (s *Service) finish(delegateID string) *domain.StatusCheck {
	s.mu.Lock()
	c := s.checks.Get(delegateID)
	if c == nil || c.State == domain.StatusCheckEnded {
		s.mu.Unlock()
		return nil
	}
	c.State, c.NextAt = domain.StatusCheckEnded, nil
	err := s.checks.Save(c)
	s.mu.Unlock()
	if err != nil {
		return nil
	}
	s.emitCheck(c, nil, nil)
	return c
}

// endLoop ends the delegate's loop; with lastTurn, the architect gets one
// last check telling it how the delegate ended.
func (s *Service) endLoop(delegateID string, status domain.SessionStatus, lastTurn bool) {
	c := s.finish(delegateID)
	if c == nil || !lastTurn {
		return
	}
	s.deliverCheck(c, "ended: "+string(status),
		"This delegate has ended. Look at what it left (list_task_messages, its branch and documents) and update the task status if needed.")
}

// --- scheduler ---

// wake makes the scheduler look at the loops again.
func (s *Service) wake() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Run is the scheduler: one goroutine for every loop, until ctx ends. It
// sleeps until the earliest firing, or until a loop changes.
func (s *Service) Run(ctx context.Context) {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		s.FireDue()
		wait := time.Hour
		if active := s.checks.ListActive(); len(active) > 0 && active[0].NextAt != nil {
			wait = max(active[0].NextAt.Sub(s.now()), time.Second)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)
		select {
		case <-ctx.Done():
			return
		case <-s.kick:
		case <-timer.C:
		}
	}
}

// FireDue fires every active loop whose time has come. A loop that missed
// several firings (the server was down) fires once.
func (s *Service) FireDue() {
	now := s.now()
	for _, c := range s.checks.ListActive() {
		if c.NextAt == nil || c.NextAt.After(now) {
			continue
		}
		s.fire(c.DelegateSessionID, now)
	}
}

func (s *Service) fire(delegateID string, now time.Time) {
	delegate := s.sessions.Get(delegateID)
	if delegate == nil || delegate.Status.IsTerminal() {
		// The delegate ended while nobody was listening (a restart).
		status := domain.SessionStopped
		if delegate != nil {
			status = delegate.Status
		}
		s.endLoop(delegateID, status, delegate != nil)
		return
	}
	s.mu.Lock()
	c := s.checks.Get(delegateID)
	if c == nil || c.State != domain.StatusCheckActive {
		s.mu.Unlock()
		return
	}
	next := now.Add(c.Every())
	c.NextAt = &next
	err := s.checks.Save(c)
	s.mu.Unlock()
	if err != nil {
		return
	}
	s.deliverCheck(c, "", "Check on this delegate (reply_to_session, list_task_messages) and update the task status if needed.")
}

// deliverCheck sends the architect a status-check turn about c's delegate.
// A stopped architect gets nothing: the firing is announced as not
// delivered. A busy one gets it when its turn ends; a newer firing replaces
// one still waiting.
func (s *Service) deliverCheck(c *domain.StatusCheck, ending, instruction string) {
	now := s.now()
	arch := s.sessions.Get(c.ArchitectSessionID)
	if arch == nil || arch.Status.IsTerminal() || s.Delivery == nil {
		no := false
		s.emitCheck(c, &now, &no)
		return
	}
	turn := s.checkTurn(c, ending) + "\n" + instruction
	_, err := s.Delivery.Deliver(arch.ID, "check:"+c.DelegateSessionID, turn, func(at time.Time) {
		s.mu.Lock()
		cur := s.checks.Get(c.DelegateSessionID)
		if cur != nil {
			cur.FiredCount++
			cur.LastFiredAt = &at
			_ = s.checks.Save(cur)
		}
		s.mu.Unlock()
		if cur != nil {
			yes := true
			s.emitCheck(cur, &at, &yes)
		}
	})
	if err != nil {
		no := false
		s.emitCheck(c, &now, &no)
	}
}

// checkTurn is the status-check header:
//
//	[status check · <agent> session <id> · status <status> · last action "<…>" · last report <kind/status, age> or none · pending reviews n]
func (s *Service) checkTurn(c *domain.StatusCheck, ending string) string {
	d := s.sessions.Get(c.DelegateSessionID)
	status, last := "deleted", ""
	if d != nil {
		status, last = string(d.Status), d.LastAction
	}
	if ending != "" {
		status = ending
	}
	report := "none"
	if r := s.messages.LastReport(c.DelegateSessionID); r != nil {
		report = string(r.Kind)
		if r.Status != "" {
			report += "/" + string(r.Status)
		}
		report += ", " + age(s.now().Sub(r.CreatedAt)) + " ago"
	}
	return fmt.Sprintf("[status check · %s session %s · status %s · last action %q · last report %s · pending reviews %s]",
		s.agentName(d), c.DelegateSessionID, status, last, report, strconv.Itoa(s.reviews.CountPending(c.TaskID)))
}

func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	default:
		return strconv.Itoa(int(d.Hours())) + "h"
	}
}
