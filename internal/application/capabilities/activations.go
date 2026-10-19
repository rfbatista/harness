package capabilities

import (
	"cmp"
	"context"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Token saver activations: each saver's on/off setting at project,
// repository and task level (ADR 0015).

var (
	_ ports.TokenSaverActivations        = (*Service)(nil)
	_ ports.TokenSaverResolver           = (*Service)(nil)
	_ ports.TokenSaverActiveRepositories = (*Service)(nil)
)

// ActivationProjects is what activations read of the projects context: an
// owner's project, and a task's project's repositories.
type ActivationProjects interface {
	ports.ProjectReader
	ports.RepositoryReader
	ports.RepositoryLister
}

// Resolve is a token saver's activation for a session of task in
// repository of project: the most specific level that is not inherit wins,
// and a project that never set it is off. An empty value reads as inherit.
func Resolve(task, repository, project domain.ActivationValue) (domain.ActivationValue, domain.ActivationLevel) {
	switch {
	case task == domain.ActivationOn || task == domain.ActivationOff:
		return task, domain.ActivationLevelTask
	case repository == domain.ActivationOn || repository == domain.ActivationOff:
		return repository, domain.ActivationLevelRepository
	case project == domain.ActivationOn:
		return project, domain.ActivationLevelProject
	default:
		return domain.ActivationOff, domain.ActivationLevelProject
	}
}

// UseTasks connects the task reader a task-level activation needs. Planning
// is built after the catalog, so the host binds it once both exist; until
// then a task's activations are UNAVAILABLE.
func (s *Service) UseTasks(t ports.TicketReader) { s.tasks.Store(&t) }

// ListTokenSaverActivations returns each token saver's own value for owner
// and what it resolves to: for a task, once per repository of its project.
func (s *Service) ListTokenSaverActivations(ctx context.Context, owner domain.ActivationOwner) ([]domain.TokenSaverActivation, error) {
	sc, err := s.activationScope(ctx, owner)
	if err != nil {
		return nil, err
	}
	return s.activationsOf(ctx, owner, sc)
}

// SetTokenSaverActivation sets one token saver for owner and returns the
// owner's activations as they now read. Inherit removes the setting.
func (s *Service) SetTokenSaverActivation(ctx context.Context, owner domain.ActivationOwner, saver domain.TokenSaver, value domain.ActivationValue) ([]domain.TokenSaverActivation, error) {
	if _, err := domain.ParseTokenSaver(string(saver)); err != nil {
		return nil, err
	}
	if _, err := domain.ParseActivationLevel(string(owner.Level)); err != nil {
		return nil, err
	}
	if _, err := domain.ParseActivationValue(owner.Level, string(value)); err != nil {
		return nil, err
	}
	sc, err := s.activationScope(ctx, owner)
	if err != nil {
		return nil, err
	}
	if value == domain.ActivationInherit {
		err = s.activations.Delete(saver, owner)
	} else {
		err = s.activations.Put(domain.Activation{Saver: saver, Owner: owner, ProjectID: sc.projectID, Value: value})
	}
	if err != nil {
		return nil, err
	}
	if err := s.publish(domain.TokenSaverActivationChanged{Saver: saver, Owner: owner, ProjectID: sc.projectID, Value: value}); err != nil {
		return nil, err
	}
	return s.activationsOf(ctx, owner, sc)
}

// RepositoriesWithTokenSaverOn answers the repositories of projectID (of
// every project when empty) where saver resolves on at some level: on for
// their sessions without a task, or on for some task of their project (a
// task's setting wins in every repository).
func (s *Service) RepositoriesWithTokenSaverOn(ctx context.Context, saver domain.TokenSaver, projectID string) ([]string, error) {
	r, err := s.reach(ctx, saver, projectID)
	if err != nil {
		return nil, err
	}
	return r.repositories, nil
}

// saverReach is where a saver's stored activations take it.
type saverReach struct {
	// repositories are those where it resolves on, as
	// RepositoriesWithTokenSaverOn answers them.
	repositories []string
	// taskOn: some task turns it on.
	taskOn bool
}

// reach reads where saver resolves on in projectID (every project when
// empty).
func (s *Service) reach(ctx context.Context, saver domain.TokenSaver, projectID string) (saverReach, error) {
	if s.activations == nil || s.projects == nil {
		return saverReach{}, errNoActivations
	}
	stored, err := s.activations.ListSaver(saver)
	if err != nil {
		return saverReach{}, err
	}
	// Only a project holding some "on" can have a repository resolve on.
	projects := map[string]bool{} // project → a task of it turns saver on
	values := map[domain.ActivationOwner]domain.ActivationValue{}
	var out saverReach
	for _, a := range stored {
		values[a.Owner] = a.Value
		if a.Value == domain.ActivationOn && (projectID == "" || a.ProjectID == projectID) {
			taskOn := a.Owner.Level == domain.ActivationLevelTask
			projects[a.ProjectID] = projects[a.ProjectID] || taskOn
			out.taskOn = out.taskOn || taskOn
		}
	}
	for p, taskOn := range projects {
		repos, err := s.projects.ListRepositories(ctx, p)
		if err != nil {
			return saverReach{}, err
		}
		project := values[domain.ActivationOwner{Level: domain.ActivationLevelProject, ID: p}]
		for _, r := range repos {
			repository := values[domain.ActivationOwner{Level: domain.ActivationLevelRepository, ID: r.ID}]
			if v, _ := Resolve("", repository, project); taskOn || v == domain.ActivationOn {
				out.repositories = append(out.repositories, r.ID)
			}
		}
	}
	return out, nil
}

// ResolveTokenSavers resolves every token saver for a session. It reads only
// what is stored, so an id that no longer exists counts as inheriting.
func (s *Service) ResolveTokenSavers(ctx context.Context, projectID, repositoryID, taskID string) ([]domain.ResolvedTokenSaver, error) {
	if s.activations == nil {
		return nil, errNoActivations
	}
	project, err := s.storedValues(ctx, domain.ActivationLevelProject, projectID)
	if err != nil {
		return nil, err
	}
	repository, err := s.storedValues(ctx, domain.ActivationLevelRepository, repositoryID)
	if err != nil {
		return nil, err
	}
	task, err := s.storedValues(ctx, domain.ActivationLevelTask, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ResolvedTokenSaver, 0, len(domain.TokenSavers))
	for _, saver := range domain.TokenSavers {
		v, from := Resolve(task[saver], repository[saver], project[saver])
		out = append(out, domain.ResolvedTokenSaver{Saver: saver, Value: v, Level: from})
	}
	return out, nil
}

// subscribeActivations drops a deleted owner's activations; a deleted
// project takes those of its repositories and tasks too.
func (s *Service) subscribeActivations(sub ports.EventSubscriber) {
	if s.activations == nil {
		return
	}
	ports.On(sub, func(_ context.Context, ev domain.ProjectDeleted) error {
		return s.activations.DeleteProject(ev.ProjectID)
	})
	ports.On(sub, func(_ context.Context, ev domain.RepositoryDeleted) error {
		return s.activations.DeleteOwner(domain.ActivationOwner{Level: domain.ActivationLevelRepository, ID: ev.RepositoryID})
	})
	ports.On(sub, func(_ context.Context, ev domain.TicketDeleted) error {
		return s.activations.DeleteOwner(domain.ActivationOwner{Level: domain.ActivationLevelTask, ID: ev.TicketID})
	})
}

var errNoActivations = &domain.StructuredError{Code: "UNAVAILABLE", Message: "token saver activations are not configured on this server"}

// activationScope is an owner placed in its project: the levels at and
// above it, and for a task the repositories its sessions may run in.
type activationScope struct {
	projectID, repositoryID, taskID string
	repositories                    []*domain.Repository
}

func (s *Service) activationScope(ctx context.Context, owner domain.ActivationOwner) (activationScope, error) {
	if s.activations == nil || s.projects == nil {
		return activationScope{}, errNoActivations
	}
	switch owner.Level {
	case domain.ActivationLevelProject:
		p, err := s.projects.GetProject(ctx, owner.ID)
		if err != nil {
			return activationScope{}, err
		}
		return activationScope{projectID: p.ID}, nil
	case domain.ActivationLevelRepository:
		r, err := s.projects.GetRepository(ctx, owner.ID)
		if err != nil {
			return activationScope{}, err
		}
		return activationScope{projectID: r.ProjectID, repositoryID: r.ID}, nil
	case domain.ActivationLevelTask:
		tasks := s.tasks.Load()
		if tasks == nil {
			return activationScope{}, errNoActivations
		}
		t, err := (*tasks).GetTicket(ctx, owner.ID)
		if err != nil {
			return activationScope{}, err
		}
		repos, err := s.projects.ListRepositories(ctx, t.ProjectID)
		if err != nil {
			return activationScope{}, err
		}
		return activationScope{projectID: t.ProjectID, taskID: t.ID, repositories: repos}, nil
	}
	_, err := domain.ParseActivationLevel(string(owner.Level))
	return activationScope{}, err
}

// activationsOf reads owner's activations in its scope.
func (s *Service) activationsOf(ctx context.Context, owner domain.ActivationOwner, sc activationScope) ([]domain.TokenSaverActivation, error) {
	levels := map[domain.ActivationLevel]string{
		domain.ActivationLevelProject:    sc.projectID,
		domain.ActivationLevelRepository: sc.repositoryID,
		domain.ActivationLevelTask:       sc.taskID,
	}
	values := map[domain.ActivationLevel]map[domain.TokenSaver]domain.ActivationValue{}
	for level, id := range levels {
		v, err := s.storedValues(ctx, level, id)
		if err != nil {
			return nil, err
		}
		values[level] = v
	}
	project, repository, task := values[domain.ActivationLevelProject], values[domain.ActivationLevelRepository], values[domain.ActivationLevelTask]
	repositories, err := s.repositoriesValues(ctx, sc.repositories)
	if err != nil {
		return nil, err
	}
	// A project that never set a saver has it off; below it, unset inherits.
	unset := domain.ActivationInherit
	if owner.Level == domain.ActivationLevelProject {
		unset = domain.ActivationOff
	}

	out := make([]domain.TokenSaverActivation, 0, len(domain.TokenSavers))
	for _, saver := range domain.TokenSavers {
		a := domain.TokenSaverActivation{Saver: saver, Value: cmp.Or(values[owner.Level][saver], unset)}
		if len(sc.repositories) == 0 {
			v, from := Resolve(task[saver], repository[saver], project[saver])
			a.Resolved = []domain.Resolution{{Value: v, Level: from, RepositoryID: sc.repositoryID}}
		} else {
			for _, r := range sc.repositories {
				v, from := Resolve(task[saver], repositories[r.ID][saver], project[saver])
				a.Resolved = append(a.Resolved, domain.Resolution{Value: v, Level: from, RepositoryID: r.ID, RepositoryName: r.DisplayName()})
			}
		}
		a.Summary = a.Summarize()
		out = append(out, a)
	}
	return out, nil
}

// repositoriesValues reads the stored values of repos at once, by
// repository and saver.
func (s *Service) repositoriesValues(ctx context.Context, repos []*domain.Repository) (map[string]map[domain.TokenSaver]domain.ActivationValue, error) {
	out := make(map[string]map[domain.TokenSaver]domain.ActivationValue, len(repos))
	ids := make([]string, 0, len(repos))
	for _, r := range repos {
		out[r.ID] = map[domain.TokenSaver]domain.ActivationValue{}
		ids = append(ids, r.ID)
	}
	list, err := s.activations.ListOwners(ctx, domain.ActivationLevelRepository, ids)
	if err != nil {
		return nil, err
	}
	for _, a := range list {
		if values, ok := out[a.Owner.ID]; ok {
			values[a.Saver] = a.Value
		}
	}
	return out, nil
}

// storedValues reads one level's stored values by saver; none for an empty id.
func (s *Service) storedValues(ctx context.Context, level domain.ActivationLevel, id string) (map[domain.TokenSaver]domain.ActivationValue, error) {
	values := map[domain.TokenSaver]domain.ActivationValue{}
	if id == "" {
		return values, nil
	}
	list, err := s.activations.List(ctx, domain.ActivationOwner{Level: level, ID: id})
	if err != nil {
		return nil, err
	}
	for _, a := range list {
		values[a.Saver] = a.Value
	}
	return values, nil
}
