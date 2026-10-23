// Package claudecli is the ports.Launcher for the interactive claude CLI: it
// maps an AgentSpec onto llmkit's interactive mode (ADR 0007), which builds
// claude's command line and routes it through the LLM proxy, and runs it in
// the host's shell.
package claudecli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/adapter/out/agents/claudehome"
	"operators-mcp/internal/ports"
)

// Kind is the AgentSpec.Kind this adapter serves.
const Kind = "claude"

var _ ports.Launcher = Launcher{}

// Interactive starts claude on a terminal: *claude.Manager.
type Interactive interface {
	StartInteractive(ctx context.Context, cfg claude.InteractiveConfig) (claude.Terminal, error)
}

// Launcher starts interactive claude sessions.
type Launcher struct {
	claude Interactive
	// sandbox confines a session whose spec carries a policy; allow is what
	// this host adds to every such policy (its Claude home, its claude's
	// install dir).
	sandbox ports.SandboxDriver
	allow   []ports.SandboxPath
	// home, when set, is the Claude home claude runs in; each session's
	// worktree is trusted in it before the start.
	home *claudehome.Home
}

// NewLauncher returns the launcher starting claude through c.
func NewLauncher(c Interactive) Launcher { return Launcher{claude: c} }

// WithSandbox makes l confine sessions whose spec has a policy with d, adding
// allow to the policy. Without a driver such a spec is refused: it never runs
// unconfined by accident.
func (l Launcher) WithSandbox(d ports.SandboxDriver, allow ...ports.SandboxPath) Launcher {
	l.sandbox, l.allow = d, allow
	return l
}

// WithHome has l trust each session's worktree in h before starting claude,
// so a fresh Claude home shows no trust prompt.
func (l Launcher) WithHome(h claudehome.Home) Launcher {
	l.home = &h
	return l
}

func (Launcher) Kind() string { return Kind }

// Launch starts spec on a terminal of size. The shell prepares the command
// llmkit built, as it prepares any agent's: a login shell sources the
// person's profile and then the spec's env file.
func (l Launcher) Launch(ctx context.Context, spec ports.AgentSpec, size ports.TermSize, sh ports.Shell) (ports.PTYProcess, error) {
	cfg, err := Config(spec)
	if err != nil {
		return nil, err
	}
	if spec.Sandbox != nil && l.sandbox == nil {
		return nil, fmt.Errorf("claudecli: the session is sandboxed but this host has no sandbox driver")
	}
	if spec.Sandbox != nil {
		// The session's temp dir is made where the session runs, which is
		// not always where the server did.
		for _, e := range spec.Env {
			if dir, ok := strings.CutPrefix(e, "TMPDIR="); ok && dir != "" {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					return nil, fmt.Errorf("claudecli: make the session's temp dir: %w", err)
				}
			}
		}
	}
	if l.home != nil && spec.Dir != "" {
		if err := l.home.TrustFolder(spec.Dir); err != nil {
			return nil, fmt.Errorf("claudecli: trust the worktree in the agent home: %w", err)
		}
	}
	cfg.Size = claude.TermSize{Cols: size.Cols, Rows: size.Rows}
	cfg.Wrap = func(c claude.Command) (claude.Command, error) {
		shellCmd := ports.ShellCommand{Program: c.Path, Args: c.Args, Dir: c.Dir, Env: c.Env, EnvFile: spec.EnvFile}
		if spec.Sandbox == nil {
			ps, err := sh.Prepare(ctx, shellCmd)
			if err != nil {
				return claude.Command{}, err
			}
			return claude.Command{Path: ps.Path, Args: ps.Args, Dir: ps.Dir, Env: ps.Env, Files: c.Files}, nil
		}
		policy := *spec.Sandbox
		for _, p := range l.allow {
			if p.Write {
				policy.Writable = append(slices.Clone(policy.Writable), p.Path)
			} else {
				policy.Readable = append(slices.Clone(policy.Readable), p.Path)
			}
		}
		for _, f := range c.Files {
			policy.Readable = append(slices.Clone(policy.Readable), f)
		}
		confine := func(cmd ports.SandboxCommand) (ports.SandboxCommand, error) { return l.sandbox.Wrap(policy, cmd) }
		var ps ports.ProcessSpec
		var err error
		if cs, ok := sh.(ports.ConfiningShell); ok {
			// A login shell runs outside the sandbox, so its profile reads
			// the home directory; only the claude it execs is confined.
			ps, err = cs.PrepareConfined(ctx, shellCmd, confine)
		} else {
			if ps, err = sh.Prepare(ctx, shellCmd); err == nil {
				var w ports.SandboxCommand
				w, err = confine(ports.SandboxCommand{Path: ps.Path, Args: ps.Args, Dir: ps.Dir, Env: ps.Env})
				ps = ports.ProcessSpec{Path: w.Path, Args: w.Args, Dir: w.Dir, Env: w.Env}
			}
		}
		if err != nil {
			return claude.Command{}, err
		}
		return claude.Command{Path: ps.Path, Args: ps.Args, Dir: ps.Dir, Env: ps.Env, Files: c.Files}, nil
	}
	t, err := l.claude.StartInteractive(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return terminal{t}, nil
}

// Config is spec as llmkit's interactive mode takes it. It carries what the
// headless session does minus the approval server: in a terminal claude asks
// its questions itself. It adds the hooks that report back to the server:
// the conversation it is in, and where its turns start and end. The session
// id is the attribution of its LLM requests.
func Config(spec ports.AgentSpec) (claude.InteractiveConfig, error) {
	if spec.Kind != "" && spec.Kind != Kind {
		return claude.InteractiveConfig{}, fmt.Errorf("claudecli: cannot run a %q agent", spec.Kind)
	}
	if spec.Conversation.ID == "" {
		return claude.InteractiveConfig{}, fmt.Errorf("claudecli: no conversation id")
	}
	cfg := claude.InteractiveConfig{
		Dir:          spec.Dir,
		Env:          spec.Env,
		Conversation: spec.Conversation.ID,
		Resume:       spec.Conversation.Resume,
		Model:        spec.Model,
		AppendSystem: spec.AppendSystem,
		SkillDirs:    spec.SkillDirs,
		AddDirs:      spec.AddDirs,
		AllowedTools: spec.AllowedTools,
		Prompt:       spec.Prompt,
		Attribution:  spec.SessionID,
		Settings:     sessionSettings(spec.HookURL, spec.Hooks, spec.Settings),
	}
	switch spec.Permission {
	case "accept_edits":
		cfg.PermissionMode = "acceptEdits"
	case "bypass":
		cfg.PermissionMode = "bypassPermissions"
	}
	for _, sv := range spec.MCPServers {
		cfg.MCPServers = append(cfg.MCPServers, llmkit.MCPServerSpec{
			Name: sv.Name, Transport: sv.Transport, Command: sv.Command, URL: sv.URL,
			Args: sv.Args, Env: sv.Env, Headers: sv.Headers,
		})
	}
	return cfg, nil
}

// sessionSettings are the --settings whose hooks post their input to url,
// with the hook's name added as event=: SessionStart names the conversation,
// UserPromptSubmit starts a turn and Stop ends one. The spec's more hooks (a
// task session's edit proxy) are added beside them. --settings layers over
// the user's own settings rather than replacing them. extra are the spec's
// other settings keys, merged verbatim. Nil when there are neither.
//
// The hooks must never disturb the session. SessionStart's and
// UserPromptSubmit's stdout would be added to claude's context, so their
// response is discarded. Stop's is passed through: it is how the server hands
// claude turns that waited for this one to end ({"decision":"block",...}),
// and it is empty otherwise. A server that is down only costs the
// five-second timeout, and the session goes on as if it had answered nothing.
func sessionSettings(url string, more map[string][]ports.HookGroup, extra map[string]json.RawMessage) *claude.Settings {
	hooks := map[claude.HookEvent][]claude.HookMatcher{}
	if url != "" {
		sep := "&"
		if !strings.Contains(url, "?") {
			sep = "?"
		}
		post := func(event, out string) []claude.HookMatcher {
			cmd := "curl -fsS -m 5 -X POST -H 'Content-Type: application/json' --data-binary @- '" +
				url + sep + "event=" + event + "' " + out + " || true"
			return []claude.HookMatcher{{Hooks: []claude.Hook{{Type: "command", Command: cmd}}}}
		}
		hooks[claude.HookSessionStart] = post("SessionStart", ">/dev/null 2>&1")
		hooks[claude.HookUserPromptSubmit] = post("UserPromptSubmit", ">/dev/null 2>&1")
		hooks[claude.HookStop] = post("Stop", "2>/dev/null")
	}
	for event, groups := range more {
		for _, g := range groups {
			m := claude.HookMatcher{Matcher: g.Matcher}
			for _, h := range g.Hooks {
				m.Hooks = append(m.Hooks, claude.Hook{Type: h.Type, Command: h.Command})
			}
			hooks[claude.HookEvent(event)] = append(hooks[claude.HookEvent(event)], m)
		}
	}
	if len(hooks) == 0 && len(extra) == 0 {
		return nil
	}
	s := &claude.Settings{Extra: extra}
	if len(hooks) > 0 {
		s.Hooks = hooks
	}
	return s
}

// terminal is llmkit's terminal as the host runs it.
type terminal struct{ claude.Terminal }

func (t terminal) Resize(s ports.TermSize) error {
	return t.Terminal.Resize(claude.TermSize{Cols: s.Cols, Rows: s.Rows})
}
