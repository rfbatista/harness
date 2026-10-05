// Package wire composes tui-client with fx. The HTTP adapters are bound to
// the same driving ports the server binds its services to; the rest of the
// client only ever sees those ports.
//
// Every long-lived piece owns its start and stop through lifecycle hooks, so
// order lives in the graph: fx starts modules in the order given and stops
// them in reverse — the terminal is restored before panes are closed and
// their sessions ended.
package wire

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/rfbatista/harnesskit/errs"
	"go.uber.org/fx"

	"operators-mcp/internal/adapter/out/httpclient"
	"operators-mcp/internal/app/runtime"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/tuiclient/app"
	"operators-mcp/internal/tuiclient/nav"
	"operators-mcp/internal/tuiclient/ui"
)

// Config is what the command line and environment decide.
type Config struct {
	// API is the coding_pool server's base URL.
	API string
	// Token is the server's bearer token; empty sends none.
	Token string
	// Claude is the claude binary sessions run; empty lets the shell find
	// "claude".
	Claude string
	// Shell is how sessions are started: "direct" or "login" (see
	// runtime.Config).
	Shell string
	// RunsOn is where sessions this client starts run: domain.RunnerServer,
	// on the server, outliving the client; or domain.RunnerTUI, in this
	// process. Claude and Shell only matter for RunnerTUI.
	RunsOn domain.Runner
	// RunnerHost names this machine on its RunnerTUI sessions.
	RunnerHost string
}

// Client connects to the server and binds the HTTP adapters to the ports.
// Startup fails unless the server answers.
var Client = fx.Module("client",
	fx.Provide(
		newClient,
		fx.Annotate(httpclient.NewProjects, fx.As(new(ports.ProjectReader)), fx.As(new(ports.RepositoryCatalog))),
		fx.Annotate(httpclient.NewPlanning, fx.As(new(ports.TicketBoard))),
		fx.Annotate(httpclient.NewAgents, fx.As(new(ports.AgentCatalog))),
		fx.Annotate(httpclient.NewSessions, fx.As(new(ports.SessionReader)), fx.As(new(ports.InteractiveSessions))),
		fx.Annotate(httpclient.NewTerminals, fx.As(new(ports.TerminalAccess))),
		fx.Annotate(httpclient.NewEvents, fx.As(new(ports.SessionFeed))),
	),
)

func newClient(lc fx.Lifecycle, cfg Config) *httpclient.Client {
	c := httpclient.New(cfg.API, httpclient.WithToken(cfg.Token))
	lc.Append(fx.StartHook(func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := c.Health(ctx); err != nil {
			return fmt.Errorf("%w\nstart the server first: make air", err)
		}
		if err := c.CheckAccess(ctx); err != nil {
			if errs.Code(err) == "UNAUTHORIZED" {
				return fmt.Errorf("the server at %s wants a token: pass --token, or set CODING_POOL_TOKEN (make tui-client TOKEN=…)", cfg.API)
			}
			return err
		}
		return nil
	}))
	return c
}

// Orphans stops this machine's RunnerTUI sessions still recorded as running
// when the client starts. They ran in a tui-client that is gone — it crashed
// or was killed before it could end them — since this one has not started
// any yet. That holds as long as one tui-client runs per machine. They stay
// resumable; RunnerServer sessions are the server's, and are left alone.
var Orphans = fx.Module("orphans", fx.Invoke(registerOrphans))

// OrphansStopped is how many sessions Orphans stopped, for the host to report.
type OrphansStopped struct{ N int }

func registerOrphans(lc fx.Lifecycle, cfg Config, sessions ports.SessionReader, interactive ports.InteractiveSessions, stopped *OrphansStopped) {
	lc.Append(fx.StartHook(func(ctx context.Context) error {
		running, err := sessions.List(ctx, ports.SessionFilter{Statuses: []domain.SessionStatus{domain.SessionRunning}})
		if err != nil {
			return err
		}
		for _, s := range running {
			// RunnerHost is empty on sessions recorded before it existed.
			mine := s.RunnerHost == "" || s.RunnerHost == cfg.RunnerHost
			if !s.Interactive || s.RunsOn != domain.RunnerTUI || !mine {
				continue
			}
			if _, err := interactive.EndInteractive(ctx, s.ID, 0, true); err == nil {
				stopped.N++
			}
		}
		return nil
	}))
}

// Program runs the Bubble Tea program over the root model. When the user
// quits, it shuts the app down; when the app stops, it quits the program,
// then closes every pane and ends its session.
var Program = fx.Module("program",
	fx.Provide(newModel),
	fx.Invoke(registerProgram),
)

type deps struct {
	fx.In

	Projects     ports.ProjectReader
	Repositories ports.RepositoryCatalog
	Board        ports.TicketBoard
	Agents       ports.AgentCatalog
	Sessions     ports.SessionReader
	Interactive  ports.InteractiveSessions
	Terminals    ports.TerminalAccess
	Feed         ports.SessionFeed
	// Host is only there when this client runs sessions (RunnerTUI).
	Host ports.TerminalHost `optional:"true"`
}

func newModel(cfg Config, d deps) app.Model {
	return app.New(app.Deps{
		Projects:     d.Projects,
		Repositories: d.Repositories,
		Board:        d.Board,
		Agents:       d.Agents,
		Sessions:     d.Sessions,
		Interactive:  d.Interactive,
		Terminals:    d.Terminals,
		Feed:         d.Feed,
		RunsOn:       cfg.RunsOn,
		Host:         d.Host,
		RunnerHost:   cfg.RunnerHost,
	})
}

// ProgramOptions are extra Bubble Tea options; tests supply input and output.
type ProgramOptions struct {
	fx.In
	Options []tea.ProgramOption `group:"tea_options"`
}

func registerProgram(lc fx.Lifecycle, sd fx.Shutdowner, m app.Model, po ProgramOptions, stopped *OrphansStopped) {
	p := tea.NewProgram(m, po.Options...)
	done := make(chan struct{})
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				defer close(done)
				_, err := p.Run()
				code := 0
				if err != nil && !errors.Is(err, tea.ErrProgramKilled) {
					code = 1
				}
				_ = sd.Shutdown(fx.ExitCode(code))
			}()
			if n := stopped.N; n > 0 {
				// Orphans ran before this hook; say so once the program
				// reads messages.
				go p.Send(nav.StatusMsg{Text: fmt.Sprintf("marked %d session%s from a previous run as stopped", n, ui.Plural(n))})
			}
			return nil
		},
		OnStop: func(ctx context.Context) error {
			p.Quit()
			select {
			case <-done:
			case <-ctx.Done():
				p.Kill()
			}
			// The registry is shared by every copy of the model, so the one
			// built here closes the panes the final one holds.
			m.Shutdown(ctx)
			return nil
		},
	})
}

// Options is the whole client, given its configuration. With RunnerTUI it
// includes the runtime stack, and sessions run in this process and end with
// it; with RunnerServer the server runs them and this process only attaches.
func Options(cfg Config) fx.Option {
	if cfg.RunsOn == "" {
		cfg.RunsOn = domain.RunnerServer
	}
	opts := []fx.Option{fx.Supply(cfg, &OrphansStopped{}), Client, Orphans}
	if cfg.RunsOn == domain.RunnerTUI {
		opts = append(opts,
			fx.Supply(runtime.Config{ClaudeBin: cfg.Claude, Shell: cfg.Shell}),
			runtime.Module,
		)
	}
	return fx.Options(append(opts, Program)...)
}
