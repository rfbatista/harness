// Package agentready decides whether an agent may start: it refuses a session
// start or a one-shot when the agent credential, the managed claude or (with
// the sandbox on) the sandbox driver is missing, and begins installing the
// managed claude's stable version when that is what is missing (ADR 0017,
// 0018, 0019).
package agentready

import (
	"context"
	"fmt"
	"strings"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The codes a refusal carries. SANDBOX_DRIVER_MISSING is the one the
// orchestration's sandbox step also answers.
const (
	CodeCredentialMissing = "AGENT_CREDENTIAL_MISSING"
	CodeClaudeMissing     = "MANAGED_CLAUDE_MISSING"
	CodeClaudeInstalling  = "MANAGED_CLAUDE_INSTALLING"
	CodeClaudeFailed      = "MANAGED_CLAUDE_INSTALL_FAILED"
	CodeSandboxMissing    = "SANDBOX_DRIVER_MISSING"
)

// Page is the Agent isolation page; each refusal links to the anchor of the
// section that fixes it.
const Page = "/agent-isolation"

// Anchors of the page's sections.
const (
	AnchorCredential = Page + "#credential"
	AnchorClaude     = Page + "#claude"
	AnchorSandbox    = Page + "#sandbox"
)

// Gate is the check. Every field is optional: what is nil is not enforced, so
// a service built without it starts agents as before.
type Gate struct {
	// Credential says whether the agent credential is set.
	Credential func() bool
	// Bin says which claude agents run and whether it is installed (the
	// -claude.bin override counts as installed).
	Bin ports.ManagedClaudeBin
	// Managed installs the claude when it is missing.
	Managed ports.ManagedClaude
	// SandboxOn says whether the sandbox is switched on; SandboxDriver
	// whether the platform has a driver. Both are read at each check.
	SandboxOn     func() bool
	SandboxDriver func() bool
}

type problem struct {
	code, text, anchor string
}

// Check answers nil when an agent may start. Otherwise it answers one error
// naming everything that is missing, so the person fixes it in one visit; its
// code is the first one's and its details list every one with its anchor. It
// never waits: a missing managed claude starts installing and the check
// answers MANAGED_CLAUDE_INSTALLING until the install is done.
func (g *Gate) Check(ctx context.Context) error {
	if g == nil {
		return nil
	}
	var ps []problem
	if g.Credential != nil && !g.Credential() {
		ps = append(ps, problem{CodeCredentialMissing,
			"no agent credential. Agents sign in with their own credential, not your Claude login. Add one in Agent isolation settings",
			AnchorCredential})
	}
	if g.Bin != nil && !g.Bin.Installed() {
		ps = append(ps, g.claudeProblem(ctx))
	}
	if g.SandboxOn != nil && g.SandboxDriver != nil && g.SandboxOn() && !g.SandboxDriver() {
		ps = append(ps, SandboxProblem())
	}
	return refusal(ps)
}

// SandboxProblem is the refusal of a sandbox that is on with no driver.
func SandboxProblem() problem {
	return problem{CodeSandboxMissing,
		"no sandbox driver. The agent sandbox is on but this platform has no driver. Install a driver or turn the sandbox off in Agent isolation settings",
		AnchorSandbox}
}

// SandboxDriverMissing is the error of a sandbox that is on with no driver.
func SandboxDriverMissing() error { return refusal([]problem{SandboxProblem()}) }

func (g *Gate) claudeProblem(ctx context.Context) problem {
	missing := problem{CodeClaudeMissing,
		"no managed claude. Agents run a claude that the harness installs and keeps. Install it in Agent isolation settings",
		AnchorClaude}
	if g.Managed == nil {
		return missing
	}
	st := g.Managed.State()
	if st.Installing == nil && st.Failed == nil {
		started, err := g.Managed.Install(ctx, "stable")
		switch {
		case err == nil:
			st = started
		case errs.Code(err) == "MANAGED_CLAUDE_BUSY":
			st = g.Managed.State()
		default:
			missing.text = "no managed claude, and installing it failed: " + err.Error() + ". Install it in Agent isolation settings"
			return missing
		}
	}
	switch {
	case st.Installing != nil:
		return problem{CodeClaudeInstalling,
			"the managed claude is installing" + progress(*st.Installing) + ". Try again when it is done; follow it in Agent isolation settings",
			AnchorClaude}
	case st.Failed != nil:
		return problem{CodeClaudeFailed,
			"installing the managed claude failed: " + st.Failed.Message + ". Retry it in Agent isolation settings",
			AnchorClaude}
	}
	return missing
}

func progress(in ports.ManagedClaudeInstall) string {
	version := in.Version
	if version == "stable" || version == "latest" || version == "" {
		version = ""
	}
	pct := ""
	if in.Total > 0 {
		pct = fmt.Sprintf("%d%%", in.Done*100/in.Total)
	}
	switch {
	case version != "" && pct != "":
		return " (" + version + ", " + pct + ")"
	case version != "":
		return " (" + version + ")"
	case pct != "":
		return " (" + pct + ")"
	}
	return ""
}

// refusal is the one error for every problem, nil for none. Each sentence ends
// with the page's anchor in parentheses, so an agent that only sees the
// message can still tell the person where to go.
func refusal(ps []problem) error {
	if len(ps) == 0 {
		return nil
	}
	var parts []string
	missing := make([]map[string]string, 0, len(ps))
	for _, p := range ps {
		parts = append(parts, "Agent not started: "+p.text+" ("+p.anchor+").")
		missing = append(missing, map[string]string{"code": p.code, "anchor": p.anchor})
	}
	return &domain.DetailedError{
		StructuredError: &domain.StructuredError{Code: ps[0].code, Message: strings.Join(parts, " ")},
		Details:         map[string]any{"missing": missing},
	}
}
