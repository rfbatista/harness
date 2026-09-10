package orchestration

import (
	"github.com/rfbatista/harnesskit/skill"

	"operators-mcp/internal/domain"
)

// sessionPluginName labels the throwaway plugin manifest written for a session.
const sessionPluginName = "session-skills"

// resolveSkillDirs materializes an agent's skills into a single Claude
// --plugin-dir tree, returning the dirs to pass to the CLI and a cleanup to run
// when the session ends.
//
// It returns a nil slice, not []string{""}, when there is nothing to write:
// an empty --plugin-dir would be passed to the CLI verbatim.
func resolveSkillDirs(skills []domain.Skill) ([]string, func(), error) {
	dir, cleanup, err := skill.MaterializePluginDir(skills, sessionPluginName)
	if err != nil {
		return nil, cleanup, err
	}
	if dir == "" {
		return nil, cleanup, nil
	}
	return []string{dir}, cleanup, nil
}
