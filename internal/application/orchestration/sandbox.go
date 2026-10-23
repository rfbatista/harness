package orchestration

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/rfbatista/llmkit"

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
	// TempDir gives a session its temp dir. Nil: the system's.
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
	// Repositories are the checkouts of every repository of the project.
	Repositories []string
	// VirtualShell is the session's shell; empty without one.
	VirtualShell string
	TempDir      string
	AgentHome    string
	// Allow is the rest of the allowlist; Extra the person's own paths.
	Allow []ports.SandboxPath
	Extra []ports.SandboxPath
}

// BuildSandboxPolicy turns a session into its sandbox policy. It alone
// decides repository visibility by mode: implement, design and default see
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
		add(s.GitCommonDir, true)
		for _, r := range s.Repositories {
			add(r, false)
		}
	default:
		add(s.Worktree, true)
		add(s.GitCommonDir, true)
	}
	for _, p := range slices.Concat(s.Allow, s.Extra) {
		add(p.Path, p.Write)
	}

	policy := ports.SandboxPolicy{WorkingDir: s.Worktree}
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
		AgentHome:    sb.Home,
		TempDir:      os.TempDir(),
	}
	if sb.TempDir != nil {
		sess.TempDir = sb.TempDir(id)
	}
	if in.Mode == domain.SessionModeArchitect || in.Mode == domain.SessionModeProduct {
		repos, err := s.catalog.Repositories.ListRepositories(ctx, in.ProjectID)
		if err != nil {
			return sandboxed{}, err
		}
		for _, r := range repos {
			sess.Repositories = append(sess.Repositories, r.RootDir)
		}
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
	link := filepath.Join(worktree, ".git")
	fi, err := os.Stat(link)
	if err != nil {
		return fallback
	}
	if fi.IsDir() {
		return link
	}
	b, err := os.ReadFile(link)
	if err != nil {
		return fallback
	}
	dir, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return fallback
	}
	dir = strings.TrimSpace(dir)
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(worktree, dir)
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

// collectViolations starts following the sandbox violations of a sandboxed
// session's process tree, storing each for the session. The returned stop ends
// it; it is nil when nothing is collected. Call it before the session's process
// runs, so nothing it is denied is missed.
func (s *Service) collectViolations(id, workingDir string, facts *domain.SandboxFacts) (stop func()) {
	sb := s.Sandbox
	if sb == nil || sb.Violations == nil || sb.ViolationStore == nil || facts == nil || !facts.Sandboxed {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := sb.Violations.Follow(ctx, workingDir)
	if err != nil {
		cancel()
		slog.Warn("follow sandbox violations failed", "session", id, "err", err)
		return nil
	}
	go func() {
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
