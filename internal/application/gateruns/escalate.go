package gateruns

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ Escalation = (*Escalator)(nil)

// Escalator is the Guard layer's escalation (ADR 0012): the session is stopped
// the way the person's Stop does, resumable, and the person is told once
// through the event the notifications context records; a delegate's architect
// gets a task message too.
type Escalator struct {
	Stopper ports.SessionStopper
	Events  ports.EventPublisher
	Feed    ports.ProjectChangeSink
	Channel ports.TaskChannel
}

// Escalate implements Escalation.
func (e *Escalator) Escalate(ctx context.Context, sess *domain.Session, run *domain.GateRun) error {
	red := run.RedKinds()
	if e.Events != nil {
		if err := e.Events.Publish(ctx, domain.SessionEscalated{SessionID: sess.ID, RunID: run.ID, RedGates: red}); err != nil {
			slog.Warn("publish a session escalation", "session", sess.ID, "err", err)
		}
	}
	if e.Feed != nil {
		e.Feed.AnnounceChange(ports.ProjectChange{SessionEscalated: &ports.SessionEscalatedChange{
			ProjectID: sess.ProjectID, SessionID: sess.ID, RunID: run.ID, RedGates: red}})
	}
	if e.Channel != nil {
		if role, err := e.Channel.SessionRole(ctx, sess.ID); err == nil && role == domain.RoleDelegate {
			names := make([]string, len(red))
			for i, k := range red {
				names[i] = string(k)
			}
			_, err := e.Channel.SendToArchitect(ctx, sess.ID, ports.TaskMessageInput{
				Kind: domain.MessageStatusReport, Status: domain.ReportBlocked, Subject: "Escalated: retry limit reached",
				Body: "This session's done declaration was red up to its retry limit, so the harness stopped it as escalated and told the person. " +
					"Red gates: " + strings.Join(names, ", ") + ". It can be resumed with resume_task_session once the cause is dealt with.\n\n" + run.FailureReport(),
			})
			if err != nil {
				slog.Warn("tell the architect of an escalation", "session", sess.ID, "err", err)
			}
		}
	}
	if e.Stopper != nil {
		var se *domain.StructuredError
		if err := e.Stopper.Stop(ctx, sess.ID); err != nil && !(errors.As(err, &se) && se.Code == "SESSION_RUNS_ON_TUI") {
			slog.Warn("stop an escalated session", "session", sess.ID, "err", err)
		}
	}
	return nil
}
