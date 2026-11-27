package app

import (
	"context"
	"errors"

	"operators-mcp/internal/application/gateruns"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
)

// taskMemoryCheck is the Learn layer's entry in the done declaration: a gated
// session that left progress.md as it found it is asked once to update it.
// Its refusal never counts toward the gate retry limit.
type taskMemoryCheck struct {
	runs *gateruns.Service
	orch *orchestration.Service
}

func (c taskMemoryCheck) Check(ctx context.Context, sess *domain.Session) (gateruns.CheckResult, error) {
	if _, _, err := c.runs.Gated(ctx, sess.ID); err != nil {
		var se *domain.StructuredError
		if errors.As(err, &se) && se.Code == "SESSION_NOT_GATED" {
			return gateruns.CheckResult{}, nil
		}
		return gateruns.CheckResult{}, err
	}
	if !c.orch.AskToUpdateTaskMemory(ctx, sess) {
		return gateruns.CheckResult{}, nil
	}
	return gateruns.CheckResult{Refused: true, Code: orchestration.TaskMemoryNotUpdatedCode, Message: orchestration.TaskMemoryNotUpdatedMessage}, nil
}

// bindTaskMemoryCheck adds the check to the done declaration.
func bindTaskMemoryCheck(d *gateruns.Declarer, runs *gateruns.Service, orch *orchestration.Service) {
	d.Add(taskMemoryCheck{runs: runs, orch: orch})
}
