package orchestration

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
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
	// cavemanIntensity is what caveman was wired at; "" when it is not
	// active.
	cavemanIntensity domain.CavemanIntensity
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
	// cavemanIntensity is the session's, read once at start and stored.
	cavemanIntensity domain.CavemanIntensity
}

// saverWiring adds one active saver to a session's config. A saver with no
// wiring here is never active, installed or not.
type saverWiring func(s *Service, w saverSession) error

var saverWirings = map[domain.TokenSaver]saverWiring{
	domain.TokenSaverRTK:       (*Service).wireRTK,
	domain.TokenSaverCaveman:   (*Service).wireCaveman,
	domain.TokenSaverCodegraph: (*Service).wireCodegraph,
}

// withTokenSavers resolves the session's savers and wires the active ones
// into cfg. A saver never stops a session: when Capabilities cannot answer,
// the session starts with none.
func (s *Service) withTokenSavers(ctx context.Context, in prepareInput, checkout, worktree string, cfg *llmkit.SessionConfig) sessionSavers {
	if s.TokenSavers == nil {
		return sessionSavers{}
	}
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
	intensity := s.TokenSavers.CavemanIntensity(ctx)
	for _, saver := range on {
		inst, ok := installed[saver]
		if !ok {
			out.notInstalled = append(out.notInstalled, saver)
			continue
		}
		if s.wireSaver(saver, saverSession{cfg: cfg, installation: inst, worktree: worktree, checkout: checkout, cavemanIntensity: intensity}) {
			out.active = append(out.active, saver)
			if saver == domain.TokenSaverCaveman {
				out.cavemanIntensity = intensity
			}
		}
	}
	return out
}

// rewireTokenSavers wires a resumed session's stored active savers into
// cfg again, without resolving. One removed since it started is left out
// and recorded as not installed: the resume goes on without it.
func (s *Service) rewireTokenSavers(ctx context.Context, sess *domain.Session, cfg *llmkit.SessionConfig) {
	if s.TokenSavers == nil || len(sess.TokenSavers) == 0 {
		return
	}
	installed, err := s.installedSavers(ctx)
	if err != nil {
		slog.Warn("token saver installations not read; the session resumes without them", "session", sess.ID, "err", err)
		return
	}
	// A session stored before intensities were kept resumes at today's.
	intensity := sess.CavemanIntensity
	if intensity == "" {
		intensity = s.TokenSavers.CavemanIntensity(ctx)
	}
	var active, gone []domain.TokenSaver
	for _, saver := range sess.TokenSavers {
		inst, ok := installed[saver]
		if !ok {
			slog.Warn("token saver no longer installed; the session resumes without it", "session", sess.ID, "saver", saver)
			gone = append(gone, saver)
			continue
		}
		active = append(active, saver)
		s.wireSaver(saver, saverSession{cfg: cfg, installation: inst, worktree: sess.WorkingDir, resumed: true, cavemanIntensity: intensity})
	}
	if len(gone) == 0 {
		return
	}
	notInstalled := append(slices.Clone(sess.TokenSaversNotInstalled), gone...)
	if err := s.sessions.UpdateTokenSavers(sess.ID, active, notInstalled); err != nil {
		slog.Warn("token savers not recorded for the resumed session", "session", sess.ID, "err", err)
	}
}

func (s *Service) installedSavers(ctx context.Context) (map[domain.TokenSaver]*domain.TokenSaverInstallation, error) {
	list, err := s.TokenSavers.InstalledTokenSavers(ctx)
	if err != nil {
		return nil, err
	}
	return domain.InstallationsBySaver(list), nil
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
// proxy's, with its telemetry off. The hook rewrites a command to a bare
// "rtk …", so rtk's dir goes first on the session's PATH too. rtk's config
// stays wherever rtk keeps it.
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
	withPathDir(w.cfg, filepath.Dir(w.installation.Path))
	return nil
}

var errNotClaude = errors.New("the session does not run claude")

// wireCaveman appends caveman's ruleset to the system prompt at the
// session's intensity, then the harness's instruction keeping person-facing
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
	switch w.cavemanIntensity {
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

// withEnv sets each NAME=value in cfg's env, replacing any value NAME
// already has there in place: a saver's telemetry stays off whatever the
// session's env says, and two savers asking for the same variable set it
// once.
func withEnv(cfg *llmkit.SessionConfig, kvs ...string) {
	env := slices.Clone(cfg.Env)
	for _, kv := range kvs {
		name, _, _ := strings.Cut(kv, "=")
		if i := slices.IndexFunc(env, func(e string) bool { return strings.HasPrefix(e, name+"=") }); i >= 0 {
			env[i] = kv
		} else {
			env = append(env, kv)
		}
	}
	cfg.Env = env
}

// withPathDir puts dir first on cfg's PATH: the PATH cfg's env already has
// (the shims'), or the server's own when it has none.
func withPathDir(cfg *llmkit.SessionConfig, dir string) {
	path := os.Getenv("PATH")
	if i := slices.IndexFunc(cfg.Env, func(e string) bool { return strings.HasPrefix(e, "PATH=") }); i >= 0 {
		path = strings.TrimPrefix(cfg.Env[i], "PATH=")
	}
	if path != "" {
		dir += string(os.PathListSeparator) + path
	}
	withEnv(cfg, "PATH="+dir)
}

// shellQuote is s as one word of a POSIX shell command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
