package orchestration

import (
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// SessionTranscript returns a page of the conversation session id's claude
// saved: the one a resume would continue (claudeID), for a session in any
// status, interactive or headless, worktree or not.
func (s *Service) SessionTranscript(_ context.Context, id string, q ports.TranscriptQuery) (ports.TranscriptPage, error) {
	sess := s.sessions.Get(id)
	if sess == nil {
		return ports.TranscriptPage{}, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	if q.Limit == 0 {
		q.Limit = ports.TranscriptDefaultLimit
	}
	switch {
	case q.Limit < 1 || q.Limit > ports.TranscriptMaxLimit:
		return ports.TranscriptPage{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: "limit must be between 1 and 500"}
	case q.Before != "" && q.After != "":
		return ports.TranscriptPage{}, &domain.StructuredError{Code: "INVALID_INPUT", Message: "give before or after, not both"}
	case s.TranscriptReader == nil:
		return ports.TranscriptPage{}, &domain.StructuredError{Code: "SESSION_TRANSCRIPT_MISSING", Message: "this server reads no saved conversations"}
	}
	return s.TranscriptReader.ReadTranscript(sess.WorkingDir, claudeID(sess), q)
}
