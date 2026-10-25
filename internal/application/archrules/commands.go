package archrules

import (
	"context"
	"os"
	"regexp"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Runner runs a rule's command in dir with env under a deadline, and answers
// the first domain.MaxRuleEventDetail bytes of its combined output and whether
// it exited 0. A timeout is a failure.
type Runner func(ctx context.Context, dir string, env []string, command string, timeout time.Duration) (output string, ok bool)

// defaultTools are the tools a before-tool command rule covers when it names
// none.
var defaultTools = []string{"Edit", "Write", "MultiEdit", "Bash"}

// toolKind is the claude tool a check is made for: a PATH shim's command is a
// Bash call.
func toolKind(tool string) string {
	switch tool {
	case "Edit", "Write", "MultiEdit", "NotebookEdit", "Bash":
		return tool
	}
	return "Bash"
}

// commandRules runs the record's command rules for the check's lifecycle
// point: those that name the session's agent, whose tools and paths filters
// match, each in the worktree with the session's environment.
func (s *Service) commandRules(ctx context.Context, w who, c ports.ArchitectureCheck, paths []string, v *verdict) {
	for _, rule := range w.cur.Compiled.Record.Rules {
		if rule.Kind != domain.RuleCommand || rule.At != c.Point || !names(rule.Agents, w.agent) {
			continue
		}
		if c.Point == domain.PointBeforeTool {
			tools := rule.Tools
			if len(tools) == 0 {
				tools = defaultTools
			}
			if !names(tools, toolKind(c.Tool)) || c.Shim && rule.Paths == "" {
				continue
			}
		}
		touched, ok := matching(rule.Paths, paths)
		if !ok {
			continue
		}
		timeout := time.Duration(rule.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = domain.RuleDefaultTimeoutSeconds * time.Second
		}
		output, passed := s.d.Run(ctx, w.sess.WorkingDir, s.env(ctx, w.sess.RepositoryID), rule.Command, timeout)
		if passed {
			continue
		}
		path, layer := "", ""
		if touched != "" {
			_, layer = w.cur.Compiled.AccessOf(w.agent, touched)
			if layer == "" {
				layer = "unlayered"
			}
			path = touched
		}
		text := "Architecture rule " + rule.ID + " (" + rule.Mode + "): " + rule.Message
		if out := strings.TrimRight(output, "\n"); out != "" {
			text += "\n" + out
		}
		if c.Point == domain.PointAfterEdit {
			// The edit has landed: both modes tell the agent, none can deny it.
			if rule.Mode == domain.ModeBlocking {
				text += "\nUndo or fix this change before you go on."
			}
			v.context = append(v.context, text)
		} else {
			v.add(rule.Mode, text)
		}
		s.record(ctx, w, rule, c, path, layer, output)
	}
}

// matching says whether a rule with the paths filter runs for the touched
// paths, and the first touched path it is about. No filter always runs.
func matching(filter string, paths []string) (first string, ok bool) {
	if filter == "" {
		if len(paths) > 0 {
			first = paths[0]
		}
		return first, true
	}
	re, err := regexp.Compile(filter)
	if err != nil {
		return "", false
	}
	for _, p := range paths {
		if re.MatchString(p) {
			return p, true
		}
	}
	return "", false
}

// env is the environment a command runs with: the server's, and the
// repository's saved root .env parsed as dotenv (ADR 0008), which wins.
func (s *Service) env(ctx context.Context, repositoryID string) []string {
	env := os.Environ()
	if s.d.EnvFiles == nil {
		return env
	}
	files, err := s.d.EnvFiles.ListEnvFiles(ctx, repositoryID)
	if err != nil {
		return env
	}
	for _, f := range files {
		if f.Path != domain.DefaultEnvFilePath {
			continue
		}
		vars, _ := domain.ParseDotenv(f.Content)
		for _, v := range vars {
			env = append(env, v.Key+"="+v.Value)
		}
	}
	return env
}
