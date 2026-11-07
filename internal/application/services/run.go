package services

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const (
	// DefaultStopGrace is how long a stop waits after SIGTERM before it kills.
	DefaultStopGrace = 10 * time.Second
	// outputDrain is how long a run's end waits for the last of its output.
	outputDrain = 2 * time.Second
)

// run is the live run of a service; the module keeps one per active service.
// Its fields are guarded by Service.mu.
type run struct {
	id        string
	serviceID string
	handle    ports.ServiceProcessHandle
	// stopping says a stop was asked for: the run ends as stopped.
	stopping bool
	// settled closes when the run left starting; done when its end is recorded.
	settled chan struct{}
	done    chan struct{}
}

// prepared is what a start works out before it creates the run.
type prepared struct {
	where domain.ServiceWhere
	env   []string
	// display is the command line the log names; notes are more [harness]
	// lines (never an env value).
	display string
	notes   []string
	// sandbox confines the run; nil is an unconfined one.
	sandbox *confinement
}

// StartService implements ports.ServiceManager.
func (s *Service) StartService(ctx context.Context, serviceID string, opts ports.ServiceStartOptions) (*domain.Service, bool, error) {
	svc, live, err := s.current(serviceID)
	if err != nil || live {
		return svc, live, err
	}
	runner := s.opts.Runners[svc.Kind]
	if runner == nil {
		return nil, false, &domain.StructuredError{Code: "RUNTIME_UNAVAILABLE", Message: "this harness cannot run " + string(svc.Kind) + " services"}
	}
	p, err := s.prepare(ctx, svc, opts)
	if err != nil {
		return nil, false, err
	}

	s.mu.Lock()
	cur, err := s.store.GetService(serviceID)
	if err != nil {
		s.mu.Unlock()
		return nil, false, err
	}
	if s.runs[serviceID] != nil {
		s.mu.Unlock()
		return cur, true, nil
	}
	r := &run{id: "run-" + newServiceID(), serviceID: serviceID, settled: make(chan struct{}), done: make(chan struct{})}
	cur.Status = domain.ServiceStarting
	cur.Run = &domain.ServiceRun{
		ID: r.id, StartedAt: s.opts.Now().UTC().Format(time.RFC3339), StartedBy: opts.StartedBy, Where: p.where, Sandboxed: p.sandbox != nil,
	}
	cur.UpdatedAt = s.opts.Now().UTC()
	lg := s.logFor(cur)
	// The log is cleared before the run is visible: a follower that sees
	// starting finds the new log.
	resetErr := lg.reset(r.id)
	if resetErr == nil {
		resetErr = s.store.SaveService(cur)
	}
	if resetErr != nil {
		s.mu.Unlock()
		return nil, false, resetErr
	}
	s.runs[serviceID] = r
	s.announceLocked(cur)
	s.mu.Unlock()

	s.launch(ctx, r, cur, runner, p, lg)
	out, err := s.store.GetService(serviceID)
	return out, false, err
}

// current reads the service and says whether it has a run going in this
// instance. The read is under mu, so a run that was created is already
// stored.
func (s *Service) current(serviceID string) (svc *domain.Service, live bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	svc, err = s.store.GetService(serviceID)
	return svc, err == nil && s.runs[serviceID] != nil, err
}

// launch writes the run's [harness] lines and spawns it: running when the
// process is going, failed when it could not be.
func (s *Service) launch(ctx context.Context, r *run, svc *domain.Service, runner ports.ServiceProcessRunner, p *prepared, lg *logFile) {
	lg.append([]byte("[harness] command: " + oneLine(p.display) + "\n[harness] directory: " + p.where.Dir + "\n[harness] " + p.sandbox.logLine() + "\n"))
	for _, n := range p.notes {
		lg.append([]byte("[harness] " + n + "\n"))
	}
	if p.where.Dir == "" {
		s.fail(r, lg, "the repository has no main checkout on this machine")
		return
	}
	if p.where.Kind == "service_folder" {
		if err := os.MkdirAll(p.where.Dir, 0o700); err != nil {
			s.fail(r, lg, "cannot create the service folder: "+oneLine(err.Error()))
			return
		}
	}
	if fi, err := os.Stat(p.where.Dir); err != nil || !fi.IsDir() {
		s.fail(r, lg, "working directory "+p.where.Dir+" does not exist")
		return
	}
	spec := ports.ServiceProcessSpec{Name: svc.ID, Path: "/bin/sh", Args: []string{"-c", svc.Command.Line}, Dir: p.where.Dir, Env: p.env}
	if p.sandbox != nil {
		var err error
		if spec, err = p.sandbox.wrap(spec); err != nil {
			s.fail(r, lg, "could not apply the sandbox: "+oneLine(err.Error()))
			return
		}
	}
	h, err := runner.Start(ctx, spec)
	if err != nil {
		s.fail(r, lg, "could not start: "+oneLine(err.Error()))
		return
	}
	s.mu.Lock()
	r.handle = h
	stopNow := r.stopping
	s.moveLocked(svc.ID, domain.ServiceRunning, nil)
	close(r.settled)
	s.mu.Unlock()
	go s.supervise(r, h, lg)
	if stopNow {
		go h.Stop(s.opts.StopGrace)
	}
}

// fail ends a run that never got going.
func (s *Service) fail(r *run, lg *logFile, msg string) {
	lg.append([]byte("[harness] failed: " + msg + "\n"))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.moveLocked(r.serviceID, domain.ServiceFailed, func(svc *domain.Service) {
		svc.Run.Error = msg
		svc.Run.EndedAt = s.opts.Now().UTC().Format(time.RFC3339)
	})
	delete(s.runs, r.serviceID)
	close(r.settled)
	close(r.done)
}

// supervise follows the process to its end and records how it ended.
func (s *Service) supervise(r *run, h ports.ServiceProcessHandle, lg *logFile) {
	out := h.Output()
	pumped := make(chan struct{})
	go func() {
		lg.pump(out)
		close(pumped)
	}()
	code := h.Wait()
	select {
	case <-pumped:
	case <-time.After(outputDrain):
	}
	_ = out.Close()
	<-pumped

	s.mu.Lock()
	stopped := r.stopping
	s.mu.Unlock()
	line := "exited with code " + strconv.Itoa(code)
	if stopped {
		line = "stopped"
	}
	lg.append([]byte("[harness] " + line + "\n"))

	s.mu.Lock()
	defer s.mu.Unlock()
	next := domain.ServiceExited
	if stopped {
		next = domain.ServiceStopped
	}
	s.moveLocked(r.serviceID, next, func(svc *domain.Service) {
		if !stopped {
			svc.Run.ExitCode = &code
		}
		svc.Run.EndedAt = s.opts.Now().UTC().Format(time.RFC3339)
	})
	delete(s.runs, r.serviceID)
	close(r.done)
}

// moveLocked moves the stored service to next, after fn changed it, and
// publishes it. A move the status machine does not allow is not made. The
// caller holds mu.
func (s *Service) moveLocked(serviceID string, next domain.ServiceStatus, fn func(*domain.Service)) {
	svc, err := s.store.GetService(serviceID)
	if err != nil || svc.Run == nil || !svc.Status.CanMoveTo(next) {
		return
	}
	svc.Status = next
	if fn != nil {
		fn(svc)
	}
	svc.UpdatedAt = s.opts.Now().UTC()
	if err := s.store.SaveService(svc); err != nil {
		return
	}
	s.announceLocked(svc)
}

// StopService implements ports.ServiceManager.
func (s *Service) StopService(ctx context.Context, serviceID string) (*domain.Service, error) {
	svc, err := s.store.GetService(serviceID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	r := s.runs[serviceID]
	if r == nil {
		s.mu.Unlock()
		return svc, nil
	}
	r.stopping = true
	h := r.handle
	s.mu.Unlock()
	if h != nil {
		h.Stop(s.opts.StopGrace)
	}
	select {
	case <-r.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return s.store.GetService(serviceID)
}

// RestartService implements ports.ServiceManager.
func (s *Service) RestartService(ctx context.Context, serviceID string, opts ports.ServiceStartOptions) (*domain.Service, error) {
	// A refused restart leaves the running service as it is.
	if svc, err := s.store.GetService(serviceID); err == nil {
		if err := s.checkSandbox(svc); err != nil {
			return nil, err
		}
	}
	if _, err := s.StopService(ctx, serviceID); err != nil {
		return nil, err
	}
	svc, _, err := s.StartService(ctx, serviceID, opts)
	return svc, err
}

// WaitServiceSettled implements ports.ServiceManager.
func (s *Service) WaitServiceSettled(ctx context.Context, serviceID string) (*domain.Service, error) {
	s.mu.Lock()
	r := s.runs[serviceID]
	s.mu.Unlock()
	if r != nil {
		select {
		case <-r.settled:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.store.GetService(serviceID)
}

// prepare works out where a run goes and with what environment. The caller
// has not created the run: a refusal here starts nothing.
func (s *Service) prepare(ctx context.Context, svc *domain.Service, opts ports.ServiceStartOptions) (*prepared, error) {
	p := &prepared{}
	if svc.Command != nil {
		p.display = svc.Command.Line
	}
	repo, err := s.repository(ctx, svc)
	if err != nil {
		return nil, err
	}
	switch {
	case repo == nil:
		p.where = domain.ServiceWhere{Kind: "service_folder", Dir: filepath.Join(s.folderRoot(), svc.ID)}
	case opts.Worktree != nil:
		p.where = domain.ServiceWhere{Kind: "session", Dir: opts.Worktree.Dir, Branch: opts.Worktree.Branch, SessionID: opts.Worktree.SessionID}
	default:
		p.where = domain.ServiceWhere{Kind: "main", Dir: repo.RootDir}
		if s.opts.Branches != nil && repo.RootDir != "" {
			p.where.Branch, _ = s.opts.Branches.CurrentBranch(ctx, repo.RootDir)
		}
	}
	if repo != nil && p.where.Dir != "" && svc.Command != nil && svc.Command.Subdir != "" {
		p.where.Dir = filepath.Join(p.where.Dir, svc.Command.Subdir)
	}
	p.env, p.notes, err = s.runEnv(ctx, svc)
	if err != nil {
		return nil, err
	}
	if p.sandbox, err = s.confine(svc, p.where); err != nil {
		return nil, err
	}
	return p, nil
}

// repository is the service's repository, nil for none.
func (s *Service) repository(ctx context.Context, svc *domain.Service) (*domain.Repository, error) {
	if svc.RepositoryID == "" {
		return nil, nil
	}
	repo, err := s.repos.GetRepository(ctx, svc.RepositoryID)
	if err != nil || repo == nil {
		return nil, &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository " + svc.RepositoryID + " is not in this project"}
	}
	return repo, nil
}

// folderRoot is where the harness-owned folders of services with no
// repository are made, one per service.
func (s *Service) folderRoot() string {
	if s.opts.FolderDir != "" {
		return s.opts.FolderDir
	}
	return filepath.Join(s.opts.LogDir, "folders")
}

// runEnv builds the run's environment, lowest precedence first: the clean
// base, the repository's saved root .env when the service exports it
// (parsed as dotenv, never sourced, never written into a checkout: ADR
// 0008, ADR 0039), then the service's own env. The notes name skipped lines
// by number only.
func (s *Service) runEnv(ctx context.Context, svc *domain.Service) (env, notes []string, err error) {
	var keys []string
	vals := map[string]string{}
	set := func(k, v string) {
		if _, ok := vals[k]; !ok {
			keys = append(keys, k)
		}
		vals[k] = v
	}
	for _, kv := range s.opts.BaseEnv() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			set(k, v)
		}
	}
	if svc.ExportRepositoryEnv && svc.RepositoryID != "" && s.opts.EnvFiles != nil {
		files, err := s.opts.EnvFiles.ListEnvFiles(ctx, svc.RepositoryID)
		if err != nil {
			return nil, nil, err
		}
		for _, f := range files {
			if f.Path != domain.DefaultEnvFilePath {
				continue
			}
			vars, skipped := domain.ParseDotenv(f.Content)
			for _, v := range vars {
				set(v.Key, v.Value)
			}
			if len(skipped) > 0 {
				notes = append(notes, ".env: skipped "+lineNumbers(skipped)+" (not KEY=VALUE)")
			}
		}
	}
	vars, _ := domain.ParseDotenv(svc.Env)
	for _, v := range vars {
		set(v.Key, v.Value)
	}
	env = make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+vals[k])
	}
	return env, notes, nil
}

func lineNumbers(lines []int) string {
	nums := make([]string, len(lines))
	for i, n := range lines {
		nums[i] = strconv.Itoa(n)
	}
	if len(lines) == 1 {
		return "line " + nums[0]
	}
	return "lines " + strings.Join(nums, ", ")
}

// oneLine makes s fit on a line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// baseEnvNames are the variables of the server's own environment a run gets:
// what a toolchain needs, none of the harness's secrets.
var baseEnvNames = []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "LC_CTYPE", "TMPDIR", "DEVELOPER_DIR"}

// defaultBaseEnv is the clean minimal environment: baseEnvNames from the
// server's environment, with the harness's shim directories off PATH.
func defaultBaseEnv() []string {
	var env []string
	for _, k := range baseEnvNames {
		v, ok := os.LookupEnv(k)
		if !ok {
			continue
		}
		if k == "PATH" {
			v = withoutShimDirs(v)
		}
		env = append(env, k+"="+v)
	}
	return env
}

// withoutShimDirs drops the directories the harness made for its shims from
// a PATH.
func withoutShimDirs(path string) string {
	var keep []string
	for _, dir := range filepath.SplitList(path) {
		base := filepath.Base(dir)
		if strings.HasPrefix(base, "harness-shims-") || strings.HasPrefix(base, "harness-keychain-shim-") {
			continue
		}
		keep = append(keep, dir)
	}
	return strings.Join(keep, string(os.PathListSeparator))
}
