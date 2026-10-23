package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/application/agentready"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// SettingSandbox is the setting that switches the agent sandbox: "off" turns
// it off, anything else (and unset) leaves it on.
const SettingSandbox = "agent_sandbox"

// Sandbox is the agent sandbox step of a session start (ADR 0017): it builds
// the session's sandbox policy and hands the session's command to the driver.
// A Service without one runs sessions unconfined; the app wires it.
type Sandbox struct {
	// Driver is the platform's sandbox driver; nil is a platform with none.
	Driver ports.SandboxDriver
	// Enabled says whether the sandbox is on, read at each session start. Nil
	// is on.
	Enabled func() bool
	// Bin is the claude the interactive dry-run wrap names. Empty: "claude".
	Bin string
	// Home is the agent Claude home.
	Home string
	// UserHome is the person's home directory and ConfigHome their
	// XDG_CONFIG_HOME (empty when unset): their git config is read-only to
	// every session, so git inside the sandbox sees their identity and
	// aliases. Empty UserHome: none.
	UserHome, ConfigHome string
	// TempDir gives the path prefix of a session's temp dir: each start makes
	// its own dir as <prefix>-<random> beside it (0700), so a start never
	// shares a dir with the one before it under the same id. Nil: the
	// system's temp dir.
	TempDir func(sessionID string) string
	// Allow gives the allowlist a session adds to the baseline: token-saver
	// dirs, toolchain dirs and caches, llmkit's Files, the harness's git
	// credential material. Nil: none.
	Allow func(sessionID string) []ports.SandboxPath
	// Extra are the paths the person adds in settings. Nil: none.
	Extra ports.SandboxExtraPaths
	// Violations follows what the sandbox denies a session's processes, where
	// the platform can. Nil: none is collected (Linux).
	Violations ports.SandboxViolationSource
	// WorkspacesRoot gives the directory session worktrees are made under, read
	// at each session start. Nil: none is hidden.
	WorkspacesRoot func() string
	// Checkouts gives every project repository checkout of the install, for
	// the one-shot calls, which see no project. Nil: none.
	Checkouts func() []string
	// ViolationStore keeps what Violations follows. Nil: nothing is kept.
	ViolationStore ports.SandboxViolationStore
}

// SandboxSwitch is the Enabled of a Sandbox that follows the SettingSandbox
// setting.
func SandboxSwitch(r ports.SettingsReader) func() bool {
	return func() bool { return r == nil || r.Setting(SettingSandbox) != "off" }
}

func (b *Sandbox) on() bool { return b != nil && (b.Enabled == nil || b.Enabled()) }

// SandboxSession is what the policy builder decides a policy from.
type SandboxSession struct {
	Mode domain.SessionMode
	// Worktree is the session's own worktree, GitCommonDir the git directory
	// it commits into.
	Worktree     string
	GitCommonDir string
	// OwnGitDir is the session's own admin entry under GitCommonDir/worktrees
	// (empty for a checkout that is not a linked worktree). A writing session
	// gets every other entry of that directory read-only, so a prune or gc it
	// runs cannot delete the worktrees it cannot see.
	OwnGitDir string
	// Repositories are the checkouts of every repository of the project, for
	// every mode: the mode decides which are granted, the rest are hidden.
	Repositories []string
	// WorkspacesRoot is the directory session worktrees are made under; it is
	// hidden, the session's own worktree inside it granted.
	WorkspacesRoot string
	// VirtualShell is the session's shell; empty without one.
	VirtualShell string
	TempDir      string
	AgentHome    string
	// UserHome and ConfigHome are the person's home and XDG config home; their
	// git config is read-only in every mode, never their ssh folder.
	UserHome, ConfigHome string
	// Allow is the rest of the allowlist; Extra the person's own paths.
	Allow []ports.SandboxPath
	Extra []ports.SandboxPath
	// OneShot marks a one-shot claude call: it sees only its temp dir (also
	// its working directory), the agent home and Allow, never a repository.
	OneShot bool
}

// hiddenPaths are the repository checkouts and the workspaces root that
// nothing granted reaches into: each one the policy does not grant, listed
// once. A path equal to or inside a granted one is not hidden (the grant
// would be cut off); a grant inside a hidden path is re-allowed after the
// deny, so it stays reachable.
func hiddenPaths(s SandboxSession, granted []ports.SandboxPath) []string {
	var hidden []string
	for _, p := range append(slices.Clone(s.Repositories), s.WorkspacesRoot) {
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		inside := slices.ContainsFunc(granted, func(g ports.SandboxPath) bool {
			return p == g.Path || strings.HasPrefix(p, g.Path+"/")
		})
		if !inside && !slices.Contains(hidden, p) {
			hidden = append(hidden, p)
		}
	}
	return hidden
}

// BuildSandboxPolicy turns a session into its sandbox policy. It alone
// decides repository visibility by mode (what is not granted lands in Hidden,
// wherever it lies): implement, design and default see
// their own worktree and git directory read-write; architect also reads every
// other repository of the project; product reads every repository and writes
// none.
func BuildSandboxPolicy(s SandboxSession) ports.SandboxPolicy {
	var all []ports.SandboxPath
	add := func(path string, write bool) {
		if path != "" {
			all = append(all, ports.SandboxPath{Path: filepath.Clean(path), Write: write})
		}
	}
	add(s.AgentHome, true)
	for _, p := range gitConfigPaths(s.UserHome, s.ConfigHome) {
		add(p, false)
	}
	if s.OneShot {
		add(s.TempDir, true)
		if resolved, err := filepath.EvalSymlinks(s.TempDir); err == nil && s.TempDir != "" {
			add(resolved, true)
		}
		for _, p := range s.Allow {
			add(p.Path, p.Write)
		}
		policy := splitPolicy(all, s.TempDir)
		policy.Hidden = hiddenPaths(s, all)
		return policy
	}
	add(s.VirtualShell, true)
	add(s.TempDir, true)
	switch s.Mode {
	case domain.SessionModeProduct:
		add(s.Worktree, false)
		for _, r := range s.Repositories {
			add(r, false)
		}
	case domain.SessionModeArchitect:
		add(s.Worktree, true)
		addGitDir(add, s)
		for _, r := range s.Repositories {
			add(r, false)
		}
	default:
		add(s.Worktree, true)
		addGitDir(add, s)
	}
	for _, p := range slices.Concat(s.Allow, s.Extra) {
		add(p.Path, p.Write)
	}

	policy := splitPolicy(all, s.Worktree)
	policy.Hidden = hiddenPaths(s, all)
	return policy
}

// addGitDir grants the common git directory read-write, its worktrees
// directory read-only as a whole, then the session's own entry in it
// read-write: the drivers let the deeper rule win, so the other worktrees'
// admin entries stay out of reach while commits, refs and objects still work.
// A session cannot create worktrees under it; the harness makes them.
func addGitDir(add func(string, bool), s SandboxSession) {
	if s.GitCommonDir == "" {
		return
	}
	add(s.GitCommonDir, true)
	add(filepath.Join(s.GitCommonDir, "worktrees"), false)
	add(s.OwnGitDir, true)
}

// gitConfigPaths are the person's git config: ~/.gitconfig and the git
// folder of ~/.config and of XDG_CONFIG_HOME. Credential folders (~/.ssh)
// are never among them: pushes use the harness's git credentials.
func gitConfigPaths(home, xdg string) []string {
	if home == "" {
		return nil
	}
	out := []string{filepath.Join(home, ".gitconfig"), filepath.Join(home, ".config", "git")}
	if xdg != "" {
		out = append(out, filepath.Join(xdg, "git"))
	}
	return out
}

// splitPolicy sorts paths into the policy's readable and writable sets: a
// path that is writable anywhere is writable, each path listed once.
func splitPolicy(all []ports.SandboxPath, workingDir string) ports.SandboxPolicy {
	policy := ports.SandboxPolicy{WorkingDir: workingDir}
	writable := map[string]bool{}
	for _, p := range all {
		if p.Write {
			writable[p.Path] = true
		}
	}
	seen := map[string]bool{}
	for _, p := range all {
		if seen[p.Path] {
			continue
		}
		seen[p.Path] = true
		if writable[p.Path] {
			policy.Writable = append(policy.Writable, p.Path)
		} else {
			policy.Readable = append(policy.Readable, p.Path)
		}
	}
	return policy
}

// OneShotWrap is the WrapFunc confining a one-shot claude call (a summary, a
// translation, generated text), read at each call: nil with the sandbox off,
// the missing-driver refusal when it is on without one. The policy exposes no
// repository: the one-shot's temp dir, the agent home and allow (claude's
// install dirs), through the same policy builder as a session's.
func (b *Sandbox) OneShotWrap(allow []ports.SandboxPath) (llmkit.WrapFunc, error) {
	if !b.on() {
		return nil, nil
	}
	if b.Driver == nil {
		return nil, agentready.SandboxDriverMissing()
	}
	// The system temp dir stays read-write here: llmkit's contract makes it
	// the one-shot's working directory, and claude writes its scratch and
	// lock files under TMPDIR, so a read-only cwd would fail calls that
	// otherwise work. A one-shot runs no tools and sees no repository, which
	// is why it does not get a temp dir of its own as a session does.
	sess := SandboxSession{OneShot: true, TempDir: os.TempDir(), AgentHome: b.Home, UserHome: b.UserHome, ConfigHome: b.ConfigHome, Allow: allow}
	if b.Checkouts != nil {
		sess.Repositories = b.Checkouts()
	}
	if b.WorkspacesRoot != nil {
		sess.WorkspacesRoot = b.WorkspacesRoot()
	}
	return sandboxWrap(b.Driver, sess), nil
}

// claudeSandboxOff is the settings value that keeps claude's own Bash sandbox
// off: inside the agent sandbox it would nest a second Seatbelt, and a
// repository's .claude/settings.json (project settings are read) could switch
// it on. --settings is flag-level, which outranks project settings.
var claudeSandboxOff = json.RawMessage(`{"enabled":false}`)

// withClaudeSandboxOff adds claudeSandboxOff to a headless session's
// --settings, next to what it already has.
func withClaudeSandboxOff(cfg *llmkit.SessionConfig) {
	var dc claude.Config
	switch d := cfg.Driver.(type) {
	case nil:
	case claude.Config:
		dc = d
	default:
		return
	}
	var settings claude.Settings
	if dc.Settings != nil {
		settings = *dc.Settings
	}
	extra := make(map[string]json.RawMessage, len(settings.Extra)+1)
	for k, v := range settings.Extra {
		extra[k] = v
	}
	extra["sandbox"] = claudeSandboxOff
	settings.Extra = extra
	dc.Settings = &settings
	cfg.Driver = dc
}

// confineSpec marks an interactive spec as running under policy: the policy
// itself and the claude settings that go with it. A nil policy leaves it
// unconfined.
func confineSpec(spec *ports.AgentSpec, policy *ports.SandboxPolicy) {
	spec.Sandbox = policy
	if policy != nil {
		spec.Settings = map[string]json.RawMessage{"sandbox": claudeSandboxOff}
	}
}

// withTempDir gives a sandboxed session its own temp dir: made 0700 before
// the start, named by TMPDIR, TMP and TEMP in its env so its tools use it,
// and removed with the session (the cleanup it answers runs cleanup first).
// Without a Sandbox that is on, with a driver and a TempDir, it does nothing.
func (s *Service) withTempDir(id string, cfg *llmkit.SessionConfig, cleanup func()) (func(), error) {
	sb := s.Sandbox
	if !sb.on() || sb.Driver == nil || sb.TempDir == nil {
		return cleanup, nil
	}
	prefix := sb.TempDir(id)
	if prefix == "" {
		return cleanup, nil
	}
	// One dir per start: a cleanup of an earlier start under the same id
	// removes only its own, never the live one.
	dir, err := mkdirTempBeside(prefix)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, fmt.Errorf("prepare the session's temp dir: %w", err)
	}
	cfg.Env = append(slices.Clone(cfg.Env), "TMPDIR="+dir, "TMP="+dir, "TEMP="+dir)
	return func() {
		if cleanup != nil {
			cleanup()
		}
		_ = os.RemoveAll(dir)
	}, nil
}

// mkdirTempBeside makes a 0700 dir named prefix-<random> in prefix's parent.
// The name stays short: unix socket paths under it have a length limit.
func mkdirTempBeside(prefix string) (string, error) {
	parent := filepath.Dir(prefix)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(parent, filepath.Base(prefix)+"-*")
}

// sessionTempDir is the temp dir the session's env names (withTempDir), the
// one the policy must grant; the system's without one.
func sessionTempDir(env []string) string {
	dir := os.TempDir()
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "TMPDIR="); ok && v != "" {
			dir = v
		}
	}
	return dir
}

// sandboxed is the outcome of the sandbox step.
type sandboxed struct {
	policy *ports.SandboxPolicy
	// wrap is what llmkit runs on the session's final command: the driver's
	// wrap under the session's policy, with the files llmkit made for the
	// process added to it. Nil when the session is not sandboxed.
	wrap llmkit.WrapFunc
	// facts are what the session records about its sandbox; nil when the
	// service has no Sandbox step at all.
	facts *domain.SandboxFacts
}

// sandbox is the sandbox step of a session start: with the sandbox on it
// builds the session's policy and has the driver wrap its command. Off, or
// without a Sandbox, it does nothing; on without a driver, the start is
// refused.
func (s *Service) sandbox(ctx context.Context, in prepareInput, id string, repo *domain.Repository, worktree string, env []string) (sandboxed, error) {
	sb := s.Sandbox
	if sb == nil {
		return sandboxed{}, nil
	}
	if !sb.on() {
		return sandboxed{facts: &domain.SandboxFacts{Visibility: domain.SandboxUnrestricted}}, nil
	}
	if sb.Driver == nil {
		return sandboxed{}, agentready.SandboxDriverMissing()
	}
	sess := SandboxSession{
		Mode:         in.Mode,
		Worktree:     worktree,
		GitCommonDir: gitCommonDir(worktree, repo.RootDir),
		OwnGitDir:    ownGitDir(worktree),
		AgentHome:    sb.Home,
		UserHome:     sb.UserHome,
		ConfigHome:   sb.ConfigHome,
		TempDir:      sessionTempDir(env),
	}
	repos, err := s.catalog.Repositories.ListRepositories(ctx, in.ProjectID)
	if err != nil {
		return sandboxed{}, err
	}
	for _, r := range repos {
		sess.Repositories = append(sess.Repositories, r.RootDir)
	}
	if sb.WorkspacesRoot != nil {
		sess.WorkspacesRoot = sb.WorkspacesRoot()
	}
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, ports.ShellDirEnv+"="); ok {
			sess.VirtualShell = v
		}
	}
	if sb.Allow != nil {
		sess.Allow = sb.Allow(id)
	}
	if sb.Extra != nil {
		sess.Extra = sb.Extra()
	}
	sess.Allow = append(sess.Allow, sessionAllow(env)...)
	policy := BuildSandboxPolicy(sess)
	if !in.Headless {
		// An interactive session is wrapped where its terminal runs, from
		// this policy; wrapping once here refuses a start the driver cannot
		// confine before anything is provisioned for it.
		bin := sb.Bin
		if bin == "" {
			bin = "claude"
		}
		if _, err := sb.Driver.Wrap(policy, ports.SandboxCommand{Path: bin, Dir: worktree, Env: env}); err != nil {
			return sandboxed{}, err
		}
	}
	facts := &domain.SandboxFacts{Sandboxed: true, Visibility: domain.SandboxVisibilityFor(in.Mode), Driver: sb.Driver.Name()}
	return sandboxed{policy: &policy, wrap: sandboxWrap(sb.Driver, sess), facts: facts}, nil
}

// sandboxWrap is the WrapFunc confining a headless session's claude: the
// session's policy plus the files llmkit made for that process, read-only
// (the MCP config, the instructions dir), under the driver. The driver's
// error stops the start; llmkit removes the files it made.
func sandboxWrap(driver ports.SandboxDriver, sess SandboxSession) llmkit.WrapFunc {
	return func(c llmkit.Command) (llmkit.Command, error) {
		s := sess
		s.Allow = slices.Clone(sess.Allow)
		for _, f := range c.Files {
			s.Allow = append(s.Allow, ports.SandboxPath{Path: f})
		}
		out, err := driver.Wrap(BuildSandboxPolicy(s), ports.SandboxCommand{Path: c.Path, Args: c.Args, Dir: c.Dir, Env: c.Env})
		if err != nil {
			return llmkit.Command{}, err
		}
		return llmkit.Command{Path: out.Path, Args: out.Args, Dir: out.Dir, Env: out.Env, Files: c.Files}, nil
	}
}

var (
	knownHostsOption = regexp.MustCompile(`UserKnownHostsFile=([^'\s]+)`)
	credSocketOption = regexp.MustCompile(`--socket '([^']+)'`)
)

// sessionAllow are the paths a session's own env names that its processes
// must reach: its PATH shims, and the git material the harness made for it
// (its git directory, the known_hosts ssh updates, the sockets of its
// ssh-agent and credential cache).
func sessionAllow(env []string) []ports.SandboxPath {
	var out []ports.SandboxPath
	for _, e := range env {
		name, v, _ := strings.Cut(e, "=")
		switch name {
		case ports.ShimDirEnv:
			out = append(out, ports.SandboxPath{Path: v})
		case "GIT_CONFIG_GLOBAL", "GIT_CONFIG_VALUE_1":
			if name == "GIT_CONFIG_VALUE_1" {
				if m := credSocketOption.FindStringSubmatch(v); m != nil {
					out = append(out, ports.SandboxPath{Path: filepath.Dir(m[1]), Write: true})
				}
			} else if v != "" {
				out = append(out, ports.SandboxPath{Path: filepath.Dir(v), Write: true})
			}
		case "GIT_SSH_COMMAND":
			if m := knownHostsOption.FindStringSubmatch(v); m != nil {
				out = append(out, ports.SandboxPath{Path: m[1], Write: true})
			}
		}
	}
	return out
}

// gitCommonDir is the git directory a worktree commits into, read from the
// worktree's own files (no git process): its .git link, then that directory's
// commondir. A worktree with neither commits into its repository's .git.
func gitCommonDir(worktree, repoRoot string) string {
	fallback := filepath.Join(repoRoot, ".git")
	dir := ownGitDir(worktree)
	if dir == "" {
		if fi, err := os.Stat(filepath.Join(worktree, ".git")); err == nil && fi.IsDir() {
			return filepath.Join(worktree, ".git")
		}
		return fallback
	}
	if c, err := os.ReadFile(filepath.Join(dir, "commondir")); err == nil {
		common := strings.TrimSpace(string(c))
		if !filepath.IsAbs(common) {
			common = filepath.Join(dir, common)
		}
		return filepath.Clean(common)
	}
	return filepath.Dir(filepath.Dir(dir))
}

// ownGitDir is the admin entry a linked worktree's .git file points at
// (<common>/worktrees/<name>); empty when .git is not such a file.
func ownGitDir(worktree string) string {
	b, err := os.ReadFile(filepath.Join(worktree, ".git"))
	if err != nil {
		return ""
	}
	dir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return ""
	}
	dir = strings.TrimSpace(dir)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(worktree, dir)
	}
	return filepath.Clean(dir)
}

// collectViolations starts following the sandbox violations of a sandboxed
// session's process tree, storing each for the session. The returned stop ends
// it; it is nil when nothing is collected. Collection starts in the background,
// so a denial in the first moments of the process may be missed.
func (s *Service) collectViolations(id, workingDir string, facts *domain.SandboxFacts) (stop func()) {
	sb := s.Sandbox
	if sb == nil || sb.Violations == nil || sb.ViolationStore == nil || facts == nil || !facts.Sandboxed {
		return nil
	}
	// The log stream can take seconds to attach, so it is followed in the
	// background: a start is never delayed or refused by the collector, and a
	// stream that cannot be followed only means no violations are recorded.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ch, err := sb.Violations.Follow(ctx, workingDir)
		if err != nil {
			slog.Debug("follow sandbox violations failed", "session", id, "err", err)
			return
		}
		for v := range ch {
			if err := sb.ViolationStore.AddSandboxViolation(id, v); err != nil {
				slog.Warn("store sandbox violation failed", "session", id, "err", err)
			}
		}
	}()
	return cancel
}

// withViolations joins stop to cleanup, so collection ends with the session.
func withViolations(cleanup, stop func()) func() {
	if stop == nil {
		return cleanup
	}
	return func() {
		stop()
		if cleanup != nil {
			cleanup()
		}
	}
}

// SandboxViolations lists what the sandbox denied the session.
func (s *Service) SandboxViolations(id string) (ports.SandboxViolationLog, error) {
	log := ports.SandboxViolationLog{Violations: []ports.SandboxViolation{}}
	if s.sessions.Get(id) == nil {
		return log, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	sb := s.Sandbox
	if sb == nil || sb.Violations == nil || sb.ViolationStore == nil {
		return log, nil
	}
	log.Available = true
	got, err := sb.ViolationStore.SandboxViolations(id)
	if err != nil {
		return log, err
	}
	if got != nil {
		log.Violations = got
	}
	return log, nil
}
