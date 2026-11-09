package app

import (
	"net/http"

	"go.uber.org/fx"
	"gorm.io/gorm"

	llminterview "github.com/rfbatista/llmkit/interview"

	"operators-mcp/internal/adapter/in/mcpinterview"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/interviews"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/ports"
)

// InterviewsModule wires the interviews agents ask the person: the service
// over their table. llmkit's Sink asks through it.
var InterviewsModule = fx.Module("interviews",
	fx.Provide(newInterviews),
	fx.Invoke(subscribeInterviews),
)

type interviewsParams struct {
	fx.In

	DB       *gorm.DB
	Sessions ports.SessionRepository
	Agents   ports.AgentRepository
	Cat      catalog.Catalog
	// Orch puts the sessions' open counts on the project feeds; graphs built
	// without the orchestration announce none.
	Orch *orchestration.Service `optional:"true"`
}

func newInterviews(p interviewsParams) *interviews.Service {
	deps := interviews.Deps{Repo: sqlite.NewInterviewRepository(p.DB), Sessions: p.Sessions, Agents: p.Agents, Events: p.Cat.Bus}
	if p.Orch != nil {
		deps.Announce = p.Orch
	}
	return interviews.New(deps)
}

// subscribeInterviews has a session that ends expire its open interviews.
func subscribeInterviews(svc *interviews.Service, cat catalog.Catalog) {
	svc.Subscribe(cat.Bus)
}

// optionalInterviews is the interviews service when InterviewsModule is in the
// graph; graphs built without it (some tests) serve none.
type optionalInterviews struct {
	fx.In

	Service *interviews.Service `optional:"true"`
}

// interviewHandler is llmkit's interview handler over the service: the ask
// tool and the capture of every interactive session, mounted at
// llminterview.PathPrefix.
func interviewHandler(svc *interviews.Service) http.Handler {
	return llminterview.Handler(mcpinterview.Sink{Interviews: svc})
}

// interviewURLs gives a session its interview endpoint on the loopback server
// at base, named by the harness session id.
func interviewURLs(base string) func(sessionID string) string {
	return func(sessionID string) string { return base + llminterview.PathPrefix + sessionID }
}
