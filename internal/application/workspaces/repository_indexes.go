package workspaces

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.RepositoryIndexes     = (*RepositoryIndexes)(nil)
	_ ports.RepositoryIndexBuilds = (*RepositoryIndexes)(nil)
)

// IndexProjects is what repository indexes read of the projects context.
type IndexProjects interface {
	ports.ProjectReader
	ports.RepositoryLister
	ports.RepositoryReader
}

// RepositoryIndexDeps are what repository indexes need.
type RepositoryIndexDeps struct {
	Projects IndexProjects
	// Sessions says which repository an ended session ran on.
	Sessions ports.SessionReader
	// Installations has the installed codegraph; Activations says where it
	// is on.
	Installations ports.TokenSaverInstallations
	Activations   ports.TokenSaverActiveRepositories
	Indexer       ports.RepositoryIndexer
	// Now is the clock an index's age is measured with; nil is time.Now.
	Now func() time.Time
}

// RepositoryIndexes keeps each repository's codegraph index in its own
// checkout (ADR 0014). It builds one when codegraph becomes active for the
// repository while installed, or is installed while active there; rebuilds
// one on request, and when a session on the repository ends with the index
// older than domain.RepositoryIndexMaxAge. Every build runs in the
// background, one per checkout at a time; nothing waits for it.
type RepositoryIndexes struct {
	d RepositoryIndexDeps

	ctx    context.Context
	cancel context.CancelFunc
	work   sync.WaitGroup

	mu       sync.Mutex
	building map[string]bool   // checkout → a build runs there
	failed   map[string]string // checkout → why its last build failed
}

// NewRepositoryIndexes returns the service; Close stops its builds.
func NewRepositoryIndexes(d RepositoryIndexDeps) *RepositoryIndexes {
	if d.Now == nil {
		d.Now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &RepositoryIndexes{d: d, ctx: ctx, cancel: cancel, building: map[string]bool{}, failed: map[string]string{}}
}

// Subscribe follows codegraph's activations and installs, and sessions
// ending. Each reaction runs in the background: the request or session that
// published the event never waits for it.
func (x *RepositoryIndexes) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(_ context.Context, ev domain.TokenSaverActivationChanged) error {
		// Only "off" cannot turn codegraph on anywhere; inherit can, when
		// the level above is on.
		if ev.Saver == domain.TokenSaverCodegraph && ev.Value != domain.ActivationOff {
			x.background(func(ctx context.Context) { x.buildMissing(ctx, ev.ProjectID) })
		}
		return nil
	})
	ports.On(sub, func(_ context.Context, ev domain.TokenSaverInstallCompleted) error {
		if ev.Saver == domain.TokenSaverCodegraph {
			x.background(func(ctx context.Context) { x.buildMissing(ctx, "") })
		}
		return nil
	})
	ports.On(sub, func(_ context.Context, ev domain.RepositorySaved) error {
		// A repository added, or given its checkout, where codegraph is
		// already on.
		x.background(func(ctx context.Context) { x.buildMissing(ctx, ev.ProjectID) })
		return nil
	})
	ports.On(sub, func(_ context.Context, ev domain.SessionEnded) error {
		x.background(func(ctx context.Context) { x.refresh(ctx, ev.SessionID) })
		return nil
	})
}

// Wait blocks until every reaction and build started so far has ended.
func (x *RepositoryIndexes) Wait() { x.work.Wait() }

// Close stops the builds that run (their codegraph is killed) and waits for
// them.
func (x *RepositoryIndexes) Close() {
	x.cancel()
	x.work.Wait()
}

// ListRepositoryIndexes answers, project by project, every repository with
// an index, a build running or failed, or codegraph on at some level.
func (x *RepositoryIndexes) ListRepositoryIndexes(ctx context.Context) ([]domain.RepositoryIndex, error) {
	on, err := x.d.Activations.RepositoriesWithTokenSaverOn(ctx, domain.TokenSaverCodegraph, "")
	if err != nil {
		return nil, err
	}
	active := make(map[string]bool, len(on))
	for _, id := range on {
		active[id] = true
	}
	projects, err := x.d.Projects.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	out := []domain.RepositoryIndex{}
	for _, p := range projects {
		repos, err := x.d.Projects.ListRepositories(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		for _, r := range repos {
			if r.RootDir == "" {
				continue
			}
			idx := x.indexOf(r, p.Name)
			if idx.BuiltAt != nil || idx.Building || idx.Error != "" || active[r.ID] {
				out = append(out, idx)
			}
		}
	}
	return out, nil
}

// RebuildRepositoryIndex starts building the repository's index in the
// background, unless a build runs there already.
func (x *RepositoryIndexes) RebuildRepositoryIndex(ctx context.Context, repositoryID string) (*domain.RepositoryIndex, error) {
	r, err := x.d.Projects.GetRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	if r.RootDir == "" {
		return nil, &domain.StructuredError{Code: "INVALID_ROOT", Message: "repository has no root_dir: there is no checkout to index"}
	}
	codegraph := x.codegraph(ctx)
	if codegraph == "" {
		return nil, domain.TokenSaverErr("TOKEN_SAVER_NOT_INSTALLED", "codegraph is not installed: install it on the Token savers page, then rebuild the index")
	}
	x.start(codegraph, r.RootDir)
	projectName := ""
	if p, err := x.d.Projects.GetProject(ctx, r.ProjectID); err == nil {
		projectName = p.Name
	}
	idx := x.indexOf(r, projectName)
	return &idx, nil
}

// buildMissing builds the index of every repository of projectID (of every
// project when empty) where codegraph is on and has no index yet.
func (x *RepositoryIndexes) buildMissing(ctx context.Context, projectID string) {
	codegraph := x.codegraph(ctx)
	if codegraph == "" {
		return // the install will come back here
	}
	ids, err := x.d.Activations.RepositoriesWithTokenSaverOn(ctx, domain.TokenSaverCodegraph, projectID)
	if err != nil {
		slog.Warn("repository indexes: where codegraph is on was not read", "project", projectID, "err", err)
		return
	}
	for _, id := range ids {
		r, err := x.d.Projects.GetRepository(ctx, id)
		if err != nil || r.RootDir == "" {
			continue
		}
		if _, built := x.d.Indexer.IndexBuiltAt(r.RootDir); !built {
			x.start(codegraph, r.RootDir)
		}
	}
}

// refresh rebuilds the index of the ended session's repository when it is
// older than domain.RepositoryIndexMaxAge, and builds it when it is missing
// while codegraph resolves on there (its first build failed, or never ran).
func (x *RepositoryIndexes) refresh(ctx context.Context, sessionID string) {
	sess, err := x.d.Sessions.Get(ctx, sessionID)
	if err != nil || sess == nil || sess.RepositoryID == "" {
		return
	}
	r, err := x.d.Projects.GetRepository(ctx, sess.RepositoryID)
	if err != nil || r.RootDir == "" {
		return
	}
	builtAt, built := x.d.Indexer.IndexBuiltAt(r.RootDir)
	switch {
	case built && x.d.Now().Sub(builtAt) <= domain.RepositoryIndexMaxAge:
		return
	case !built && !x.codegraphOn(ctx, r):
		return
	}
	if codegraph := x.codegraph(ctx); codegraph != "" {
		x.start(codegraph, r.RootDir)
	}
}

// codegraphOn says whether codegraph resolves on for r at some level.
func (x *RepositoryIndexes) codegraphOn(ctx context.Context, r *domain.Repository) bool {
	ids, err := x.d.Activations.RepositoriesWithTokenSaverOn(ctx, domain.TokenSaverCodegraph, r.ProjectID)
	if err != nil {
		slog.Warn("repository indexes: where codegraph is on was not read", "project", r.ProjectID, "err", err)
		return false
	}
	return slices.Contains(ids, r.ID)
}

// codegraph is the installed codegraph executable, or "".
func (x *RepositoryIndexes) codegraph(ctx context.Context) string {
	installed, err := x.d.Installations.InstalledTokenSavers(ctx)
	if err != nil {
		slog.Warn("repository indexes: installations not read", "err", err)
		return ""
	}
	if in := domain.InstallationsBySaver(installed)[domain.TokenSaverCodegraph]; in != nil {
		return in.Path
	}
	return ""
}

// start builds checkout's index in the background, unless one is building.
func (x *RepositoryIndexes) start(codegraph, checkout string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.building[checkout] || x.ctx.Err() != nil {
		return
	}
	x.building[checkout] = true
	delete(x.failed, checkout)
	x.work.Add(1)
	go func() {
		defer x.work.Done()
		began := time.Now()
		err := x.d.Indexer.BuildIndex(x.ctx, codegraph, checkout)
		x.mu.Lock()
		delete(x.building, checkout)
		if err != nil {
			x.failed[checkout] = err.Error()
		}
		x.mu.Unlock()
		if err != nil {
			slog.Warn("repository index build failed", "checkout", checkout, "err", err)
			return
		}
		slog.Info("repository index built", "checkout", checkout, "took", time.Since(began).Round(time.Millisecond))
	}()
}

// background runs fn off the caller's goroutine, counted by Wait.
func (x *RepositoryIndexes) background(fn func(ctx context.Context)) {
	if x.ctx.Err() != nil {
		return
	}
	x.work.Add(1)
	go func() {
		defer x.work.Done()
		fn(x.ctx)
	}()
}

// IndexBuilding says whether a build runs on checkout now.
func (x *RepositoryIndexes) IndexBuilding(checkout string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.building[checkout]
}

// indexOf is r's index as it stands.
func (x *RepositoryIndexes) indexOf(r *domain.Repository, projectName string) domain.RepositoryIndex {
	idx := domain.RepositoryIndex{
		RepositoryID: r.ID, RepositoryName: r.DisplayName(),
		ProjectID: r.ProjectID, ProjectName: projectName, Checkout: r.RootDir,
	}
	if at, ok := x.d.Indexer.IndexBuiltAt(r.RootDir); ok {
		at = at.UTC()
		idx.BuiltAt = &at
	}
	x.mu.Lock()
	idx.Building, idx.Error = x.building[r.RootDir], x.failed[r.RootDir]
	x.mu.Unlock()
	return idx
}
