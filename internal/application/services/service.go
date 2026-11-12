// Package services keeps a project's services (ADR 0076): the programs the
// person defines on a project, each a command line or a container image,
// with the last run of each, and the feed that tells followers about every
// change. It is a context of its own, beside the apps that run App tab runs,
// and shares no state with them. This file holds the definitions; run.go
// runs them and log.go keeps their logs.
package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.ServiceManager = (*Service)(nil)

// DefaultRuntime is the container runtime image services use until the
// person picks another.
const DefaultRuntime = "docker"

// availableTTL is how long a lookup of the runtime's command is kept: a list
// read does no lookup of its own on every call.
const availableTTL = 10 * time.Second

// Options are what a Service reads from the rest of the harness.
type Options struct {
	// LogDir is where each service's log file lives, one file per service.
	LogDir string
	// Sandbox is the person's sandbox switch and Driver the platform's
	// driver (nil result: none); both nil reads as no sandbox in the way.
	Sandbox interface{ Enabled() bool }
	Driver  func() ports.SandboxDriver
	// Allow is the allowlist a command service's sandbox adds to its own paths
	// (what agents get, and the person's extra paths); Checkouts the main
	// checkouts of a project's repositories, which it reads. Nil: none.
	Allow     func() []ports.SandboxPath
	Checkouts func(projectID string) []string
	// LookPath finds a command on the server's PATH (exec.LookPath); nil
	// reads every command as missing.
	LookPath func(command string) (string, error)
	// Now is the clock; nil is time.Now.
	Now func() time.Time

	// Runners start the process of a run, by the kind of the service; a kind
	// with none cannot start.
	Runners map[domain.ServiceKind]ports.ServiceProcessRunner
	// StopGrace is how long a stop waits after SIGTERM before it kills; zero
	// is DefaultStopGrace.
	StopGrace time.Duration
	// LogCap is how much of a run's output its log keeps; zero is
	// DefaultLogCap.
	LogCap int64
	// FolderDir holds the harness-owned folder of each service with no
	// repository, one per service id; empty is a folder in LogDir.
	FolderDir string
	// EnvFiles reads a repository's saved env files, for the root .env a
	// service exports; nil exports nothing.
	EnvFiles ports.EnvFileLister
	// RunWorktrees lists the worktrees a command service of a repository can
	// run in besides its main checkout; nil offers the main checkout only.
	RunWorktrees ports.ServiceRunWorktrees
	// Branches names the branch a main checkout holds; nil leaves it empty.
	Branches ports.RunBranchReader
	// BranchGit is the git a branch run needs: the branch list, and the service
	// worktree (ADR 0080); nil offers no branch.
	BranchGit ports.ServiceBranchGit
	// Fetcher fetches a remote branch before a run on it; nil runs the tracking
	// ref as it is.
	Fetcher ports.ServiceBranchFetcher
	// WorktreeRoot holds the service worktrees, one folder per service id;
	// empty is a folder in LogDir.
	WorktreeRoot string
	// EnvWriter writes a repository's saved env files into a service worktree
	// before a branch run; nil writes none.
	EnvWriter ports.EnvFileIO
	// Runtime is the container runtime setting; nil keeps docker in memory.
	Runtime ports.ServiceRuntimeSetting
	// Instance labels the containers this harness starts; empty is a fresh id
	// per module.
	Instance string
	// Sweeper lists and removes the harness's labelled containers at boot;
	// nil leaves them.
	Sweeper ports.ServiceContainerSweeper
	// BaseEnv is the clean environment every run starts from; nil is the
	// server's own PATH, HOME and locale without the harness's shims.
	BaseEnv func() []string
}

// Service implements ports.ServiceManager.
type Service struct {
	store    ports.ServiceStore
	projects ports.ProjectReader
	repos    ports.RepositoryReader
	opts     Options

	// mu orders every change with its publication, so a list read under it
	// holds exactly the changes up to the sequence number it reports.
	mu    sync.Mutex
	seq   map[string]int64 // per project
	feed  *feed
	avail availableCache
	// runs are the live runs by service id. Guarded by mu.
	runs map[string]*run
	// closed is set by Shutdown: no run starts after it. Guarded by mu.
	closed bool

	logMu sync.Mutex
	logs  map[string]*logFile
}

// New returns the module over its store and the readers of projects and
// repositories.
func New(store ports.ServiceStore, projects ports.ProjectReader, repos ports.RepositoryReader, opts Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.StopGrace <= 0 {
		opts.StopGrace = DefaultStopGrace
	}
	if opts.LogCap <= 0 {
		opts.LogCap = DefaultLogCap
	}
	if opts.Runtime == nil {
		opts.Runtime = &memoryRuntime{}
	}
	if opts.Instance == "" {
		opts.Instance = newServiceID()
	}
	if opts.BaseEnv == nil {
		opts.BaseEnv = defaultBaseEnv
	}
	return &Service{
		store: store, projects: projects, repos: repos, opts: opts, seq: map[string]int64{}, feed: newFeed(),
		runs: map[string]*run{}, logs: map[string]*logFile{},
	}
}

func newServiceID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// LogPath is where the service's log is kept.
func (s *Service) LogPath(serviceID string) string {
	return filepath.Join(s.opts.LogDir, serviceID+".log")
}

// RunningServiceCount counts the project's starting and running services:
// one read of the store.
func (s *Service) RunningServiceCount(_ context.Context, projectID string) (int, error) {
	return s.store.RunningServices(projectID)
}

// ListServices reads the project's services by name, the feed's sequence
// number as of the read, the container runtime and the sandbox.
func (s *Service) ListServices(ctx context.Context, projectID string) (*ports.ServiceList, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	list, err := s.store.ListServices(projectID)
	seq := s.seq[projectID]
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &ports.ServiceList{Services: list, Seq: seq, Runtime: s.runtimeInfo(), Sandbox: s.sandboxState()}, nil
}

// GetService reads one service.
func (s *Service) GetService(_ context.Context, serviceID string) (*domain.Service, error) {
	return s.store.GetService(serviceID)
}

// CreateService saves a new, stopped service the person made.
func (s *Service) CreateService(ctx context.Context, projectID string, in domain.ServiceInput) (*domain.Service, error) {
	return s.CreateServiceBy(ctx, projectID, in, domain.ServiceStartedBy{Kind: "person"})
}

// ValidateServiceInput implements ports.ServiceManager: the checks of a create,
// or of an update of serviceID, on the definition in its stored form.
func (s *Service) ValidateServiceInput(ctx context.Context, projectID, serviceID string, in domain.ServiceInput) (domain.ServiceInput, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return in, err
	}
	in, err := in.Normalized()
	if err != nil {
		return in, err
	}
	if serviceID != "" {
		cur, err := s.store.GetService(serviceID)
		if err != nil {
			return in, err
		}
		if in.Kind != cur.Kind {
			return in, &domain.StructuredError{Code: "INVALID_INPUT", Message: "kind cannot be changed after the service is created"}
		}
	}
	if err := s.checkRepository(ctx, projectID, in.RepositoryID); err != nil {
		return in, err
	}
	if err := s.checkSubdir(ctx, in); err != nil {
		return in, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return in, s.checkNameFree(projectID, in.Name, serviceID)
}

// CreateServiceBy saves a new, stopped service made by by.
func (s *Service) CreateServiceBy(ctx context.Context, projectID string, in domain.ServiceInput, by domain.ServiceStartedBy) (*domain.Service, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	in, err := in.Normalized()
	if err != nil {
		return nil, err
	}
	if err := s.checkRepository(ctx, projectID, in.RepositoryID); err != nil {
		return nil, err
	}
	if err := s.checkSubdir(ctx, in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkNameFree(projectID, in.Name, ""); err != nil {
		return nil, err
	}
	now := s.opts.Now().UTC()
	svc := &domain.Service{
		ID: newServiceID(), ProjectID: projectID, Name: in.Name, Kind: in.Kind,
		RepositoryID: in.RepositoryID, ExportRepositoryEnv: in.ExportRepositoryEnv, Env: in.Env,
		Command: in.Command, Image: in.Image, Status: domain.ServiceStopped, CreatedBy: by, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.store.SaveService(svc); err != nil {
		return nil, err
	}
	s.announceLocked(svc)
	return svc, nil
}

// UpdateService replaces a service's definition and keeps its status and
// run. Its kind cannot change.
func (s *Service) UpdateService(ctx context.Context, serviceID string, in domain.ServiceInput) (*domain.Service, bool, error) {
	cur, err := s.store.GetService(serviceID)
	if err != nil {
		return nil, false, err
	}
	in, err = in.Normalized()
	if err != nil {
		return nil, false, err
	}
	if in.Kind != cur.Kind {
		return nil, false, &domain.StructuredError{Code: "INVALID_INPUT", Message: "kind cannot be changed after the service is created"}
	}
	if err := s.checkRepository(ctx, cur.ProjectID, in.RepositoryID); err != nil {
		return nil, false, err
	}
	if err := s.checkSubdir(ctx, in); err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Read again under the lock: a run may have changed the service since.
	cur, err = s.store.GetService(serviceID)
	if err != nil {
		return nil, false, err
	}
	if err := s.checkNameFree(cur.ProjectID, in.Name, cur.ID); err != nil {
		return nil, false, err
	}
	cur.Name, cur.RepositoryID, cur.ExportRepositoryEnv, cur.Env = in.Name, in.RepositoryID, in.ExportRepositoryEnv, in.Env
	cur.Command, cur.Image = in.Command, in.Image
	cur.UpdatedAt = s.opts.Now().UTC()
	if err := s.store.SaveService(cur); err != nil {
		return nil, false, err
	}
	s.announceLocked(cur)
	return cur, cur.Status.Active(), nil
}

// DeleteService removes a service that is not starting or running, and its
// log.
func (s *Service) DeleteService(ctx context.Context, serviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.store.GetService(serviceID)
	if err != nil {
		return err
	}
	if cur.Status.Active() {
		return &domain.StructuredError{Code: "SERVICE_RUNNING", Message: "service " + cur.Name + " is " + string(cur.Status) + ": stop it before deleting it"}
	}
	if err := s.store.DeleteService(serviceID); err != nil {
		return err
	}
	s.removeServiceWorktree(context.WithoutCancel(ctx), serviceID)
	s.forgetLog(serviceID)
	for _, p := range []string{s.LogPath(serviceID), s.LogPath(serviceID) + ".first"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	count, err := s.store.RunningServices(cur.ProjectID)
	if err != nil {
		return err
	}
	s.publishLocked(cur.ProjectID, ports.ServiceEvent{Kind: ports.ServiceDeleted, ServiceID: serviceID, RunningCount: count})
	return nil
}

// FollowServices follows the project's feed until ctx ends.
func (s *Service) FollowServices(ctx context.Context, projectID string) (<-chan ports.ServiceEvent, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.feed.follow(ctx, projectID), nil
}

// checkRepository refuses a repository that is unknown or of another
// project. No repository is fine.
func (s *Service) checkRepository(ctx context.Context, projectID, repositoryID string) error {
	if repositoryID == "" {
		return nil
	}
	r, err := s.repos.GetRepository(ctx, repositoryID)
	if err != nil || r == nil || r.ProjectID != projectID {
		return &domain.StructuredError{Code: "REPOSITORY_NOT_FOUND", Message: "repository " + repositoryID + " is not in this project"}
	}
	return nil
}

// checkSubdir refuses a command service whose subfolder is not a folder in
// its repository's main checkout, so a command typed into the wrong field is
// caught at the save and not at every start. A repository with no checkout on
// this machine has nothing to check against.
func (s *Service) checkSubdir(ctx context.Context, in domain.ServiceInput) error {
	if in.Command == nil || in.Command.Subdir == "" || in.RepositoryID == "" {
		return nil
	}
	repo, err := s.repos.GetRepository(ctx, in.RepositoryID)
	if err != nil || repo == nil || repo.RootDir == "" {
		return nil
	}
	if fi, err := os.Stat(filepath.Join(repo.RootDir, in.Command.Subdir)); err != nil || !fi.IsDir() {
		return &domain.StructuredError{Code: "INVALID_INPUT", Message: "command.subdir: " + strconv.Quote(in.Command.Subdir) + " is not a folder in " + repo.Name + "'s main checkout"}
	}
	return nil
}

// checkNameFree refuses a name another service of the project has. The
// caller holds mu.
func (s *Service) checkNameFree(projectID, name, exceptID string) error {
	list, err := s.store.ListServices(projectID)
	if err != nil {
		return err
	}
	for _, o := range list {
		if o.Name == name && o.ID != exceptID {
			return &domain.StructuredError{Code: "SERVICE_NAME_TAKEN", Message: "a service named " + name + " already exists in this project"}
		}
	}
	return nil
}

// announceLocked publishes service_changed for svc with the project's
// running count. The caller holds mu.
func (s *Service) announceLocked(svc *domain.Service) {
	count, _ := s.store.RunningServices(svc.ProjectID)
	s.publishLocked(svc.ProjectID, ports.ServiceEvent{Kind: ports.ServiceChanged, Service: svc.Clone(), RunningCount: count})
}

// publishLocked numbers the event within its project and sends it. The
// caller holds mu.
func (s *Service) publishLocked(projectID string, e ports.ServiceEvent) {
	s.seq[projectID]++
	e.Seq = s.seq[projectID]
	s.feed.announce(projectID, e)
}

// runtimeInfo names the container runtime and whether its command is found.
func (s *Service) runtimeInfo() ports.ServiceRuntimeInfo {
	name := s.opts.Runtime.ContainerRuntime()
	return ports.ServiceRuntimeInfo{Name: name, Available: s.avail.found(name, s.opts.LookPath, s.opts.Now(), availableTTL)}
}

// sandboxState reads the person's switch and the platform's driver.
func (s *Service) sandboxState() string {
	if !s.sandboxSwitchOn() {
		return ports.ServiceSandboxOff
	}
	if s.opts.Driver == nil || s.opts.Driver() == nil {
		return ports.ServiceSandboxUnavailable
	}
	return ports.ServiceSandboxOn
}

type availableCache struct {
	mu      sync.Mutex
	command string
	at      time.Time
	ok      bool
}

// found says whether command is on the PATH, looked up at most once per ttl.
func (c *availableCache) found(command string, look func(string) (string, error), now time.Time, ttl time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.command == command && now.Sub(c.at) < ttl {
		return c.ok
	}
	ok := false
	if look != nil {
		_, err := look(command)
		ok = err == nil
	}
	c.command, c.at, c.ok = command, now, ok
	return ok
}
