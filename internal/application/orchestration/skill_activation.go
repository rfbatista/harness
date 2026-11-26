package orchestration

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/domain"
)

// Skill activation (Learn layer): a skill whose SKILL.md metadata carries an
// activation rule loads its body into a session at start, for a matching task
// type, or the first time the agent touches a matching file. The rule is read
// from the repository's stored instruction files, never from the checkout.

// SkillTouchTools are the tools whose use touches a file.
const SkillTouchTools = "Read|Edit|Write|MultiEdit"

const skillActivationHookTimeout = 10

// SkillActivationHooks are the claude settings hooks of a session whose skill
// activation endpoint is at skillsURL: PostToolUse on a touched file posts the
// hook's own input; the endpoint answers in claude's hook output format.
func SkillActivationHooks(skillsURL string) map[claude.HookEvent][]claude.HookMatcher {
	curl := "curl -fsS -m 10 -X POST -H 'Content-Type: application/json'" +
		" --data-binary @- '" + skillsURL + "/activate' 2>/dev/null || true"
	return map[claude.HookEvent][]claude.HookMatcher{
		claude.HookPostToolUse: {{Matcher: SkillTouchTools, Hooks: []claude.Hook{{Type: "command", Command: curl, Timeout: skillActivationHookTimeout}}}},
	}
}

// activatableSkill is a stored skill with an activation rule.
type activatableSkill struct {
	name string
	body string
	rule domain.SkillActivation
}

// activatableSkills are the skills of files that have a valid activation rule.
// A skill with an invalid rule never activates (the drafts flow refuses one).
func activatableSkills(files []domain.InstructionFile) []activatableSkill {
	var out []activatableSkill
	for _, f := range files {
		if f.Kind != "skill" || path.Base(f.Path) != domain.SkillFileName || path.Dir(f.Path) != skillFolder(f.Path) {
			continue
		}
		doc, err := domain.ParseSkillDocument([]byte(f.Content))
		if err != nil {
			continue
		}
		rule, err := domain.ParseSkillActivation(doc.Metadata)
		if err != nil || rule.Empty() {
			continue
		}
		name := doc.Name
		if name == "" {
			name = path.Base(path.Dir(f.Path))
		}
		out = append(out, activatableSkill{name: name, body: doc.Body, rule: rule})
	}
	return out
}

// skillActiveFilePrefix names the memory files that carry the body of a skill
// activated at start.
const skillActiveFilePrefix = "SKILL-"

// activatedSkillFiles are the bodies of the skills whose task type matches the
// session's mode, delivered beside the instruction files as memory files.
func (s *Service) activatedSkillFiles(ctx context.Context, role domain.SessionRole, mode domain.SessionMode, repositoryID string) []llmkit.InstructionFile {
	if s.Architectures == nil || repositoryID == "" || role == domain.RoleArchitect || mode == domain.SessionModeDefault {
		return nil
	}
	files, err := s.Architectures.InstructionFilesOf(ctx, repositoryID)
	if err != nil {
		slog.Warn("list a repository's instruction files for skill activation", "repository", repositoryID, "err", err)
		return nil
	}
	var out []llmkit.InstructionFile
	for _, sk := range activatableSkills(files) {
		if sk.rule.MatchesTaskType(mode) {
			out = append(out, llmkit.InstructionFile{Kind: llmkit.InstructionMemory, Path: skillActiveFilePrefix + sk.name + ".md", Content: []byte(skillActiveText(sk))})
		}
	}
	return out
}

func skillActiveText(sk activatableSkill) string {
	return fmt.Sprintf("# Skill %s (activated)\n\n%s\n", sk.name, sk.body)
}

// activatedOnTouch remembers, per session, the skills already loaded.
type activatedOnTouch struct {
	mu   sync.Mutex
	done map[string]map[string]bool
}

func (a *activatedOnTouch) first(sessionID, skill string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done == nil {
		a.done = map[string]map[string]bool{}
	}
	if a.done[sessionID] == nil {
		a.done[sessionID] = map[string]bool{}
	}
	if a.done[sessionID][skill] {
		return false
	}
	a.done[sessionID][skill] = true
	return true
}

// ActivateSkillsOnTouch implements ports.SkillActivator: the bodies of the
// session's skills whose path pattern matches filePath (absolute, or relative
// to the session's worktree) that have not been loaded in this session yet.
func (s *Service) ActivateSkillsOnTouch(ctx context.Context, sessionID, filePath string) string {
	if s.Architectures == nil || filePath == "" {
		return ""
	}
	sess, err := s.sessions.Find(ctx, sessionID)
	if err != nil || sess == nil || sess.RepositoryID == "" || sess.WorkingDir == "" {
		return ""
	}
	rel := filePath
	if filepath.IsAbs(rel) {
		r, err := filepath.Rel(sess.WorkingDir, rel)
		if err != nil || r == ".." || strings.HasPrefix(r, "../") {
			return ""
		}
		rel = r
	}
	rel = filepath.ToSlash(rel)
	files, err := s.Architectures.InstructionFilesOf(ctx, sess.RepositoryID)
	if err != nil {
		slog.Warn("list a repository's instruction files for skill activation", "repository", sess.RepositoryID, "err", err)
		return ""
	}
	var out []string
	for _, sk := range activatableSkills(files) {
		if sk.rule.MatchesPath(rel) && s.touchActivations.first(sessionID, sk.name) {
			out = append(out, skillActiveText(sk))
		}
	}
	return strings.Join(out, "\n")
}
