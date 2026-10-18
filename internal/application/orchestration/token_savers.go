package orchestration

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/domain"
)

// A session's token savers (ADR 0015): preparing it asks Capabilities once
// which savers resolve on for its task, repository and project, keeps those
// installed as its active savers, and wires each into its config through
// the saver's saverWiring. The set is stored with the session; a resume
// wires the stored set again and never resolves.

// sessionSavers is what preparing a session settled about its savers.
type sessionSavers struct {
	// active were wired into its config.
	active []domain.TokenSaver
	// notInstalled resolved on but are not installed: the session runs
	// without them and says so.
	notInstalled []domain.TokenSaver
}

// saverSession is what wiring one active saver into a session sees.
type saverSession struct {
	cfg *llmkit.SessionConfig
	// installation is the saver's: Path is its executable, or caveman's
	// ruleset.
	installation *domain.TokenSaverInstallation
	// worktree is the session's working dir; checkout is its repository's
	// root dir, "" on a resume.
	worktree, checkout string
	// resumed is a resume: the worktree already holds whatever a start put
	// in it.
	resumed bool
}

// saverWiring adds one active saver to a session's config. A saver with no
// wiring here is never active, installed or not.
type saverWiring func(s *Service, w saverSession) error

var saverWirings = map[domain.TokenSaver]saverWiring{
	domain.TokenSaverRTK:     (*Service).wireRTK,
	domain.TokenSaverCaveman: (*Service).wireCaveman,
}

// withTokenSavers resolves the session's savers and wires the active ones
// into cfg. A saver never stops a session: when Capabilities cannot answer,
// the session starts with none.
func (s *Service) withTokenSavers(in prepareInput, checkout, worktree string, cfg *llmkit.SessionConfig) sessionSavers {
	if s.TokenSavers == nil {
		return sessionSavers{}
	}
	ctx := context.TODO()
	resolved, err := s.TokenSavers.ResolveTokenSavers(ctx, in.ProjectID, in.RepositoryID, in.TicketID)
	if err != nil {
		slog.Warn("token savers not resolved; the session starts without them", "session", cfg.ID, "err", err)
		return sessionSavers{}
	}
	var on []domain.TokenSaver
	for _, r := range resolved {
		if r.Value == domain.ActivationOn {
			on = append(on, r.Saver)
		}
	}
	if len(on) == 0 {
		return sessionSavers{}
	}
	installed, err := s.installedSavers(ctx)
	if err != nil {
		slog.Warn("token saver installations not read; the session starts without them", "session", cfg.ID, "err", err)
		return sessionSavers{}
	}
	var out sessionSavers
	for _, saver := range on {
		inst, ok := installed[saver]
		if !ok {
			out.notInstalled = append(out.notInstalled, saver)
			continue
		}
		if s.wireSaver(saver, saverSession{cfg: cfg, installation: inst, worktree: worktree, checkout: checkout}) {
			out.active = append(out.active, saver)
		}
	}
	return out
}

// rewireTokenSavers wires a resumed session's stored active savers into
// cfg again, without resolving. One removed since it started is left out.
func (s *Service) rewireTokenSavers(sess *domain.Session, cfg *llmkit.SessionConfig) {
	if s.TokenSavers == nil || len(sess.TokenSavers) == 0 {
		return
	}
	installed, err := s.installedSavers(context.TODO())
	if err != nil {
		slog.Warn("token saver installations not read; the session resumes without them", "session", sess.ID, "err", err)
		return
	}
	for _, saver := range sess.TokenSavers {
		inst, ok := installed[saver]
		if !ok {
			slog.Warn("token saver no longer installed; the session resumes without it", "session", sess.ID, "saver", saver)
			continue
		}
		s.wireSaver(saver, saverSession{cfg: cfg, installation: inst, worktree: sess.WorkingDir, resumed: true})
	}
}

func (s *Service) installedSavers(ctx context.Context) (map[domain.TokenSaver]*domain.TokenSaverInstallation, error) {
	list, err := s.TokenSavers.InstalledTokenSavers(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[domain.TokenSaver]*domain.TokenSaverInstallation, len(list))
	for _, in := range list {
		out[in.Saver] = in
	}
	return out, nil
}

// wireSaver wires one saver; false when it has no wiring or its wiring
// failed, which leaves it out of the session.
func (s *Service) wireSaver(saver domain.TokenSaver, w saverSession) bool {
	wire := saverWirings[saver]
	if wire == nil {
		return false
	}
	if err := wire(s, w); err != nil {
		slog.Warn("token saver not wired; the session runs without it", "session", w.cfg.ID, "saver", saver, "err", err)
		return false
	}
	return true
}

// wireRTK runs rtk's own hook before every Bash call, beside the edit
// proxy's, with its telemetry off. rtk's config stays wherever rtk keeps it.
func (s *Service) wireRTK(w saverSession) error {
	hook := []claude.Hook{{Type: "command", Command: shellQuote(w.installation.Path) + " hook claude"}}
	dc, ok := withHooks(w.cfg.Driver, map[claude.HookEvent][]claude.HookMatcher{
		claude.HookPreToolUse: {{Matcher: "Bash", Hooks: hook}},
	})
	if !ok {
		return errNotClaude
	}
	w.cfg.Driver = dc
	withEnv(w.cfg, "RTK_TELEMETRY_DISABLED=1", "DO_NOT_TRACK=1")
	return nil
}

var errNotClaude = errors.New("the session does not run claude")

// wireCaveman appends caveman's ruleset to the system prompt at the
// configured intensity, then the harness's instruction keeping person-facing
// text in normal prose (ADR 0013). The ruleset is caveman's "full"; lite and
// ultra add a line of the harness's own after it.
func (s *Service) wireCaveman(w saverSession) error {
	b, err := os.ReadFile(w.installation.Path)
	if err != nil {
		return err
	}
	rules := withoutFrontMatter(string(b))
	if rules == "" {
		return errors.New("caveman's ruleset is empty")
	}
	system := appendSection(w.cfg.AppendSystem, rules)
	switch s.TokenSavers.CavemanIntensity(context.TODO()) {
	case domain.CavemanLite:
		system = appendSection(system, cavemanLiteLine)
	case domain.CavemanUltra:
		system = appendSection(system, cavemanUltraLine)
	}
	w.cfg.AppendSystem = appendSection(system, personFacingText)
	withEnv(w.cfg, "DO_NOT_TRACK=1")
	return nil
}

// The intensity lines the harness appends after caveman's ruleset.
const (
	cavemanLiteLine  = "Caveman intensity: lite. Softer than the rules above: keep articles and full sentences, and drop only filler, pleasantries and hedging."
	cavemanUltraLine = "Caveman intensity: ultra. Harder than the rules above: fragments only, no articles, no connecting words, abbreviations wherever the meaning stays clear."
)

// personFacingText keeps caveman out of what a person reads (the Glossary's
// "Person-facing text"). Appended after the ruleset, it wins over the
// ruleset's own list of things written in plain prose, which names tickets.
const personFacingText = `Person-facing text stays in normal prose. The terse style above does not apply to what a person reads: documents (specs, ADRs, glossary, contracts), task comments, review requests, notifications, commit messages, pull request text, code and code comments, and the replies of an interactive session. Write those in clear, complete sentences, as you would without these rules.

Everything else stays terse, and that includes ticket documents and task messages between sessions, as well as a headless session's replies and its narration between tool calls. This overrides any exception in the rules above that lists tickets among the things written in plain prose.`

// withoutFrontMatter is a SKILL.md without its YAML front matter, trimmed.
func withoutFrontMatter(skill string) string {
	if rest, ok := strings.CutPrefix(skill, "---\n"); ok {
		if _, body, ok := strings.Cut(rest, "\n---\n"); ok {
			skill = body
		}
	}
	return strings.TrimSpace(skill)
}

// withEnv adds each NAME=value to cfg's env unless NAME is already set
// there, so two savers asking for the same variable set it once.
func withEnv(cfg *llmkit.SessionConfig, kvs ...string) {
	env := slices.Clone(cfg.Env)
	for _, kv := range kvs {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.ContainsFunc(env, func(e string) bool { return strings.HasPrefix(e, name+"=") }) {
			env = append(env, kv)
		}
	}
	cfg.Env = env
}

// shellQuote is s as one word of a POSIX shell command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
