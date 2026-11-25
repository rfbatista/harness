package productowner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// oneOffTimeout bounds a one-off run: a digest is a short summary.
const oneOffTimeout = 10 * time.Minute

// RunOnce runs the product owner once, with no person and no thread (ADR
// 0113): a headless session in product-owner mode, started as a thread's is,
// with the same sandbox, read-only tools and guardrails. It is given prompt,
// and answers the text of its answer. The session ends with the run and stays
// out of every session surface, as a thread's does.
func (s *Service) RunOnce(ctx context.Context, projectID, prompt string) (string, error) {
	if err := s.available(); err != nil {
		return "", err
	}
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return "", err
	}
	st, err := s.store.Settings(ctx, projectID)
	if err != nil {
		return "", err
	}
	sessionID, err := s.sessions.StartProductOwnerSession(ctx, ports.ProductOwnerSessionSpec{ProjectID: projectID, AgentID: st.AgentID, Prompt: st.Prompt})
	if err != nil {
		return "", unavailableOr(err)
	}
	ch, _, unsub := s.sessions.Subscribe(sessionID)
	defer unsub()
	defer func() {
		bg := context.Background()
		_ = s.sessions.Stop(bg, sessionID)
		s.waitProcessEnd(bg, sessionID)
	}()
	if err := s.sessions.Send(ctx, sessionID, prompt); err != nil {
		return "", fmt.Errorf("send the one-off prompt: %w", err)
	}
	var answer string
	timeout := time.After(oneOffTimeout)
	for done := false; !done; {
		select {
		case ev, ok := <-ch:
			if !ok {
				done = true
				break
			}
			switch ev.Type {
			case "output":
				if strings.TrimSpace(ev.Text) != "" {
					answer = ev.Text
				}
			case "usage":
				done = true
			}
		case <-timeout:
			return "", &domain.StructuredError{Code: "TIMEOUT", Message: "the one-off product owner run timed out"}
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if strings.TrimSpace(answer) == "" {
		return "", &domain.StructuredError{Code: "NO_ANSWER", Message: "the one-off product owner run ended without an answer"}
	}
	return answer, nil
}
