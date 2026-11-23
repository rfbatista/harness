package orchestration

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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
	if e.Kind == domain.ContextEntryResume && len(c.Entries) > 0 {
		markChanged(&e, c.Entries[len(c.Entries)-1])
	}
	c.Entries = append(c.Entries, e)
	c.Changes = append(c.Changes, domain.ContextChange{
		At: e.At, Kind: domain.ContextChangeEntryAdded, Subject: e.ID, Detail: entryAddedDetail(e),
	})
	return s.Contexts.Save(ctx, c)
}

func entryAddedDetail(e domain.ContextEntry) string {
	if e.Kind == domain.ContextEntryResume {
		return "Resumed: the context was handed to claude again"
	}
	return "Started: the context was handed to claude"
}

// markChanged marks each piece of e that differs from the piece of the same id
// in prev: a version change, or for a piece with no version a change of text,
// told by short content hashes. A piece prev did not have is not marked.
func markChanged(e *domain.ContextEntry, prev domain.ContextEntry) {
	for i, p := range e.Instructions {
		before, ok := prev.Piece(p.ID)
		if !ok {
			continue
		}
		switch {
		case p.Version != "" || before.Version != "":
			if p.Version != before.Version {
				e.Instructions[i].Changed = &domain.ContextPieceChange{FromVersion: before.Version, ToVersion: p.Version}
			}
		case p.Text != before.Text:
			e.Instructions[i].Changed = &domain.ContextPieceChange{FromVersion: shortHash(before.Text), ToVersion: shortHash(p.Text)}
		}
	}
}

// shortHash is the first 8 hex digits of text's SHA-256.
func shortHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:4])
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
	if in.Facts.Headless {
		s.keepRedactor(id, in.Cfg)
	}
	if err := s.recordEntry(ctx, id, buildContextEntry(in)); err != nil {
		slog.Warn("store a session's context", "session", id, "err", err)
	}
}

func newContextEntryID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// recordResume stores the context entry of a session that has just been
// resumed, from what the resume resolved and the first message it was given.
func (s *Service) recordResume(ctx context.Context, sess *domain.Session, in contextInput, opening string) {
	in.ID, in.Kind, in.At, in.Opening = newContextEntryID(), domain.ContextEntryResume, time.Now(), opening
	if err := s.recordEntry(ctx, sess.ID, buildContextEntry(in)); err != nil {
		slog.Warn("store a session's context", "session", sess.ID, "err", err)
	}
}

// repositoryName is the name of repository id, for a piece's source; "" when
// it cannot be read.
func (s *Service) repositoryName(ctx context.Context, id string) string {
	if id == "" {
		return ""
	}
	repo, err := s.catalog.Repositories.GetRepository(context.WithoutCancel(ctx), id)
	if err != nil || repo == nil {
		return ""
	}
	return repo.Name
}

// resumeFacts are a resumed session's setup at a glance.
func (s *Service) resumeFacts(sess *domain.Session, ag resolvedAgent, cfg llmkit.SessionConfig, permission llmkit.PermissionMode, sb sandboxed) domain.ContextFacts {
	in := prepareInput{Role: s.sessionRole(context.TODO(), sess.ID), Mode: sess.Mode, Branch: sess.Branch, Headless: false}
	return s.contextFacts(in, ag, cfg, &domain.Workspace{Path: sess.WorkingDir}, sessionSavers{active: sess.TokenSavers}, sb, permission)
}
