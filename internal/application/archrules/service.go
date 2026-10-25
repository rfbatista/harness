// Package archrules checks a session's actions against its repository's
// architecture rules: the hooks, the PATH shims and the commit check all ask
// it (ADR 0033). It reads the repository's current saved record on every
// check, so a saved rule protects running sessions at once, and records every
// rule that fails.
package archrules

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"operators-mcp/internal/application/repoarch"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.ArchitectureRules      = (*Service)(nil)
	_ ports.ArchitectureRuleEvents = (*Service)(nil)
)

// Deps are the Service's collaborators.
type Deps struct {
	Sessions interface {
		Get(id string) *domain.Session
	}
	Agents  ports.AgentLister
	Records interface {
		Current(ctx context.Context, repositoryID string) (*repoarch.Current, error)
	}
	Events ports.ArchitectureRuleEventStore
	// Feed announces each event; nil announces none.
	Feed ports.ProjectChangeSink
	Now  func() time.Time
	// EnvFiles are the repositories' saved env files, whose root .env a
	// command rule sees; nil gives it the server's environment alone.
	EnvFiles ports.EnvFileLister
	// Run runs command rules; nil runs them with sh.
	Run Runner
}

// Service checks architecture rules.
type Service struct{ d Deps }

// NewService returns the rule checker.
func NewService(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Run == nil {
		d.Run = ShellRunner
	}
	return &Service{d: d}
}

// Subscribe registers the context's reactions to other contexts' events: a
// session's events go with it.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(ctx context.Context, ev domain.SessionDeleted) error {
		return s.d.Events.DeleteBySession(ctx, ev.SessionID)
	})
}

// who is a session as the rules see it: the repository it works in, its
// worktree and its agent's name (plain when it has none).
type who struct {
	sess  *domain.Session
	agent string
	cur   *repoarch.Current
}

// resolve answers the session's record, or false when no rule applies to it:
// no such session, no repository or worktree, or no saved record. An error
// says the record could not be read.
func (s *Service) resolve(ctx context.Context, sessionID string) (who, bool, error) {
	sess := s.d.Sessions.Get(sessionID)
	if sess == nil || sess.RepositoryID == "" || sess.WorkingDir == "" {
		return who{}, false, nil
	}
	cur, err := s.d.Records.Current(ctx, sess.RepositoryID)
	if err != nil {
		return who{}, false, err
	}
	if cur == nil {
		return who{}, false, nil
	}
	// An agent that cannot be read is treated as plain: a lookup failure never
	// stops the check, and plain is the narrowest agent.
	agent := domain.PlainAgent
	if sess.AgentID != "" {
		if a, err := s.d.Agents.GetAgent(ctx, sess.AgentID); err == nil && a != nil && a.Name != "" {
			agent = a.Name
		}
	}
	return who{sess: sess, agent: agent, cur: cur}, true, nil
}

// CheckArchitecture implements ports.ArchitectureRules.
func (s *Service) CheckArchitecture(ctx context.Context, c ports.ArchitectureCheck) ports.ArchitectureVerdict {
	w, ok, err := s.resolve(ctx, c.SessionID)
	if err != nil {
		return ports.ArchitectureVerdict{Context: "Architecture rules could not be checked: the saved architecture could not be read."}
	}
	if !ok {
		return ports.ArchitectureVerdict{}
	}
	paths := inside(w.sess.WorkingDir, c.Paths)
	if c.Shim {
		paths = withContents(w.sess.WorkingDir, paths)
	}
	var v verdict
	if c.Point == domain.PointBeforeTool || c.Point == domain.PointBeforeCommit {
		s.editOutsideLayer(ctx, w, c, paths, &v)
	}
	// A commit the layers already fail runs no command: it is failing.
	if len(v.deny) == 0 || c.Point != domain.PointBeforeCommit {
		s.commandRules(ctx, w, c, paths, &v)
	}
	return v.out()
}

// ListArchitectureRuleEvents implements ports.ArchitectureRuleEvents.
func (s *Service) ListArchitectureRuleEvents(ctx context.Context, repositoryID string, since time.Time, limit int) ([]domain.ArchitectureRuleEvent, error) {
	return s.d.Events.List(ctx, repositoryID, since, limit)
}

// verdict collects what the failed rules say.
type verdict struct {
	deny, context []string
}

func (v *verdict) add(mode, text string) {
	if mode == domain.ModeBlocking {
		v.deny = append(v.deny, text)
	} else {
		v.context = append(v.context, text)
	}
}

func (v *verdict) out() ports.ArchitectureVerdict {
	return ports.ArchitectureVerdict{Deny: strings.Join(v.deny, "\n"), Context: strings.Join(v.context, "\n")}
}

// editOutsideLayer applies the record's edit-outside-layer rule: every path
// whose layer does not grant the agent change fails it.
func (s *Service) editOutsideLayer(ctx context.Context, w who, c ports.ArchitectureCheck, paths []string, v *verdict) {
	rec := w.cur.Compiled.Record
	rule := rec.EditOutsideLayerRule()
	if !names(rule.Agents, w.agent) {
		return
	}
	for _, p := range paths {
		level, layer := w.cur.Compiled.AccessOf(w.agent, p)
		if level == domain.AccessChange {
			continue
		}
		if layer == "" {
			layer = "unlayered"
		}
		v.add(rule.Mode, "Architecture rule "+rule.ID+" ("+rule.Mode+"): "+rule.Message+" — "+p+" is in layer "+layer+", which "+w.agent+" may not change.")
		s.record(ctx, w, rule, c, p, layer, "")
	}
}

// names says whether agent is in list; an empty list names every agent.
func names(list []string, agent string) bool {
	if len(list) == 0 {
		return true
	}
	for _, n := range list {
		if n == agent {
			return true
		}
	}
	return false
}

// record stores a failed rule and puts it on the project's feed.
func (s *Service) record(ctx context.Context, w who, rule domain.ArchRule, c ports.ArchitectureCheck, path, layer, detail string) {
	ev := &domain.ArchitectureRuleEvent{
		At: s.d.Now().UTC(), SessionID: w.sess.ID, TaskID: w.sess.TicketID, Agent: w.agent, RuleID: rule.ID, Kind: rule.Kind,
		AtPoint: c.Point, Mode: rule.Mode, Outcome: domain.OutcomeWarned, Path: path, Layer: layer, Tool: c.Tool, Detail: detail,
		RepositoryID: w.sess.RepositoryID, ProjectID: w.sess.ProjectID,
	}
	if rule.Mode == domain.ModeBlocking {
		ev.Outcome = domain.OutcomeBlocked
	}
	if s.d.Events.Add(ctx, ev) != nil {
		return
	}
	if s.d.Feed != nil {
		s.d.Feed.AnnounceChange(ports.ProjectChange{ArchitectureRuleEvent: &ports.ArchitectureRuleEventChange{
			RepositoryID: ev.RepositoryID, Event: ev, ProjectID: ev.ProjectID,
		}})
	}
}

// inside answers the paths that are in the worktree, relative to it with
// forward slashes. A relative path is the worktree's own.
func inside(worktree string, paths []string) []string {
	root := resolve(worktree)
	var out []string
	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(worktree, abs)
		}
		rel, err := filepath.Rel(root, resolve(abs))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

// resolve is p with the symlinks of its longest existing ancestor resolved,
// so a file not yet written compares like one that is.
func resolve(p string) string {
	p = filepath.Clean(p)
	for dir, rest := p, ""; ; {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return p
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

// maxExpanded caps the files a directory operation is expanded into.
const maxExpanded = 5000

// withContents adds, for each path (relative to the worktree) that is a
// directory, the files under it: `rm -r internal` changes every layer inside
// internal, not just the folder.
func withContents(worktree string, paths []string) []string {
	out := slices.Clone(paths)
	for _, p := range paths {
		root := filepath.Join(worktree, filepath.FromSlash(p))
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(f string, d fs.DirEntry, err error) error {
			if err != nil || len(out) >= maxExpanded {
				return filepath.SkipAll
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if rel, err := filepath.Rel(worktree, f); err == nil {
				out = append(out, filepath.ToSlash(rel))
			}
			return nil
		})
	}
	return out
}
