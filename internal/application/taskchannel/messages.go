package taskchannel

import (
	"context"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// SendToArchitect stores a message from a session to its task's architect and
// delivers it.
func (s *Service) SendToArchitect(ctx context.Context, fromSessionID string, in ports.TaskMessageInput) (*domain.TaskMessage, error) {
	switch in.Kind {
	case domain.MessageReviewRequest, domain.MessageStatusReport, domain.MessageQuestion:
	default:
		return nil, invalid("kind must be review_request, status_report or question")
	}
	if in.Verdict != "" {
		return nil, invalid("only the architect gives a verdict")
	}
	sc, err := s.scopeOf(ctx, fromSessionID)
	if err != nil {
		return nil, err
	}
	arch := sc.architect()
	if arch == nil {
		return nil, errNoArchitect
	}
	if arch.ID == fromSessionID {
		return nil, invalid("you are the task's architect: reply to a session with reply_to_session")
	}
	if in.InReplyTo != "" {
		if prev := s.messages.Get(in.InReplyTo); prev == nil || prev.TaskID != sc.ticket.ID {
			return nil, notFound("MESSAGE_NOT_FOUND", "message "+in.InReplyTo+" is not on this task")
		}
	}
	m, err := s.store(ctx, sc, fromSessionID, arch, in)
	if err != nil {
		return nil, err
	}
	if in.Kind == domain.MessageStatusReport {
		s.pushBack(fromSessionID)
	}
	return m, nil
}

// ReplyFromArchitect sends the architect's reply to a session on its task.
func (s *Service) ReplyFromArchitect(ctx context.Context, architectID, toSessionID string, in ports.TaskMessageInput) (*domain.TaskMessage, error) {
	sc, err := s.architectScope(ctx, architectID)
	if err != nil {
		return nil, err
	}
	to := sc.member(toSessionID)
	if to == nil {
		return nil, notFound("SESSION_NOT_ON_TASK", "session "+toSessionID+" is not on this task")
	}
	if to.ID == architectID {
		return nil, invalid("you cannot reply to yourself")
	}
	if in.Kind != "" && in.Kind != domain.MessageReply {
		return nil, invalid("the architect sends replies")
	}
	in.Kind = domain.MessageReply
	if in.InReplyTo != "" {
		prev := s.messages.Get(in.InReplyTo)
		if prev == nil || prev.TaskID != sc.ticket.ID {
			return nil, notFound("MESSAGE_NOT_FOUND", "message "+in.InReplyTo+" is not on this task")
		}
		if in.Verdict != "" && prev.Kind != domain.MessageReviewRequest {
			return nil, invalid("a verdict answers a review_request")
		}
	} else if in.Verdict != "" {
		return nil, invalid("a verdict answers a review_request: give in_reply_to")
	}
	return s.store(ctx, sc, architectID, to, in)
}

// store validates, persists, announces and delivers a message.
func (s *Service) store(ctx context.Context, sc *scope, fromID string, to *domain.Session, in ports.TaskMessageInput) (*domain.TaskMessage, error) {
	m := &domain.TaskMessage{
		TaskID: sc.ticket.ID, ProjectID: sc.ticket.ProjectID, FromSessionID: fromID, ToSessionID: to.ID,
		Kind: in.Kind, Subject: strings.TrimSpace(in.Subject), Body: in.Body, Status: in.Status, Verdict: in.Verdict,
		InReplyTo: in.InReplyTo, DocumentIDs: ids(in.DocumentIDs), ArtifactIDs: ids(in.ArtifactIDs),
		CreatedAt: s.now(), Queued: !to.Status.IsTerminal() && s.Delivery != nil,
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if err := s.checkAttachments(ctx, sc.ticket.ID, m.DocumentIDs, m.ArtifactIDs); err != nil {
		return nil, err
	}
	m, err := s.messages.Create(m)
	if err != nil {
		return nil, err
	}
	s.emitMessage(m)
	if m.Queued {
		s.send(m, s.sessions.Get(fromID))
	}
	if got := s.messages.Get(m.ID); got != nil {
		m = got
	}
	return m, nil
}

// send hands a stored message to the courier. When it goes in, now or at the
// end of the recipient's turn, the message is marked delivered and announced.
func (s *Service) send(m *domain.TaskMessage, from *domain.Session) {
	_, err := s.Delivery.Deliver(m.ToSessionID, "msg:"+m.ID, s.messageTurn(m, from), func(at time.Time) {
		if d, err := s.messages.MarkDelivered(m.ID, at); err == nil {
			s.emitMessage(d)
		}
	})
	if err != nil {
		_ = s.messages.SetQueued(m.ID, false)
	}
}

// messageTurn is the user turn a message arrives as: the fixed header, then
// the subject, body and attachments.
func (s *Service) messageTurn(m *domain.TaskMessage, from *domain.Session) string {
	kind := string(m.Kind)
	switch {
	case m.Status != "":
		kind += "/" + string(m.Status)
	case m.Verdict != "":
		kind += "/" + string(m.Verdict)
	}
	var b strings.Builder
	b.WriteString("[task message " + m.ID + " · " + kind + " · from " + s.agentName(from) + " session " + m.FromSessionID + "]\n")
	if m.InReplyTo != "" {
		b.WriteString("In reply to " + m.InReplyTo + "\n")
	}
	if m.Subject != "" {
		b.WriteString("Subject: " + m.Subject + "\n")
	}
	b.WriteString(m.Body)
	if len(m.DocumentIDs) > 0 {
		b.WriteString("\nDocuments: " + strings.Join(m.DocumentIDs, ", "))
	}
	if len(m.ArtifactIDs) > 0 {
		b.WriteString("\nArtifacts: " + strings.Join(m.ArtifactIDs, ", "))
	}
	return b.String()
}

// ListTaskMessages is every message on the task, oldest first.
func (s *Service) ListTaskMessages(_ context.Context, taskID string, f ports.TaskMessageFilter) ([]*domain.TaskMessage, error) {
	if taskID == "" {
		return nil, invalid("ticket_id is required")
	}
	return s.messages.List(taskID, f), nil
}

// ListSessionMessages is the task's messages as the viewer may see them.
func (s *Service) ListSessionMessages(ctx context.Context, viewerSessionID string, f ports.TaskMessageFilter) ([]*domain.TaskMessage, error) {
	sc, err := s.scopeOf(ctx, viewerSessionID)
	if err != nil {
		return nil, err
	}
	if a := sc.architect(); a == nil || a.ID != viewerSessionID {
		f.SessionID = viewerSessionID
	}
	return s.messages.List(sc.ticket.ID, f), nil
}

// Recover hands the courier the messages that were waiting in an outbox when
// the server stopped, for recipients still running; the others stay
// undelivered, to be read with list_task_messages.
func (s *Service) Recover() {
	for _, m := range s.messages.ListQueued() {
		to := s.sessions.Get(m.ToSessionID)
		if to == nil || to.Status.IsTerminal() || s.Delivery == nil {
			_ = s.messages.SetQueued(m.ID, false)
			continue
		}
		s.send(m, s.sessions.Get(m.FromSessionID))
	}
}
