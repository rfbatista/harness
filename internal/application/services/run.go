package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

// overrideSet is the run overrides a start applies and who set them.
type overrideSet struct {
	values map[string]string
	by     domain.ServiceStartedBy
}

// maxOverrides is how many run overrides a run takes.
const maxOverrides = 100

// checkOverrides refuses a bad run override, naming its key and never
// quoting a value. An image service's values also go in a line of its env
// file, so they hold no line break.
func checkOverrides(kind domain.ServiceKind, env map[string]string) error {
	bad := func(format string, args ...any) error {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: fmt.Sprintf(format, args...)}
	}
	if len(env) > maxOverrides {
		return bad("env: at most %d variables, got %d", maxOverrides, len(env))
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := env[k]
		switch {
		case !domain.ValidEnvKey(k):
			return bad("env: %q is not a variable name: use letters, digits and _, not starting with a digit", k)
		case strings.ContainsRune(v, 0):
			return bad("env: the value of %q has a NUL character", k)
		case kind == domain.ServiceKindImage && strings.ContainsAny(v, "\n\r"):
			return bad("env: the value of %q has a line break, which an image service cannot take", k)
		}
	}
	return nil
}

// prepared is what a start works out before it creates the run.
type prepared struct {
	where domain.ServiceWhere
	// worktreeKey is the run worktree, "" when the run has none.
	worktreeKey string
	// base and subdir split where.Dir into the run worktree and the
	// subfolder below it; subdir is "" for none.
	base, subdir string
	env          []string
	// display is the command line the log names; notes are more [harness]
	// lines (never an env value).
	display string
	notes   []string
	// sandbox confines the run; nil is an unconfined one.
	sandbox *confinement
	// image is set for an image service's run.
	image *imagePlan
	// branch is set for a run on a branch of the repository (ADR 0080).
	branch *branchPlan
	// vars are the variables the run is given, for its record.
	vars []ports.ServiceRunVariable
}

// branchPlan is a run on a branch, in the service worktree: where it is, and
// the ref to move it to.
type branchPlan struct {
	name   string
	remote bool
	// repoID and repoRoot name the repository the service worktree belongs to.
	repoID, repoRoot string
	// dir is the service worktree.
	dir string
}

// branchKeyPrefix starts the run worktree key of a branch run.
const branchKeyPrefix = "branch:"

// StartService implements ports.ServiceManager.
func (s *Service) StartService(ctx context.Context, serviceID string, opts ports.ServiceStartOptions) (*domain.Service, bool, error) {
	var ov overrideSet
	if opts.Env != nil {
		ov = overrideSet{values: opts.Env, by: opts.StartedBy}
	}
	return s.start(ctx, serviceID, opts, ov)
}

// start is StartService with the overrides settled: a restart that repeats
// the last run passes the last run's.
func (s *Service) start(ctx context.Context, serviceID string, opts ports.ServiceStartOptions, ov overrideSet) (*domain.Service, bool, error) {
	svc, live, err := s.current(serviceID)
	if err != nil || live {
		return svc, live, err
	}
	if err := checkOverrides(svc.Kind, ov.values); err != nil {
		return nil, false, err
	}
	runner := s.opts.Runners[svc.Kind]
	if runner == nil {
		return nil, false, &domain.StructuredError{Code: "RUNTIME_UNAVAILABLE", Message: "this harness cannot run " + string(svc.Kind) + " services"}
	}
	p, err := s.prepare(ctx, svc, opts, ov.values)
	if err != nil {
		return nil, false, err
	}
	if len(ov.values) > 0 {
		for i := range p.vars {
			if p.vars[i].Source == ports.VariableFromOverride {
				p.vars[i].By = overrideByText(ov.by)
			}
		}
		// Names only: a value is never in the log.
		p.notes = append(p.notes, "run overrides: "+strings.Join(sortedKeys(ov.values), ", "))
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
	if s.closed {
		s.mu.Unlock()
		return nil, false, &domain.StructuredError{Code: "UNAVAILABLE", Message: "the harness is shutting down: no service starts now"}
	}
	r := &run{id: "run-" + newServiceID(), serviceID: serviceID, settled: make(chan struct{}), done: make(chan struct{})}
	cur.Status = domain.ServiceStarting
	cur.Run = &domain.ServiceRun{
		ID: r.id, StartedAt: s.opts.Now().UTC().Format(time.RFC3339), StartedBy: opts.StartedBy, Where: p.where, WorktreeKey: p.worktreeKey,
		Sandboxed: p.sandbox != nil || p.image != nil, Overrides: []domain.ServiceRunOverride{},
	}
	rec := &ports.ServiceRunRecord{ServiceID: serviceID, RunID: r.id, Variables: p.vars}
	if len(ov.values) > 0 {
		by := ov.by
		cur.Run.OverrideBy = &by
		rec.Overrides, rec.OverrideBy = ov.values, &by
		for _, name := range sortedKeys(ov.values) {
			cur.Run.Overrides = append(cur.Run.Overrides, domain.ServiceRunOverride{Name: name, By: overrideByText(by)})
		}
	}
	cur.UpdatedAt = s.opts.Now().UTC()
	lg := s.logFor(cur)
	// The log is cleared before the run is visible: a follower that sees
	// starting finds the new log.
	resetErr := lg.reset(r.id)
	if resetErr == nil {
		resetErr = s.store.SaveRunRecord(rec)
	}
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

	if p.image != nil {
		s.launchImage(ctx, r, cur, runner, p, lg)
	} else {
		s.launch(ctx, r, cur, runner, p, lg)
	}
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
	if p.branch != nil {
		if err := s.checkoutBranch(ctx, r, p, lg); err != nil {
			s.fail(r, lg, oneLine(err.Error()))
			return
		}
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
		if p.subdir != "" && dirExists(p.base) {
			s.fail(r, lg, "subfolder "+strconv.Quote(p.subdir)+" does not exist in "+p.base)
			return
		}
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
	var ov overrideSet
	if opts.Env != nil {
		ov = overrideSet{values: opts.Env, by: opts.StartedBy}
	}
	// A refused restart leaves the running service as it is.
	if svc, err := s.store.GetService(serviceID); err == nil {
		if err := s.checkSandbox(svc); err != nil {
			return nil, err
		}
		if err := s.checkRuntime(svc); err != nil {
			return nil, err
		}
		if opts.Repeat {
			opts.Repeat = false
			if svc.Run != nil {
				opts.WorktreeKey, opts.Worktree = svc.Run.WorktreeKey, nil
			}
		}
		if opts.Env == nil {
			ov = s.lastOverrides(serviceID)
		}
		if err := checkOverrides(svc.Kind, ov.values); err != nil {
			return nil, err
		}
		repo, err := s.repository(ctx, svc)
		if err != nil {
			return nil, err
		}
		if _, err := s.pickWorktree(ctx, svc, repo, opts); err != nil {
			return nil, err
		}
	}
	if _, err := s.StopService(ctx, serviceID); err != nil {
		return nil, err
	}
	svc, _, err := s.start(ctx, serviceID, opts, ov)
	return svc, err
}

// lastOverrides are the run overrides of the service's last run, with who
// set them; none when that run is not the one on record (after a restart of
// the harness).
func (s *Service) lastOverrides(serviceID string) overrideSet {
	svc, err := s.store.GetService(serviceID)
	if err != nil || svc.Run == nil {
		return overrideSet{}
	}
	rec, err := s.store.GetRunRecord(serviceID)
	if err != nil || rec == nil || rec.RunID != svc.Run.ID || rec.OverrideBy == nil {
		return overrideSet{}
	}
	return overrideSet{values: maps.Clone(rec.Overrides), by: *rec.OverrideBy}
}

// GetRunVariables implements ports.ServiceManager.
func (s *Service) GetRunVariables(_ context.Context, serviceID string) (*ports.ServiceRunVariables, error) {
	svc, err := s.store.GetService(serviceID)
	if err != nil {
		return nil, err
	}
	out := &ports.ServiceRunVariables{Variables: []ports.ServiceRunVariable{}}
	if svc.Run == nil {
		return out, nil
	}
	out.RunID = svc.Run.ID
	rec, err := s.store.GetRunRecord(serviceID)
	if err != nil {
		return nil, err
	}
	if rec != nil && rec.RunID == svc.Run.ID {
		out.Variables = append(out.Variables, rec.Variables...)
		out.Overrides, out.OverrideBy = maps.Clone(rec.Overrides), rec.OverrideBy
	}
	return out, nil
}

// overrideByText is who set run overrides as the person reads it.
func overrideByText(by domain.ServiceStartedBy) string {
	short := "session " + shortID(by.SessionID)
	if by.Agent == "" {
		return short
	}
	return by.Agent + " · " + short
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
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
func (s *Service) prepare(ctx context.Context, svc *domain.Service, opts ports.ServiceStartOptions, overrides map[string]string) (*prepared, error) {
	if svc.Kind == domain.ServiceKindImage {
		if _, err := s.pickWorktree(ctx, svc, nil, opts); err != nil {
			return nil, err
		}
		return s.prepareImage(ctx, svc, overrides)
	}
	p := &prepared{}
	if svc.Command != nil {
		p.display = svc.Command.Line
	}
	repo, err := s.repository(ctx, svc)
	if err != nil {
		return nil, err
	}
	pick, err := s.pickWorktree(ctx, svc, repo, opts)
	if err != nil {
		return nil, err
	}
	switch {
	case repo == nil:
		p.where = domain.ServiceWhere{Kind: "service_folder", Dir: filepath.Join(s.folderRoot(), svc.ID)}
	case pick.Kind == domain.RunWorktreeBranch:
		name, remote := branchOfKey(pick.Key)
		p.where = domain.ServiceWhere{Kind: "branch", Dir: pick.Dir, Branch: name, Remote: remote}
		p.worktreeKey = pick.Key
		p.branch = &branchPlan{name: name, remote: remote, repoID: repo.ID, repoRoot: repo.RootDir, dir: pick.Dir}
	case pick.Kind == domain.RunWorktreeSession:
		p.where = domain.ServiceWhere{Kind: "session", Dir: pick.Dir, Branch: pick.Branch, SessionID: pick.SessionID}
		p.worktreeKey = pick.Key
	default:
		p.where = domain.ServiceWhere{Kind: "main", Dir: pick.Dir, Branch: pick.Branch}
		p.worktreeKey = pick.Key
	}
	if repo != nil && p.where.Dir != "" && svc.Command != nil && svc.Command.Subdir != "" {
		p.base, p.subdir = p.where.Dir, svc.Command.Subdir
		p.where.Dir = filepath.Join(p.where.Dir, svc.Command.Subdir)
	}
	p.env, p.notes, p.vars, err = s.layeredEnv(ctx, svc, s.opts.BaseEnv(), overrides)
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

// layeredEnv builds the run's environment over the given base, lowest
// precedence first: the base, the repository's saved root .env when the
// service exports it (parsed as dotenv, never sourced, never written into a
// checkout: ADR 0008, ADR 0039), the service's own env, then the run
// overrides (ADR 0077). The notes name skipped lines by number only. vars are
// the variables the run was given beyond the base, each with the source that
// won.
func (s *Service) layeredEnv(ctx context.Context, svc *domain.Service, base []string, overrides map[string]string) (env, notes []string, vars []ports.ServiceRunVariable, err error) {
	var keys []string
	vals := map[string]string{}
	sources := map[string]string{}
	set := func(k, v, source string) {
		if _, ok := vals[k]; !ok {
			keys = append(keys, k)
		}
		vals[k], sources[k] = v, source
	}
	for _, kv := range base {
		if k, v, ok := strings.Cut(kv, "="); ok {
			set(k, v, "")
		}
	}
	if svc.ExportRepositoryEnv && svc.RepositoryID != "" && s.opts.EnvFiles != nil {
		files, err := s.opts.EnvFiles.ListEnvFiles(ctx, svc.RepositoryID)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, f := range files {
			if f.Path != domain.DefaultEnvFilePath {
				continue
			}
			parsed, skipped := domain.ParseDotenv(f.Content)
			for _, v := range parsed {
				set(v.Key, v.Value, ports.VariableFromRepositoryEnv)
			}
			if len(skipped) > 0 {
				notes = append(notes, ".env: skipped "+lineNumbers(skipped)+" (not KEY=VALUE)")
			}
		}
	}
	own, _ := domain.ParseDotenv(svc.Env)
	for _, v := range own {
		set(v.Key, v.Value, ports.VariableFromService)
	}
	for _, k := range sortedKeys(overrides) {
		set(k, overrides[k], ports.VariableFromOverride)
	}
	env = make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+vals[k])
		if sources[k] != "" {
			vars = append(vars, ports.ServiceRunVariable{Name: k, Value: vals[k], Source: sources[k]})
		}
	}
	sort.Slice(vars, func(i, j int) bool { return vars[i].Name < vars[j].Name })
	return env, notes, vars, nil
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

func dirExists(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

// pickWorktree resolves the run worktree a start asked for. The key is
// looked up among the worktrees on offer now: nothing in a request is
// trusted as a path. A service with no repository or an image service has
// none, and refuses a key.
func (s *Service) pickWorktree(ctx context.Context, svc *domain.Service, repo *domain.Repository, opts ports.ServiceStartOptions) (*domain.RunWorktree, error) {
	key := strings.TrimSpace(opts.WorktreeKey)
	if opts.Worktree == nil && key != "" && (svc.Kind != domain.ServiceKindCommand || repo == nil) {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "worktree: only a command service of a repository runs in a worktree"}
	}
	if repo == nil {
		return nil, nil
	}
	if w := opts.Worktree; w != nil {
		// Trusted: the agent's own worktree, checked by its tool.
		return &domain.RunWorktree{Key: "session:" + w.SessionID, Kind: domain.RunWorktreeSession, Branch: w.Branch, Dir: w.Dir, SessionID: w.SessionID}, nil
	}
	if strings.HasPrefix(key, branchKeyPrefix) {
		return s.pickBranch(ctx, svc, repo, key)
	}
	if key == "" || key == "main" {
		main := &domain.RunWorktree{Key: "main", Kind: domain.RunWorktreeMain, Dir: repo.RootDir}
		if s.opts.Branches != nil && repo.RootDir != "" {
			main.Branch, _ = s.opts.Branches.CurrentBranch(ctx, repo.RootDir)
		}
		return main, nil
	}
	all, err := s.runWorktrees(ctx, svc, repo)
	if err != nil {
		return nil, err
	}
	for _, w := range all {
		if w.Key == key {
			return w, nil
		}
	}
	return nil, &domain.StructuredError{Code: "WORKTREE_NOT_AVAILABLE", Message: "that worktree is no longer available"}
}

// pickBranch resolves a branch key against the branches listed now: nothing in
// a request is trusted as a ref. The service worktree it names is the
// service's own, made at the first run.
func (s *Service) pickBranch(ctx context.Context, svc *domain.Service, repo *domain.Repository, key string) (*domain.RunWorktree, error) {
	name, remote := branchOfKey(key)
	gone := &domain.StructuredError{Code: "WORKTREE_NOT_AVAILABLE", Message: "branch " + strconv.Quote(name) + " does not exist in " + repo.Name}
	if s.opts.BranchGit == nil || repo.RootDir == "" || !dirExists(repo.RootDir) || name == "" {
		return nil, gone
	}
	all, err := s.opts.BranchGit.ListServiceBranches(ctx, repo.RootDir)
	if err != nil {
		return nil, err
	}
	for _, b := range all {
		if b.Name == name && b.Remote == remote {
			return &domain.RunWorktree{Key: key, Kind: domain.RunWorktreeBranch, Branch: name, Dir: s.serviceWorktreeDir(svc.ID)}, nil
		}
	}
	return nil, gone
}

// branchOfKey splits a branch key: branch:<name> is local, branch:origin/<name>
// a branch of origin.
func branchOfKey(key string) (name string, remote bool) {
	name = strings.TrimPrefix(key, branchKeyPrefix)
	if rest, ok := strings.CutPrefix(name, "origin/"); ok {
		return rest, true
	}
	return name, false
}

// serviceWorktreeDir is where the service's service worktree is, or will be.
func (s *Service) serviceWorktreeDir(serviceID string) string {
	return filepath.Join(s.worktreeRoot(), serviceID)
}

func (s *Service) worktreeRoot() string {
	if s.opts.WorktreeRoot != "" {
		return filepath.Clean(s.opts.WorktreeRoot)
	}
	return filepath.Join(s.opts.LogDir, "service-worktrees")
}

// ownsWorktreeDir says dir is a direct child of the service worktrees folder:
// the only kind of path the module clears or removes.
func (s *Service) ownsWorktreeDir(dir string) bool {
	return filepath.Dir(filepath.Clean(dir)) == s.worktreeRoot() && filepath.Base(dir) != ".." && filepath.Base(dir) != "."
}

// checkoutBranch readies a branch run: the service worktree is made when it is
// not there and moved, detached, to the branch's tip with its tracked files
// reset, then the repository's saved env files are written into it. The run
// records the short commit it runs.
func (s *Service) checkoutBranch(ctx context.Context, r *run, p *prepared, lg *logFile) error {
	b, git := p.branch, s.opts.BranchGit
	if git == nil {
		return errors.New("branch runs are not available")
	}
	if !s.ownsWorktreeDir(b.dir) {
		return errors.New("the service worktree is not under the harness's service-worktrees folder")
	}
	ref := "refs/heads/" + b.name
	if b.remote {
		ref = "refs/remotes/origin/" + b.name
		if s.opts.Fetcher != nil {
			lg.append([]byte("[harness] fetching origin/" + b.name + "\n"))
			if err := s.opts.Fetcher.FetchServiceBranch(ctx, b.repoID, b.repoRoot, b.name); err != nil {
				return errors.New(redactUserinfo(err.Error()))
			}
		}
	}
	sha, err := git.ResolveCommit(ctx, b.repoRoot, ref)
	if err != nil {
		return err
	}
	if err := git.PrepareServiceWorktree(ctx, b.repoRoot, b.dir); err != nil {
		return err
	}
	if err := git.CheckoutDetached(ctx, b.dir, sha); err != nil {
		return err
	}
	if s.opts.EnvFiles != nil && s.opts.EnvWriter != nil {
		files, err := s.opts.EnvFiles.ListEnvFiles(ctx, b.repoID)
		if err != nil {
			return err
		}
		if len(files) > 0 {
			if err := s.opts.EnvWriter.Write(b.dir, files); err != nil {
				return errors.New("writing the repository's env files into the service worktree failed: " + err.Error())
			}
		}
	}
	short := shortSHA(sha)
	lg.append([]byte("[harness] branch: " + b.name + " @ " + short + " (service worktree)\n"))
	s.mu.Lock()
	defer s.mu.Unlock()
	if svc, err := s.store.GetService(r.serviceID); err == nil && svc.Run != nil && svc.Run.ID == r.id {
		svc.Run.Where.SHA = short
		svc.UpdatedAt = s.opts.Now().UTC()
		if err := s.store.SaveService(svc); err == nil {
			s.announceLocked(svc)
		}
	}
	return nil
}

// userinfoPattern matches the credentials of a URL, "://user:token@".
var userinfoPattern = regexp.MustCompile(`://[^/@\s]+@`)

// redactUserinfo drops the credentials a URL in git's message carries.
func redactUserinfo(s string) string { return userinfoPattern.ReplaceAllString(s, "://") }

// removeServiceWorktree removes the service's service worktree: git's
// registration and the directory, through the one path this module made. It
// never fails a delete: what is left is logged.
func (s *Service) removeServiceWorktree(ctx context.Context, serviceID string) {
	dir := s.serviceWorktreeDir(serviceID)
	if !s.ownsWorktreeDir(dir) {
		return
	}
	var err error
	if s.opts.BranchGit != nil {
		err = s.opts.BranchGit.RemoveServiceWorktree(ctx, dir)
	} else {
		err = os.RemoveAll(dir)
	}
	if err != nil {
		slog.Warn("remove a service worktree", "service", serviceID, "dir", dir, "err", err)
	}
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// runWorktrees is the worktrees a command service of the repository can run
// in now.
func (s *Service) runWorktrees(ctx context.Context, svc *domain.Service, repo *domain.Repository) ([]*domain.RunWorktree, error) {
	if s.opts.RunWorktrees == nil {
		return nil, nil
	}
	return s.opts.RunWorktrees.ListRepositoryRunWorktrees(ctx, svc.ProjectID, repo.ID)
}

// ListServiceRunWorktrees implements ports.ServiceManager.
func (s *Service) ListServiceRunWorktrees(ctx context.Context, serviceID string) ([]*domain.RunWorktree, error) {
	svc, err := s.store.GetService(serviceID)
	if err != nil {
		return nil, err
	}
	if svc.Kind != domain.ServiceKindCommand || svc.RepositoryID == "" {
		return []*domain.RunWorktree{}, nil
	}
	repo, err := s.repository(ctx, svc)
	if err != nil {
		return nil, err
	}
	all, err := s.runWorktrees(ctx, svc, repo)
	if err != nil {
		return nil, err
	}
	if all == nil {
		all = []*domain.RunWorktree{}
	}
	return all, nil
}

// ListServiceBranches implements ports.ServiceManager.
func (s *Service) ListServiceBranches(ctx context.Context, serviceID, query string) (*ports.ServiceBranchList, error) {
	none := &ports.ServiceBranchList{Branches: []ports.ServiceBranch{}}
	svc, err := s.store.GetService(serviceID)
	if err != nil {
		return nil, err
	}
	if svc.Kind != domain.ServiceKindCommand || svc.RepositoryID == "" || s.opts.BranchGit == nil {
		return none, nil
	}
	repo, err := s.repository(ctx, svc)
	if err != nil {
		return nil, err
	}
	if repo.RootDir == "" || !dirExists(repo.RootDir) {
		return none, nil
	}
	all, err := s.opts.BranchGit.ListServiceBranches(ctx, repo.RootDir)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	out := none
	for _, b := range all {
		if query != "" && !strings.Contains(strings.ToLower(b.Name), query) {
			continue
		}
		if len(out.Branches) == ports.ServiceBranchLimit {
			out.Truncated = true
			break
		}
		out.Branches = append(out.Branches, b)
	}
	return out, nil
}
