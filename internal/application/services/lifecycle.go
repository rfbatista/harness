package services

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Services never outlive the harness server and never start by themselves:
// Shutdown stops every run, Recover settles what a server that died left
// behind, and a deleted project takes its services with it.

// Shutdown stops every live run in parallel, each with its escalation
// (SIGTERM, then SIGKILL after the stop grace), and returns once they have
// all ended or ctx is done. No run starts after it.
func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	ids := make([]string, 0, len(s.runs))
	for id := range s.runs {
		ids = append(ids, id)
	}
	s.mu.Unlock()

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.StopService(ctx, id); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}

// Recover is the boot clean-up. Every service left starting or running in
// the store belongs to a run of a server that is gone: it becomes stopped,
// its log ends with the restart line and followers are told. Nothing is
// started. Then the containers carrying this harness's instance label, which
// that server may have left running, are removed (another harness's stay) from
// each container runtime that is available; a runtime that is missing or fails
// is logged, never fatal.
func (s *Service) Recover(ctx context.Context) error {
	active, err := s.store.ActiveServices()
	if err != nil {
		return err
	}
	for _, svc := range active {
		s.settleLeftover(svc)
	}
	s.sweepContainers(ctx)
	return nil
}

// settleLeftover ends the run a previous server left going in svc.
func (s *Service) settleLeftover(svc *domain.Service) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runs[svc.ID] != nil {
		return
	}
	s.logFor(svc).append([]byte("[harness] the harness restarted\n"))
	s.moveLocked(svc.ID, domain.ServiceStopped, func(cur *domain.Service) {
		cur.Run.EndedAt = s.opts.Now().UTC().Format(time.RFC3339)
	})
}

// sweepContainers removes every container this harness labelled, from both
// runtimes whose command is on the PATH: the person may have switched the
// setting since a run started, and its container stays with the old runtime.
func (s *Service) sweepContainers(ctx context.Context) {
	if s.opts.Sweeper == nil || s.opts.LookPath == nil {
		return
	}
	for _, runtime := range []string{"docker", "podman"} {
		if _, err := s.opts.LookPath(runtime); err != nil {
			continue
		}
		s.sweepRuntime(ctx, runtime)
	}
}

func (s *Service) sweepRuntime(ctx context.Context, runtime string) {
	found, err := s.opts.Sweeper.ListServiceContainers(ctx, runtime)
	if err != nil {
		slog.Warn("list the containers services left behind", "runtime", runtime, "err", err)
		return
	}
	for _, c := range found {
		if c.Instance != s.opts.Instance {
			continue
		}
		if err := s.opts.Sweeper.RemoveServiceContainer(ctx, runtime, c.Name); err != nil {
			slog.Warn("remove a container a service left behind", "runtime", runtime, "container", c.Name, "err", err)
			continue
		}
		slog.Info("removed a container a service left behind", "container", c.Name, "service", c.ServiceID)
	}
}

// Subscribe makes the module delete a project's services when the project is
// deleted: each is stopped, then removed with its log, and announced as
// service_deleted.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(ctx context.Context, ev domain.ServiceRequestChanged) error {
		s.requestChanged(ev)
		return nil
	})
	ports.On(sub, func(ctx context.Context, ev domain.RepositoryDeleted) error {
		s.removeRepositoryWorktrees(ctx, ev.ProjectID, ev.RepositoryID)
		return nil
	})
	ports.On(sub, func(ctx context.Context, ev domain.ProjectDeleted) error {
		s.deleteProjectServices(ctx, ev.ProjectID)
		return nil
	})
}

// removeRepositoryWorktrees removes the service worktree of each service of the
// repository. The services stay: a start of one is refused while the repository
// is gone.
func (s *Service) removeRepositoryWorktrees(ctx context.Context, projectID, repositoryID string) {
	list, err := s.store.ListServices(projectID)
	if err != nil {
		slog.Warn("list a repository's services", "repository", repositoryID, "err", err)
		return
	}
	for _, svc := range list {
		if svc.RepositoryID == repositoryID {
			s.removeServiceWorktree(context.WithoutCancel(ctx), svc.ID)
		}
	}
}

// deleteProjectServices is best effort: the project is already gone, so a
// failure is logged and the rest are still removed.
func (s *Service) deleteProjectServices(ctx context.Context, projectID string) {
	list, err := s.store.ListServices(projectID)
	if err != nil {
		slog.Warn("list a deleted project's services", "project", projectID, "err", err)
		return
	}
	// The stop outlives the request that deleted the project: a process
	// must not stay because that request ended.
	ctx = context.WithoutCancel(ctx)
	var wg sync.WaitGroup
	for _, svc := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.StopService(ctx, svc.ID); err != nil {
				slog.Warn("stop a deleted project's service", "service", svc.ID, "err", err)
			}
		}()
	}
	wg.Wait()
	for _, svc := range list {
		if err := s.DeleteService(ctx, svc.ID); err != nil {
			slog.Warn("delete a deleted project's service", "service", svc.ID, "err", err)
		}
	}
}
