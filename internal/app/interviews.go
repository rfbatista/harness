package app

import (
	"go.uber.org/fx"
	"gorm.io/gorm"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/interviews"
	"operators-mcp/internal/ports"
)

// InterviewsModule wires the interviews agents ask the person: the service
// over their table. llmkit's Sink asks through it.
var InterviewsModule = fx.Module("interviews",
	fx.Provide(newInterviews),
)

type interviewsParams struct {
	fx.In

	DB       *gorm.DB
	Sessions ports.SessionRepository
	Agents   ports.AgentRepository
}

func newInterviews(p interviewsParams) *interviews.Service {
	return interviews.New(interviews.Deps{Repo: sqlite.NewInterviewRepository(p.DB), Sessions: p.Sessions, Agents: p.Agents})
}

// optionalInterviews is the interviews service when InterviewsModule is in the
// graph; graphs built without it (some tests) serve none.
type optionalInterviews struct {
	fx.In

	Service *interviews.Service `optional:"true"`
}
