package taskchannel

import (
	"context"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// RequestUserReview opens a pending review request for the person.
func (s *Service) RequestUserReview(ctx context.Context, architectID string, in ports.ReviewRequestInput) (*domain.ReviewRequest, error) {
	sc, err := s.architectScope(ctx, architectID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Subject) == "" || strings.TrimSpace(in.Body) == "" {
		return nil, invalid("subject and body are required")
	}
	if in.AboutSessionID != "" && sc.member(in.AboutSessionID) == nil {
		return nil, notFound("SESSION_NOT_ON_TASK", "session "+in.AboutSessionID+" is not on this task")
	}
	if err := s.checkAttachments(ctx, sc.ticket.ID, in.DocumentIDs, in.ArtifactIDs); err != nil {
		return nil, err
	}
	now := s.now()
	r, err := s.reviews.Create(&domain.ReviewRequest{
		TaskID: sc.ticket.ID, ProjectID: sc.ticket.ProjectID, ArchitectSessionID: architectID,
		AboutSessionID: in.AboutSessionID, Subject: strings.TrimSpace(in.Subject), Body: in.Body,
		DocumentIDs: ids(in.DocumentIDs), ArtifactIDs: ids(in.ArtifactIDs),
		State: domain.ReviewPending, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return nil, err
	}
	s.emitReview(r)
	return r, nil
}

// WithdrawReview withdraws one of the task's pending review requests.
func (s *Service) WithdrawReview(ctx context.Context, architectID, reviewID string) (*domain.ReviewRequest, error) {
	sc, err := s.architectScope(ctx, architectID)
	if err != nil {
		return nil, err
	}
	r := s.reviews.Get(reviewID)
	if r == nil || r.TaskID != sc.ticket.ID {
		return nil, notFound("REVIEW_NOT_FOUND", "review request not found on this task")
	}
	if r.State != domain.ReviewPending {
		return nil, &domain.StructuredError{Code: "REVIEW_NOT_PENDING", Message: "review request is already " + string(r.State)}
	}
	r.State, r.UpdatedAt = domain.ReviewWithdrawn, s.now()
	if err := s.reviews.Update(r); err != nil {
		return nil, err
	}
	s.emitReview(r)
	return r, nil
}

// ListReviewRequests is the task's review requests, newest first.
func (s *Service) ListReviewRequests(_ context.Context, taskID string, state domain.ReviewState) ([]*domain.ReviewRequest, error) {
	if taskID == "" {
		return nil, invalid("ticket_id is required")
	}
	if _, err := domain.ParseReviewState(string(state)); err != nil {
		return nil, err
	}
	return s.reviews.List(ports.ReviewFilter{TaskID: taskID, State: state}), nil
}

// ListProjectReviewRequests is the project's review requests, newest first.
func (s *Service) ListProjectReviewRequests(_ context.Context, projectID string, state domain.ReviewState) ([]*domain.ReviewRequest, error) {
	if projectID == "" {
		return nil, invalid("project_id is required")
	}
	if _, err := domain.ParseReviewState(string(state)); err != nil {
		return nil, err
	}
	return s.reviews.List(ports.ReviewFilter{ProjectID: projectID, State: state}), nil
}

// RespondReview settles a pending review with the person's decision and
// delivers it to the architect that asked; if that one is not running, to
// the task's current architect.
func (s *Service) RespondReview(_ context.Context, reviewID string, decision domain.ReviewState, note string) (*domain.ReviewRequest, bool, error) {
	if decision != domain.ReviewApproved && decision != domain.ReviewChangesRequested {
		return nil, false, invalid("decision must be approved or changes_requested")
	}
	note = strings.TrimSpace(note)
	if decision == domain.ReviewChangesRequested && note == "" {
		return nil, false, invalid("say what to change: a note is required for changes_requested")
	}
	r := s.reviews.Get(reviewID)
	if r == nil {
		return nil, false, notFound("REVIEW_NOT_FOUND", "review request not found")
	}
	if r.State != domain.ReviewPending {
		return nil, false, &domain.StructuredError{Code: "REVIEW_NOT_PENDING", Message: "review request is already " + string(r.State)}
	}
	now := s.now()
	r.State, r.ResponseNote, r.RespondedAt, r.UpdatedAt = decision, note, &now, now
	if err := s.reviews.Update(r); err != nil {
		return nil, false, err
	}
	s.emitReview(r)

	to := s.sessions.Get(r.ArchitectSessionID)
	if to == nil || to.Status.IsTerminal() {
		to = domain.TaskArchitect(s.taskSessions(r.TaskID))
	}
	if to == nil || to.Status.IsTerminal() || s.Delivery == nil {
		return r, false, nil
	}
	turn := "[review response " + r.ID + " · review_response · " + string(decision) + "]\nSubject: " + r.Subject
	if note != "" {
		turn += "\n" + note
	}
	delivered, err := s.Delivery.Deliver(to.ID, "review:"+r.ID, turn, func(time.Time) {})
	return r, err == nil && delivered, nil
}
