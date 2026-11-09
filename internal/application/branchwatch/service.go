// Package branchwatch keeps each session's Branches list and the after-turn
// check that finds branches and pushes no branch request explains (ADR 0070).
package branchwatch

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.SessionBranchLists = (*Service)(nil)

// Deps are the Service's collaborators.
type Deps struct {
	Sessions     ports.SessionRepository
	Workspaces   ports.WorkspaceRepository
	Repositories ports.RepositoryRepository
	Requests     ports.BranchRequestStore
	Store        ports.BranchWatchStore
	Pushes       ports.RecordedPushStore
	Evidence     ports.GitBranchEvidence
	Git          ports.GitWorktreeReader
	// Events announces findings to the inbox, Feed the lists to the project
	// feed; nil announces nothing.
	Events ports.EventPublisher
	Feed   ports.ProjectChangeSink
	// Debounce is how long turn ends are gathered into one check.
	Debounce time.Duration
	Now      func() time.Time
}

// Service derives the Branches lists from stored state and runs the checks.
type Service struct {
	d Deps

	mu    sync.Mutex
	repos map[string]*repoCheck
	// sent is the last list announced per session, so an unchanged one is not
	// announced again.
	sent map[string][]byte
}

// repoCheck is the check of one repository: at most one runs at a time, and
// turn ends that arrive meanwhile are one more run.
type repoCheck struct {
	running bool
	timer   *time.Timer
	ended   map[string]bool
}

// NewService returns the service.
func NewService(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Debounce == 0 {
		d.Debounce = 150 * time.Millisecond
	}
	return &Service{d: d, repos: map[string]*repoCheck{}, sent: map[string][]byte{}}
}

// Subscribe registers the service's reactions: a session's starting snapshot,
// the check at every turn end, and the list following the requests.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(ctx context.Context, ev domain.SessionWorktreeProvisioned) error {
		s.sessionStarted(context.WithoutCancel(ctx), ev.SessionID)
		return nil
	})
	ports.On(sub, func(ctx context.Context, ev domain.SessionTurnEnded) error {
		s.turnEnded(ev.SessionID)
		return nil
	})
	ports.On(sub, func(ctx context.Context, ev domain.BranchRequestChanged) error {
		s.announceList(context.WithoutCancel(ctx), ev.Request.SessionID)
		return nil
	})
}

func snapshotOf(snap *ports.GitBranchSnapshot, at time.Time) ports.BranchBaseline {
	return ports.BranchBaseline{Branches: snap.Branches, RemoteRefs: snap.RemoteRefs, Remotes: snap.Remotes, TakenAt: at}
}

func sessionKey(id string) string { return "session:" + id }
func repoKey(id string) string    { return "repo:" + id }

// sessionStarted stores the starting snapshot of the session's repository.
func (s *Service) sessionStarted(ctx context.Context, sessionID string) {
	sess := s.d.Sessions.Get(sessionID)
	if sess == nil || sess.RepositoryID == "" {
		return
	}
	repo := s.d.Repositories.Get(ctx, sess.RepositoryID)
	if repo == nil || repo.RootDir == "" {
		return
	}
	snap, err := s.d.Evidence.BranchSnapshot(ctx, repo.RootDir)
	if err != nil {
		slog.Warn("snapshot a session's branches", "session", sessionID, "err", err)
		return
	}
	b := snapshotOf(snap, s.d.Now())
	if err := s.d.Store.SaveBaseline(ctx, sessionKey(sessionID), b); err != nil {
		slog.Warn("store a session's starting snapshot", "session", sessionID, "err", err)
	}
	// The repository's latest snapshot starts here when no check made one.
	if prev, _ := s.d.Store.Baseline(ctx, repoKey(repo.ID)); prev == nil {
		_ = s.d.Store.SaveBaseline(ctx, repoKey(repo.ID), b)
	}
}

// announceList sends the session's list on the project feed when it changed.
func (s *Service) announceList(ctx context.Context, sessionID string) {
	if s.d.Feed == nil {
		return
	}
	sess := s.d.Sessions.Get(sessionID)
	if sess == nil {
		return
	}
	list, err := s.SessionBranches(ctx, sessionID)
	if err != nil {
		slog.Warn("read a session's branches", "session", sessionID, "err", err)
		return
	}
	enc, _ := json.Marshal(list)
	s.mu.Lock()
	same := bytes.Equal(s.sent[sessionID], enc)
	s.sent[sessionID] = enc
	s.mu.Unlock()
	if same {
		return
	}
	s.d.Feed.AnnounceChange(ports.ProjectChange{SessionBranches: &ports.SessionBranchesChange{
		SessionBranches: domain.SessionBranches{SessionID: sessionID, Branches: list}, ProjectID: sess.ProjectID,
	}})
}
