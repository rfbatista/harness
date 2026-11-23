package orchestration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/rfbatista/llmkit"

	"operators-mcp/internal/domain"
)

// SessionContext reads what session id's context was when claude started and
// resumed it, its use of tools and skills, and what changed (ADR 0099). A
// session that started before the harness recorded context reads as not
// recorded.
func (s *Service) SessionContext(ctx context.Context, id string) (domain.SessionContextView, error) {
	if s.sessions.Get(id) == nil {
		return domain.SessionContextView{}, sessionContextNotFound()
	}
	c, err := s.loadContext(ctx, id)
	if err != nil {
		return domain.SessionContextView{}, err
	}
	return c.View(id), nil
}

// SessionContextPiece reads the whole text of one piece of one entry.
func (s *Service) SessionContextPiece(ctx context.Context, id, entryID, pieceID string) (string, error) {
	if s.sessions.Get(id) == nil {
		return "", sessionContextNotFound()
	}
	c, err := s.loadContext(ctx, id)
	if err != nil {
		return "", err
	}
	if c == nil {
		return "", &domain.StructuredError{Code: "CONTEXT_ENTRY_NOT_FOUND", Message: "context entry not found"}
	}
	entry, ok := c.Entry(entryID)
	if !ok {
		return "", &domain.StructuredError{Code: "CONTEXT_ENTRY_NOT_FOUND", Message: "context entry not found"}
	}
	piece, ok := entry.Piece(pieceID)
	if !ok {
		return "", &domain.StructuredError{Code: "CONTEXT_PIECE_NOT_FOUND", Message: "context piece not found"}
	}
	return piece.Text, nil
}

func sessionContextNotFound() error {
	return &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
}

// loadContext reads the stored context: nil when there is none, or when this
// server stores none.
func (s *Service) loadContext(ctx context.Context, id string) (*domain.SessionContext, error) {
	if s.Contexts == nil {
		return nil, nil
	}
	return s.Contexts.Load(ctx, id)
}

// recordEntry stores e as the session's newest context entry and keeps the
// earlier ones.
func (s *Service) recordEntry(ctx context.Context, id string, e domain.ContextEntry) error {
	if s.Contexts == nil {
		return nil
	}
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	c, err := s.Contexts.Load(ctx, id)
	if err != nil {
		return err
	}
	if c == nil {
		c = &domain.SessionContext{SessionID: id}
	}
	c.Entries = append(c.Entries, e)
	return s.Contexts.Save(ctx, c)
}

// contextFacts are the session's setup at a glance, from what prepare
// resolved.
func (s *Service) contextFacts(in prepareInput, ag resolvedAgent, cfg llmkit.SessionConfig, ws *domain.Workspace, savers sessionSavers, sb sandboxed, permission llmkit.PermissionMode) domain.ContextFacts {
	f := domain.ContextFacts{
		Role:           string(in.Role),
		Mode:           string(in.Mode),
		Model:          cfg.Model,
		Headless:       in.Headless,
		PermissionMode: string(permission),
		Sandbox:        "off",
		Worktree:       ws.Path,
		Branch:         in.Branch,
		TokenSavers:    []string{},
	}
	if ag.agent != nil {
		f.Agent = ag.agent.Name
	}
	if sb.facts != nil {
		f.RepositoryVisibility = sb.facts.Visibility
		if sb.facts.Sandboxed && sb.facts.Driver != "" {
			f.Sandbox = sb.facts.Driver
		}
	}
	for _, t := range savers.active {
		f.TokenSavers = append(f.TokenSavers, string(t))
	}
	return f
}

// recordStart stores the context entry of a session that has just been
// recorded, from what prepare resolved and the opening message it was given.
// A failure to store is logged: it must not fail a start that already ran.
func (s *Service) recordStart(ctx context.Context, id string, p *prepared, opening string) {
	in := p.context
	in.ID, in.Kind, in.At, in.Opening = newContextEntryID(), domain.ContextEntryStart, time.Now(), opening
	if err := s.recordEntry(ctx, id, buildContextEntry(in)); err != nil {
		slog.Warn("store a session's context", "session", id, "err", err)
	}
}

func newContextEntryID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
