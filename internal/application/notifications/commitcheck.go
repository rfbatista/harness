package notifications

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// A boot that finds sessions with the commit check off posts one
// commit_check_off notification per project, listing every affected session
// (ADR 0098). It follows the sessions_interrupted notification's pattern
// (ADR 0083): it lists sessions with their state, and a state change sends it
// again on the stream. Posting a new one, in a later boot, ends the project's
// previous one; it resolves as repaired once no listed session is still off.

// endLegacyCommitChecks resolves, as ended, the commit_check_off rows of the
// per-session rule that earlier revisions recorded: one per session, with no
// list. The per-project notification replaces them (contract revision 6, "Rows
// from before revision 6"). It runs once per boot, and finds none after the
// first.
func (s *Service) endLegacyCommitChecks() {
	open := true
	list, err := s.repo.List(context.Background(), ports.NotificationFilter{Kind: domain.NotificationCommitCheckOff, Open: &open})
	if err != nil {
		slog.Warn("read the commit check off notifications", "err", err)
		return
	}
	for _, n := range list {
		if len(n.ListedSessions) > 0 {
			continue
		}
		s.resolve(n.CauseKey, domain.OutcomeEnded)
	}
}

// commitCheckKey is the cause of a project's commit_check_off notification:
// only one is open at a time.
func commitCheckKey(projectID string) string { return "commit_check_off:" + projectID }

// onCommitCheck handles a session's commit check going off or being repaired.
func (s *Service) onCommitCheck(ev domain.SessionCommitCheckChanged) {
	if ev.ProjectID == "" {
		slog.Warn("notification for a session that is not recorded", "session", ev.SessionID)
		return
	}
	if !ev.Off {
		s.settleCommitCheck(ev.ProjectID, ev.SessionID, domain.CommitCheckRepaired)
		return
	}
	// Names are read before the write: a transaction holds the database's one
	// writer connection.
	entry := domain.ListedSession{
		SessionID: ev.SessionID, TaskID: ev.TicketID, State: domain.CommitCheckOff,
		AgentName: s.actorOfSession(ev.SessionID).Name,
		Link:      domain.TaskLink(ev.ProjectID, ev.TicketID) + "?session=" + url.QueryEscape(ev.SessionID),
	}
	line := s.commitCheckLine(ev.TicketID, entry.AgentName, ev.Reason)
	key := commitCheckKey(ev.ProjectID)
	var created bool
	c, err := s.write(func(tx ports.NotificationRepository, now time.Time) ([]*domain.Notification, bool, error) {
		// A per-session row that outlived the first boot is ended by the
		// project's notification, so at most one stays open.
		openRows := true
		old, err := tx.List(context.Background(), ports.NotificationFilter{ProjectID: ev.ProjectID, Kind: domain.NotificationCommitCheckOff, Open: &openRows})
		if err != nil {
			return nil, false, err
		}
		var legacy []*domain.Notification
		for _, n := range old {
			if len(n.ListedSessions) == 0 {
				ended, err := tx.Resolve(n.CauseKey, now, domain.OutcomeEnded)
				if err != nil {
					return nil, false, err
				}
				legacy = append(legacy, ended...)
			}
		}
		open := tx.OpenByCause(key)
		if open != nil && !open.CreatedAt.Before(s.bootedAt) {
			list, changed := withState(open.ListedSessions, entry)
			if !changed {
				return legacy, len(legacy) > 0, nil
			}
			w := commitCheckWhat(len(list))
			cur, err := tx.UpdateCommitCheck(open.ID, list, w.verb, w.title(domain.ActorHarness), joinLines(open.Body, line))
			if err != nil || cur == nil {
				return nil, false, err
			}
			return append(legacy, cur), true, nil
		}
		out := legacy
		if open != nil { // an earlier boot's
			ended, err := tx.Resolve(key, now, domain.OutcomeEnded)
			if err != nil {
				return nil, false, err
			}
			out = ended
		}
		w := commitCheckWhat(1)
		n := &domain.Notification{
			ProjectID: ev.ProjectID, Source: domain.NotificationFromSystem,
			Kind: domain.NotificationCommitCheckOff, Level: domain.NotificationAction,
			Actor: systemActor(domain.ActorHarness), Body: clip(line, domain.MaxNotificationBodyLen),
			Link: domain.ProjectLink(ev.ProjectID), CauseKey: key, CreatedAt: now,
			ListedSessions: []domain.ListedSession{entry},
		}
		w.apply(n)
		made, err := tx.Create(n)
		if err != nil {
			return nil, false, err
		}
		created = true
		return append(out, made), true, nil
	})
	if err != nil {
		slog.Warn("record the commit check off notification", "err", err)
		return
	}
	if c != nil && created {
		s.delivery.enqueue(*c.Changed[len(c.Changed)-1])
	}
}

// commitCheckLine is "task · agent: reason", one line of the body.
func (s *Service) commitCheckLine(taskID, agent, reason string) string {
	title := "(no task)"
	if tk := s.readers.Tickets.Get(context.Background(), taskID); taskID != "" && tk != nil {
		title = tk.Title
	}
	return title + " · " + agent + ": " + strings.TrimSpace(reason)
}

func joinLines(body, line string) string {
	return clip(strings.TrimSpace(body+"\n"+line), domain.MaxNotificationBodyLen)
}

// withState is list with entry's session in entry's state, and whether that
// changed the list.
func withState(list []domain.ListedSession, entry domain.ListedSession) ([]domain.ListedSession, bool) {
	out := append([]domain.ListedSession(nil), list...)
	for i, e := range out {
		if e.SessionID != entry.SessionID {
			continue
		}
		if e.State == entry.State {
			return out, false
		}
		out[i].State = entry.State
		return out, true
	}
	return append(out, entry), true
}

// settleCommitCheck moves a session of the project's open notification to
// state and sends the notification again, or resolves it when no listed
// session is still off.
func (s *Service) settleCommitCheck(projectID, sessionID string, state domain.CommitCheckState) {
	if open := s.repo.OpenByCause(commitCheckKey(projectID)); open != nil {
		s.settleListed(open, sessionID, state)
	}
}

// settleListed does it for notification n when it lists the session.
func (s *Service) settleListed(n *domain.Notification, sessionID string, state domain.CommitCheckState) {
	s.logged("settle the commit check off notification", func(tx ports.NotificationRepository, now time.Time) ([]*domain.Notification, bool, error) {
		cur := tx.Get(n.ID)
		if cur == nil || cur.ResolvedAt != nil {
			return nil, false, nil
		}
		list, changed := withState(cur.ListedSessions, domain.ListedSession{SessionID: sessionID, State: state})
		if !changed || !listsSession(cur.ListedSessions, sessionID) {
			return nil, false, nil
		}
		w := commitCheckWhat(len(list))
		cur, err := tx.UpdateCommitCheck(cur.ID, list, w.verb, w.title(domain.ActorHarness), cur.Body)
		if err != nil || cur == nil {
			return nil, false, err
		}
		outcome := domain.OutcomeEnded
		for _, e := range list {
			switch e.State {
			case domain.CommitCheckOff:
				return []*domain.Notification{cur}, true, nil
			case domain.CommitCheckRepaired:
				outcome = domain.OutcomeRepaired
			}
		}
		resolved, err := tx.Resolve(cur.CauseKey, now, outcome)
		return resolved, len(resolved) > 0, err
	})
}

// settleDeleted ends the session in every open commit check off notification
// that lists it.
func (s *Service) settleDeleted(sessionID string) {
	open := true
	list, err := s.repo.List(context.Background(), ports.NotificationFilter{Kind: domain.NotificationCommitCheckOff, Open: &open})
	if err != nil {
		slog.Warn("read the commit check off notifications", "err", err)
		return
	}
	for _, n := range list {
		if listsSession(n.ListedSessions, sessionID) {
			s.settleListed(n, sessionID, domain.CommitCheckEnded)
		}
	}
}

// listedStates is list with each session's task title as of now, and a
// session that is gone shown as ended. The stored list is not changed.
func (s *Service) listedStates(ctx context.Context, list []domain.ListedSession, tasks map[string]string) []domain.ListedSession {
	out := make([]domain.ListedSession, len(list))
	for i, e := range list {
		if e.State == domain.CommitCheckOff && s.readers.Sessions.Get(e.SessionID) == nil {
			e.State = domain.CommitCheckEnded
		}
		if e.TaskID != "" {
			title, ok := tasks[e.TaskID]
			if !ok {
				if tk := s.readers.Tickets.Get(ctx, e.TaskID); tk != nil {
					title = tk.Title
				}
				tasks[e.TaskID] = title
			}
			e.TaskTitle = title
		}
		out[i] = e
	}
	return out
}

// listsSession says the list has the session.
func listsSession(list []domain.ListedSession, sessionID string) bool {
	for _, e := range list {
		if e.SessionID == sessionID {
			return true
		}
	}
	return false
}
