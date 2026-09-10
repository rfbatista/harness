package execution

import (
	"context"

	"github.com/firebase/genkit/go/genkit"
)

// RegisterFlows defines Genkit flows so they appear in the Genkit Dev UI.
func RegisterFlows(g *genkit.Genkit, svc *Service) {
	genkit.DefineFlow(g, "runZoneTask", func(ctx context.Context, req RunTaskRequest) (string, error) {
		task, err := svc.RunTask(ctx, req)
		if err != nil {
			return "", err
		}
		if task.Error != "" {
			return "FAILED: " + task.Error, nil
		}
		return task.Result, nil
	})
}
