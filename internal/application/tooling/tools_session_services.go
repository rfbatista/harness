package tooling

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The services server (see "Contract: Harness server ↔ Agent sessions — the
// services MCP server", ADR 0075): agents list, read, start, stop and restart
// the services of their project. The person defines the services; there is no
// tool to create, edit or delete one.

// SessionServicesToolNames lists the tools SessionServicesTools returns, in
// order. It is the single source of truth for the orchestration's allow-list.
var SessionServicesToolNames = domain.ServicesServerTools

// DefaultServicesSettleTimeout is how long start_service and restart_service
// wait for a run to be running or failed before they answer starting.
const DefaultServicesSettleTimeout = 60 * time.Second

// Limits of read_service_logs.
const (
	defaultLogTailLines = 200
	maxLogTailLines     = 2000
	// maxLogReadBytes is what a read from an offset returns at most.
	maxLogReadBytes = 64 << 10
)

// ServicesOption tunes SessionServicesTools.
type ServicesOption func(*servicesTools)

// WithServicesSettleTimeout sets how long a start or restart waits for the run
// to settle (default DefaultServicesSettleTimeout). A run still starting then
// is answered as it is, with status starting.
func WithServicesSettleTimeout(d time.Duration) ServicesOption {
	return func(t *servicesTools) { t.settle = d }
}

type servicesTools struct {
	sessions ports.SessionRepository
	agents   ports.AgentLister
	services ports.ServiceManager
	repos    ports.RepositoryReader
	settle   time.Duration
}

// serviceView is the shape every services tool returns for a service.
type serviceView struct {
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Status     string   `json:"status"`
	Command    string   `json:"command"`
	Image      string   `json:"image"`
	Repository string   `json:"repository"`
	Ports      []string `json:"ports"`
	EnvNames   []string `json:"env_names"`
	Run        *runView `json:"run"`
}

type runView struct {
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at"`
	ExitCode  *int   `json:"exit_code"`
	Error     string `json:"error"`
	StartedBy string `json:"started_by"`
	Yours     bool   `json:"yours"`
	Where     string `json:"where"`
	Dir       string `json:"dir"`
	Sandboxed bool   `json:"sandboxed"`
}

// SessionServicesTools serves the six tools of the services MCP server. Like
// the task tools they take no project id: the session id travels in the
// context and every handler resolves its project from it, so a session only
// reaches the services of its own project. agents, when set, names the agent
// that starts a run.
func SessionServicesTools(sessions ports.SessionRepository, agents ports.AgentLister, services ports.ServiceManager, repos ports.RepositoryReader, opts ...ServicesOption) []domain.Tool {
	t := &servicesTools{sessions: sessions, agents: agents, services: services, repos: repos, settle: DefaultServicesSettleTimeout}
	for _, o := range opts {
		o(t)
	}
	nameSchema := `"name":{"type":"string","description":"The service's name, as list_services gives it."}`
	worktreeSchema := `"worktree":{"type":"string","enum":["own"],"description":"own: run a command service of your repository in your worktree instead of the repository's main checkout. Omit for the main checkout."}`
	return []domain.Tool{
		{
			Name: "list_services",
			Description: "List the services of this session's project, by name: the programs the person defined to run beside each other " +
				"(a command line or a container image), each with its status and its last run. Environment variables show as names only.",
			InputSchema: emptySchema(),
			Source:      "code",
			Handler: func(ctx context.Context, _ map[string]any) (any, error) {
				sess, err := t.scope(ctx)
				if err != nil {
					return nil, err
				}
				list, err := t.services.ListServices(ctx, sess.ProjectID)
				if err != nil {
					return nil, t.refusal(ctx, err, "")
				}
				views := make([]serviceView, 0, len(list.Services))
				for _, svc := range list.Services {
					views = append(views, t.view(ctx, svc, sess.ID))
				}
				return map[string]any{"count": len(views), "services": views}, nil
			},
		},
		{
			Name:        "service_status",
			Description: "One service of this project with its status and last run: who started it, where it runs, how it ended.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["name"],"properties":{` + nameSchema + `}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, svc, err := t.target(ctx, args)
				if err != nil {
					return nil, err
				}
				return map[string]any{"service": t.view(ctx, svc, sess.ID)}, nil
			},
		},
		{
			Name: "start_service",
			Description: "Start a service and answer once its run is running or has failed (a failed run is a normal answer: status failed, run.error). " +
				"Without worktree a command service runs in its repository's main checkout. worktree: \"own\" runs it in your worktree, " +
				"which only a command service of your own repository can. A service that is already starting or running is returned with " +
				"already_running true, wherever it runs and whoever started it; nothing is restarted. If the run is still starting after " +
				"a minute the answer says starting: call service_status.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["name"],"properties":{` + nameSchema + `,` + worktreeSchema + `}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, svc, err := t.target(ctx, args)
				if err != nil {
					return nil, err
				}
				opts, err := t.startOptions(ctx, sess, svc, args)
				if err != nil {
					return nil, err
				}
				started, already, err := t.services.StartService(ctx, svc.ID, opts)
				if err != nil {
					return nil, t.refusal(ctx, err, svc.Name)
				}
				if !already {
					started = t.settled(ctx, started)
				}
				return map[string]any{"service": t.view(ctx, started, sess.ID), "already_running": already}, nil
			},
		},
		{
			Name:        "stop_service",
			Description: "Stop a service, whoever started it, and answer once its run has ended. A service that is not running is returned unchanged.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["name"],"properties":{` + nameSchema + `}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, svc, err := t.target(ctx, args)
				if err != nil {
					return nil, err
				}
				stopped, err := t.services.StopService(ctx, svc.ID)
				if err != nil {
					return nil, t.refusal(ctx, err, svc.Name)
				}
				return map[string]any{"service": t.view(ctx, stopped, sess.ID)}, nil
			},
		},
		{
			Name: "restart_service",
			Description: "Stop a service's run, if there is one, then start it with the rules of start_service (worktree included), " +
				"and answer once the new run is running or has failed. Without worktree it repeats the last run in the same worktree.",
			InputSchema: schemaFromJSON(`{"type":"object","required":["name"],"properties":{` + nameSchema + `,` + worktreeSchema + `}}`),
			Source:      "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				sess, svc, err := t.target(ctx, args)
				if err != nil {
					return nil, err
				}
				opts, err := t.startOptions(ctx, sess, svc, args)
				if err != nil {
					return nil, err
				}
				// Without a worktree it repeats the last run (ADR 0077).
				if wt, given := args["worktree"]; !given || wt == nil {
					opts.Repeat = true
				}
				restarted, err := t.services.RestartService(ctx, svc.ID, opts)
				if err != nil {
					return nil, t.refusal(ctx, err, svc.Name)
				}
				return map[string]any{"service": t.view(ctx, t.settled(ctx, restarted), sess.ID)}, nil
			},
		},
		{
			Name: "read_service_logs",
			Description: "Read a service's log, the current or last run's, with the server's own [harness] lines. By default the last tail_lines " +
				"lines (1-2000, default 200). With offset, the text from that byte offset instead, at most 64 KB: pass next_offset back to follow " +
				"the log. truncated is true when the answer was cut at that limit, or the offset fell before first_offset (the oldest byte kept).",
			InputSchema: schemaFromJSON(`{"type":"object","required":["name"],"properties":{` + nameSchema + `,` +
				`"tail_lines":{"type":"integer","minimum":1,"maximum":2000,"description":"How many of the last lines. Default: 200."},` +
				`"offset":{"type":"integer","minimum":0,"description":"A byte offset in the log: read from there instead of the end."}}}`),
			Source: "code",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				_, svc, err := t.target(ctx, args)
				if err != nil {
					return nil, err
				}
				return t.readLogs(ctx, svc, args)
			},
		},
	}
}

// scope is the session's project: every tool's first step.
func (t *servicesTools) scope(ctx context.Context) (*domain.Session, error) {
	sess, err := resolveProjectScope(ctx, t.sessions)
	if err != nil {
		return nil, err
	}
	if sess.ProjectID == "" {
		return nil, errNoServicesProject
	}
	return sess, nil
}

var errNoServicesProject = &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "this session has no project"}

// target is the session and the service its arguments name, among its
// project's.
func (t *servicesTools) target(ctx context.Context, args map[string]any) (*domain.Session, *domain.Service, error) {
	sess, err := t.scope(ctx)
	if err != nil {
		return nil, nil, err
	}
	name := strings.TrimSpace(getString(args, "name", ""))
	if name == "" {
		return nil, nil, invalidInput("name is required: a service of this project, as list_services gives it")
	}
	list, err := t.services.ListServices(ctx, sess.ProjectID)
	if err != nil {
		return nil, nil, t.refusal(ctx, err, name)
	}
	for _, svc := range list.Services {
		if svc.Name == name {
			return sess, svc, nil
		}
	}
	return nil, nil, serviceNotFound(name)
}

func serviceNotFound(name string) error {
	return &domain.StructuredError{Code: "SERVICE_NOT_FOUND", Message: `no service named "` + name + `" in this project; call list_services`}
}

// refusal gives a module refusal the contract's message, which says what to do
// next; the MCP bridge passes agents only the message. Other errors pass.
func (t *servicesTools) refusal(ctx context.Context, err error, name string) error {
	var se *domain.StructuredError
	if !errors.As(err, &se) {
		return err
	}
	switch se.Code {
	case "SERVICE_NOT_FOUND":
		return serviceNotFound(name)
	case "PROJECT_NOT_FOUND":
		return errNoServicesProject
	case "SANDBOX_UNAVAILABLE":
		return &domain.StructuredError{Code: se.Code, Message: "the sandbox is on but this platform has no sandbox driver; command services cannot start"}
	case "RUNTIME_UNAVAILABLE":
		return &domain.StructuredError{Code: se.Code, Message: `the container runtime "` + t.services.ContainerRuntime(ctx).Name + `" was not found on the harness server; image services cannot start`}
	}
	return err
}

// startOptions say who starts a run and where: the calling session, in its own
// worktree when the arguments ask for it and the service can run there.
func (t *servicesTools) startOptions(ctx context.Context, sess *domain.Session, svc *domain.Service, args map[string]any) (ports.ServiceStartOptions, error) {
	opts := ports.ServiceStartOptions{StartedBy: domain.ServiceStartedBy{Kind: "session", SessionID: sess.ID, Agent: t.agentName(ctx, sess), TaskID: sess.TicketID}}
	wt, given := args["worktree"]
	if !given || wt == nil {
		return opts, nil
	}
	if wt != "own" {
		return opts, invalidInput(`worktree must be "own" or omitted`)
	}
	if reason := t.noWorktree(ctx, sess, svc); reason != "" {
		return opts, &domain.StructuredError{Code: "WORKTREE_NOT_AVAILABLE", Message: `"` + svc.Name + `" cannot run in your worktree: ` + reason}
	}
	opts.Worktree = &ports.ServiceWorktree{Dir: sess.WorkingDir, Branch: sess.Branch, SessionID: sess.ID}
	return opts, nil
}

// noWorktree is why the service cannot run in the session's worktree, "" when
// it can.
func (t *servicesTools) noWorktree(ctx context.Context, sess *domain.Session, svc *domain.Service) string {
	switch {
	case svc.Kind == domain.ServiceKindImage:
		return "it is an image service"
	case svc.RepositoryID == "":
		return "it has no repository"
	case svc.RepositoryID != sess.RepositoryID:
		return "it runs from repository " + t.repositoryName(ctx, svc.RepositoryID) + ", not yours"
	case sess.WorkspaceID == "" || sess.WorkingDir == "":
		return "this session has no worktree"
	}
	return ""
}

func (t *servicesTools) repositoryName(ctx context.Context, id string) string {
	if id == "" || t.repos == nil {
		return ""
	}
	if repo, err := t.repos.GetRepository(ctx, id); err == nil && repo != nil {
		return repo.Name
	}
	return ""
}

func (t *servicesTools) agentName(ctx context.Context, sess *domain.Session) string {
	if sess.AgentID == "" || t.agents == nil {
		return ""
	}
	if a, err := t.agents.GetAgent(ctx, sess.AgentID); err == nil && a != nil {
		return a.Name
	}
	return ""
}

// settled waits, up to the settle timeout, for a run that is starting to be
// running or failed; one still starting then is answered as it is.
func (t *servicesTools) settled(ctx context.Context, svc *domain.Service) *domain.Service {
	if svc == nil || svc.Status != domain.ServiceStarting {
		return svc
	}
	wctx, cancel := context.WithTimeout(ctx, t.settle)
	defer cancel()
	if got, err := t.services.WaitServiceSettled(wctx, svc.ID); err == nil && got != nil {
		return got
	}
	if got, err := t.services.GetService(ctx, svc.ID); err == nil && got != nil {
		return got
	}
	return svc
}

// view is the service as the contract's ServiceView, as seen by session id.
func (t *servicesTools) view(ctx context.Context, svc *domain.Service, sessionID string) serviceView {
	v := serviceView{
		Name: svc.Name, Kind: string(svc.Kind), Status: string(svc.Status),
		Repository: t.repositoryName(ctx, svc.RepositoryID), Ports: []string{}, EnvNames: []string{},
	}
	if svc.Command != nil {
		v.Command = svc.Command.Line
		for _, p := range svc.Command.Ports {
			v.Ports = append(v.Ports, strconv.Itoa(p))
		}
	}
	if svc.Image != nil {
		v.Image = svc.Image.Ref
		for _, p := range svc.Image.Ports {
			v.Ports = append(v.Ports, strconv.Itoa(p.Host)+"->"+strconv.Itoa(p.Container)+"/"+p.Protocol)
		}
	}
	// Names only: a value never leaves the server.
	vars, _ := domain.ParseDotenv(svc.Env)
	for _, e := range vars {
		v.EnvNames = append(v.EnvNames, e.Key)
	}
	if r := svc.Run; r != nil {
		v.Run = &runView{
			StartedAt: r.StartedAt, EndedAt: r.EndedAt, ExitCode: r.ExitCode, Error: r.Error,
			StartedBy: startedByText(r.StartedBy), Yours: r.StartedBy.Kind == "session" && r.StartedBy.SessionID == sessionID,
			Where: whereText(r.Where, sessionID), Dir: r.Where.Dir, Sandboxed: r.Sandboxed,
		}
	}
	return v
}

func shortSessionID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func startedByText(by domain.ServiceStartedBy) string {
	if by.Kind != "session" {
		return "person"
	}
	text := "session " + shortSessionID(by.SessionID)
	if by.Agent != "" {
		text += " (" + by.Agent + ")"
	}
	return text
}

func whereText(w domain.ServiceWhere, sessionID string) string {
	switch w.Kind {
	case "session":
		if w.SessionID == sessionID {
			return "your worktree"
		}
		return "worktree of session " + shortSessionID(w.SessionID)
	case "service_folder":
		return "service folder"
	case "container":
		return "container"
	}
	return "main checkout"
}

// readLogs is read_service_logs: the last lines, or from a byte offset.
func (t *servicesTools) readLogs(ctx context.Context, svc *domain.Service, args map[string]any) (any, error) {
	lines := defaultLogTailLines
	if n, ok := getFloat(args, "tail_lines"); ok {
		if n < 1 || n > maxLogTailLines || n != float64(int(n)) {
			return nil, invalidInput("tail_lines must be between 1 and 2000")
		}
		lines = int(n)
	}
	var offset *int64
	if n, ok := getFloat(args, "offset"); ok {
		if n < 0 || n != float64(int64(n)) {
			return nil, invalidInput("offset must be a whole number of bytes, 0 or more")
		}
		o := int64(n)
		offset = &o
	}
	if offset == nil {
		read, truncated, err := t.services.ReadServiceLogTail(ctx, svc.ID, lines)
		if err != nil {
			return nil, t.refusal(ctx, err, svc.Name)
		}
		return logAnswer(read.Text, read.NextOffset, read.FirstOffset, truncated), nil
	}
	read, err := t.services.ReadServiceLog(ctx, svc.ID, offset)
	if err != nil {
		return nil, t.refusal(ctx, err, svc.Name)
	}
	text, next := read.Text, read.NextOffset
	// The module reads more than an agent is given at once. The cut falls on a
	// character; a byte that was no character already counts as one byte.
	truncated := read.Offset > *offset
	if len(text) > maxLogReadBytes {
		cut := 0
		for i, r := range text {
			size := utf8.RuneLen(r)
			if r == utf8.RuneError {
				size = 1
			}
			if cut+size > maxLogReadBytes {
				text = text[:i]
				break
			}
			cut += size
		}
		next, truncated = read.Offset+int64(cut), true
	}
	return logAnswer(text, next, read.FirstOffset, truncated), nil
}

func logAnswer(text string, next, first int64, truncated bool) map[string]any {
	return map[string]any{"text": text, "next_offset": next, "first_offset": first, "truncated": truncated}
}
