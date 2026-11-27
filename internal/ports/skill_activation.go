package ports

import "context"

// SkillActivator loads the body of a skill whose activation rule matches a
// file a session touched (*orchestration.Service). It answers the text to give
// the agent as context, "" when no skill is due: a skill loads once per
// session.
type SkillActivator interface {
	ActivateSkillsOnTouch(ctx context.Context, sessionID, filePath string) string
}
