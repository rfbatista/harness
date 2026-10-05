// Package apps runs a repository's application from a session's worktree:
// a saved run command or an ad-hoc command line, in a terminal on the
// server, its output kept so it can be read again. It is a context of its
// own: it reads sessions and saved commands through read ports and spawns on
// the terminal host.
package apps

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strings"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.AppRunner = (*Service)(nil)

// CommandKind is the terminal host's agent kind for a plain command line.
const CommandKind = "command"

const (
	// keepPerSession bounds the runs remembered per session; the oldest ended
	// ones go first.
	keepPerSession = 20
	// startSize is the terminal a run starts at; the browser resizes it.
	startCols, startRows = 120, 32
)

// Service implements ports.AppRunner.
type Service struct {
	sessions ports.SessionReader
	commands ports.RunCommandLister
	host     ports.TerminalHost
	now      func() time.Time

	mu   sync.Mutex
	runs map[string]*run
}

// NewService returns the runner. commands may be nil (only ad-hoc commands).
func NewService(sessions ports.SessionReader, commands ports.RunCommandLister, host ports.TerminalHost) *Service {
	return &Service{sessions: sessions, commands: commands, host: host, now: time.Now, runs: map[string]*run{}}
}

type run struct {
	rec  domain.AppRun
	term ports.Terminal
	log  *logBuffer
}

// Start runs the saved command called name, or the command line given.
func (s *Service) Start(ctx context.Context, sessionID, name, command string) (*domain.AppRun, error) {
	sess, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess.WorkingDir == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "this session has no worktree to run the application in"}
	}

	name, command = strings.TrimSpace(name), strings.TrimSpace(command)
	if name != "" {
		if existing := s.running(sessionID, name); existing != nil {
			return existing, nil
		}
		command, err = s.savedCommand(ctx, sess.RepositoryID, name)
		if err != nil {
			return nil, err
		}
	} else if command == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "pick a saved command or type one to run"}
	}
	if len(command) > domain.MaxRunCommandLength {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "the command is too long"}
	}

	id := "run-" + randomHex()
	rec := domain.AppRun{
		ID: id, SessionID: sessionID, Name: cmp.Or(name, command), Command: command,
		Dir: sess.WorkingDir, Status: domain.AppRunRunning, StartedAt: s.now(),
	}
	// Recorded before it starts, so an exit that comes at once is recorded too.
	r := &run{rec: rec, log: newLogBuffer()}
	s.mu.Lock()
	s.runs[id] = r
	s.mu.Unlock()

	spec := ports.AgentSpec{Kind: CommandKind, SessionID: sessionID, Dir: sess.WorkingDir, Command: command}
	if err := s.host.Spawn(ctx, id, spec, ports.TermSize{Cols: startCols, Rows: startRows}, func(code int) { s.ended(id, code) }); err != nil {
		s.mu.Lock()
		delete(s.runs, id)
		s.mu.Unlock()
		return nil, err
	}
	term, err := s.host.Attach(id)
	if err != nil {
		// It exited before it could be watched; its output is gone with it.
		term = &endedTerminal{run: r, svc: s}
		r.log.append([]byte("(the command exited before its output could be read)\r\n"))
		r.log.finish()
	} else {
		go r.log.collect(term)
	}
	s.mu.Lock()
	r.term = term
	s.prune(sessionID)
	out := r.rec
	s.mu.Unlock()
	return &out, nil
}

func (s *Service) savedCommand(ctx context.Context, repositoryID, name string) (string, error) {
	if s.commands != nil && repositoryID != "" {
		list, err := s.commands.ListRunCommands(ctx, repositoryID)
		if err != nil {
			return "", err
		}
		for _, c := range list {
			if c.Name == name {
				return c.Command, nil
			}
		}
	}
	return "", &domain.StructuredError{Code: "RUN_COMMAND_NOT_FOUND", Message: "this repository has no run command called " + name}
}

// running is the session's live run of the saved command name, if any.
func (s *Service) running(sessionID, name string) *domain.AppRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.runs {
		if r.rec.SessionID == sessionID && r.rec.Name == name && r.rec.Status == domain.AppRunRunning {
			out := r.rec
			return &out
		}
	}
	return nil
}

func (s *Service) ended(id string, code int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return
	}
	if r.rec.Status == domain.AppRunRunning {
		r.rec.Status = domain.AppRunExited
	}
	r.rec.ExitCode = code
	now := s.now()
	r.rec.EndedAt = &now
}

// prune forgets the session's oldest ended runs beyond keepPerSession.
// The caller holds s.mu.
func (s *Service) prune(sessionID string) {
	var mine []*run
	for _, r := range s.runs {
		if r.rec.SessionID == sessionID {
			mine = append(mine, r)
		}
	}
	if len(mine) <= keepPerSession {
		return
	}
	slices.SortFunc(mine, func(a, b *run) int { return a.rec.StartedAt.Compare(b.rec.StartedAt) })
	for _, r := range mine[:len(mine)-keepPerSession] {
		if r.rec.Status != domain.AppRunRunning {
			delete(s.runs, r.rec.ID)
		}
	}
}

// Subscribe stops a session's runs when the session is deleted, before its
// worktree goes.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(ctx context.Context, ev domain.SessionDeleted) error {
		s.StopSession(ctx, ev.SessionID)
		return nil
	})
}

// StopSession stops every running run of the session and forgets them all.
func (s *Service) StopSession(ctx context.Context, sessionID string) {
	s.mu.Lock()
	var ids []string
	for id, r := range s.runs {
		if r.rec.SessionID == sessionID {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		_, _ = s.Stop(ctx, id)
	}
	s.mu.Lock()
	for _, id := range ids {
		delete(s.runs, id)
	}
	s.mu.Unlock()
}

// List returns the session's runs, newest first.
func (s *Service) List(_ context.Context, sessionID string) ([]*domain.AppRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []*domain.AppRun{}
	for _, r := range s.runs {
		if r.rec.SessionID == sessionID {
			rec := r.rec
			out = append(out, &rec)
		}
	}
	slices.SortFunc(out, func(a, b *domain.AppRun) int { return b.StartedAt.Compare(a.StartedAt) })
	return out, nil
}

// Stop kills a running run; an ended one is returned as it is.
func (s *Service) Stop(_ context.Context, runID string) (*domain.AppRun, error) {
	s.mu.Lock()
	r, ok := s.runs[runID]
	if !ok {
		s.mu.Unlock()
		return nil, errRunNotFound
	}
	if r.rec.Status == domain.AppRunRunning {
		r.rec.Status = domain.AppRunStopped
	}
	term := r.term
	s.mu.Unlock()
	if term != nil {
		_ = term.Kill() // returns once the process is gone; ended() records the code
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := r.rec
	return &out, nil
}

// Attach returns the run's terminal, replaying its output so far.
func (s *Service) Attach(_ context.Context, runID string) (ports.Terminal, error) {
	s.mu.Lock()
	r, ok := s.runs[runID]
	var term ports.Terminal
	if ok {
		term = r.term
	}
	s.mu.Unlock()
	if !ok || term == nil {
		return nil, errRunNotFound
	}
	return &logTerminal{Terminal: term, log: r.log}, nil
}

var errRunNotFound = &domain.StructuredError{Code: "RUN_NOT_FOUND", Message: "run not found"}

func randomHex() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
