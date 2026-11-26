package agentsystem_test

import (
	"context"
	"errors"
	"testing"

	"encoding/json"

	"operators-mcp/internal/adapter/out/eventbus"
	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

type fakeProjects map[string]*domain.Project

func (f fakeProjects) Get(_ context.Context, id string) *domain.Project { return f[id] }

type fakeRepositories map[string]*domain.Repository

func (f fakeRepositories) Get(_ context.Context, id string) *domain.Repository { return f[id] }

// world is a project "p1" with repositories "r1" and "r2", and a repository
// "other" that belongs to project "p2".
func world() agentsystem.Deps {
	return worldOn(newStore())
}

func newStore() *sqlite.AgentSystemRepository {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		panic(err)
	}
	return sqlite.NewAgentSystemRepository(db)
}

func worldOn(store ports.AgentSystemStore) agentsystem.Deps {
	return agentsystem.Deps{
		Store:    store,
		Projects: fakeProjects{"p1": {ID: "p1"}, "p2": {ID: "p2"}},
		Repositories: fakeRepositories{
			"r1":    {ID: "r1", ProjectID: "p1"},
			"r2":    {ID: "r2", ProjectID: "p1"},
			"other": {ID: "other", ProjectID: "p2"},
		},
		ReviewModel: func() string { return "opus" },
	}
}

func TestEffectiveOfANewProjectIsTheHarnessDefaults(t *testing.T) {
	svc := agentsystem.New(world())

	got, err := svc.Effective(context.Background(), "p1", "r1")
	if err != nil {
		t.Fatal(err)
	}

	harness := func(name string, src domain.AgentSystemSource) {
		if src != domain.SourceHarness {
			t.Errorf("%s source = %q, want harness", name, src)
		}
	}
	harness("context_map.enabled", got.Learn.ContextMap.Enabled.Source)
	harness("task_memory.require_update", got.Learn.TaskMemory.RequireUpdate.Source)
	harness("protected_paths", got.Guard.ProtectedPaths.Source)
	harness("lockfile_rule.enabled", got.Guard.LockfileRule.Enabled.Source)
	harness("retry_limit", got.Guard.RetryLimit.Source)
	harness("tests_fail_first.available", got.Guard.TestsFailFirst.Available.Source)
	harness("critic.enabled", got.Review.Critic.Enabled.Source)
	harness("critic.model", got.Review.Critic.Model.Source)
	harness("critic.standards", got.Review.Critic.Standards.Source)
	harness("visual_qa.ui", got.Review.VisualQA.UI.Source)
	harness("trajectory_recording.default", got.Score.TrajectoryRecording.Default.Source)

	if !got.Learn.ContextMap.Enabled.Value || !got.Learn.TaskMemory.RequireUpdate.Value {
		t.Errorf("learn defaults = %+v, want both on", got.Learn)
	}
	if got.Guard.RetryLimit.Value != 3 {
		t.Errorf("retry limit = %d, want 3", got.Guard.RetryLimit.Value)
	}
	if !got.Guard.LockfileRule.Enabled.Value || got.Guard.TestsFailFirst.Available.Value {
		t.Errorf("guard toggles = %+v / %+v", got.Guard.LockfileRule, got.Guard.TestsFailFirst)
	}
	if !got.Review.Critic.Enabled.Value || got.Review.Critic.Model.Value != "opus" {
		t.Errorf("critic = %+v, want enabled on the review-mode model", got.Review.Critic)
	}
	if got.Review.VisualQA.UI.Value != nil {
		t.Errorf("visual_qa.ui = %+v, want none declared", got.Review.VisualQA.UI.Value)
	}
	if got.Score.TrajectoryRecording.Default.Value {
		t.Error("trajectory recording default on, want off")
	}
	if !contains(got.Guard.ProtectedPaths.Value, "check.sh") || !contains(got.Guard.ProtectedPaths.Value, ".github/workflows/**") {
		t.Errorf("protected paths = %v, want check.sh and CI config", got.Guard.ProtectedPaths.Value)
	}
	if !contains(got.Review.Critic.Standards.Value, "AGENTS.md") || !contains(got.Review.Critic.Standards.Value, "CLAUDE.md") {
		t.Errorf("standards = %v", got.Review.Critic.Standards.Value)
	}
}

func TestEffectiveWithNoDetectableGateMissesEveryRequiredGate(t *testing.T) {
	svc := agentsystem.New(world())

	got, err := svc.Effective(context.Background(), "p1", "r1")
	if err != nil {
		t.Fatal(err)
	}

	want := []domain.GateKind{domain.GateBuild, domain.GateTest, domain.GateLint, domain.GateSecretScan, domain.GateLockfile}
	if len(got.MissingRequiredGates) != len(want) {
		t.Fatalf("missing = %v, want %v", got.MissingRequiredGates, want)
	}
	for i, k := range want {
		if got.MissingRequiredGates[i] != k {
			t.Fatalf("missing = %v, want %v", got.MissingRequiredGates, want)
		}
	}
	if len(got.Guard.Gates) != 5 {
		t.Fatalf("gates = %d, want the five required kinds", len(got.Guard.Gates))
	}
	for _, g := range got.Guard.Gates {
		if g.TimeoutSeconds.Value != 600 || g.Command.Value != "" || g.Command.Source != domain.SourceHarness {
			t.Errorf("gate %s = %+v", g.Kind, g)
		}
	}
}

func TestEffectiveRefusesWhatIsNotInTheProject(t *testing.T) {
	svc := agentsystem.New(world())

	for _, c := range []struct{ project, repository, code string }{
		{"nope", "", "PROJECT_NOT_FOUND"},
		{"p1", "nope", "REPOSITORY_NOT_FOUND"},
		{"p1", "other", "REPOSITORY_NOT_FOUND"},
	} {
		_, err := svc.Effective(context.Background(), c.project, c.repository)
		var se *domain.StructuredError
		if !errors.As(err, &se) || se.Code != c.code {
			t.Errorf("Effective(%q, %q) error = %v, want %s", c.project, c.repository, err, c.code)
		}
	}
}

func TestEffectiveIsAValueTheLayerKeeps(t *testing.T) {
	svc := agentsystem.New(world())

	first, _ := svc.Effective(context.Background(), "p1", "r1")
	first.Guard.ProtectedPaths.Value[0] = "changed-by-the-layer"
	first.Guard.Gates[0].Command.Value = "changed"

	second, _ := svc.Effective(context.Background(), "p1", "r1")
	if second.Guard.ProtectedPaths.Value[0] == "changed-by-the-layer" || second.Guard.Gates[0].Command.Value == "changed" {
		t.Fatal("a change to one returned value showed in the next")
	}
}

func TestEffectiveWithNoRepositoryIsTheProjectDefaults(t *testing.T) {
	svc := agentsystem.New(world())

	got, err := svc.Effective(context.Background(), "p1", "")
	if err != nil || got.Guard.RetryLimit.Value != 3 {
		t.Fatalf("Effective(p1, \"\") = %+v, %v", got.Guard.RetryLimit, err)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

type recorder struct {
	events  []domain.Event
	changes []ports.ProjectChange
}

func (r *recorder) Publish(_ context.Context, ev ...domain.Event) error {
	r.events = append(r.events, ev...)
	return nil
}
func (r *recorder) AnnounceChange(c ports.ProjectChange) { r.changes = append(r.changes, c) }

func patch(t *testing.T, kv map[string]string) map[string]json.RawMessage {
	t.Helper()
	out := map[string]json.RawMessage{}
	for k, v := range kv {
		out[k] = json.RawMessage(v)
	}
	return out
}

func codes(t *testing.T, err error) map[string]string {
	t.Helper()
	var ve *domain.AgentSystemValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error = %v, want a validation error", err)
	}
	out := map[string]string{}
	for _, e := range ve.Errors {
		out[e.Field] = e.Code
	}
	return out
}

func TestAProjectDefaultIsFollowedByEveryRepositoryAndResetReturnsToTheHarnessDefault(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()

	if _, err := svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.retry_limit": "5", "learn.context_map.enabled": "false"})); err != nil {
		t.Fatal(err)
	}
	for _, repo := range []string{"r1", "r2"} {
		got, _ := svc.Effective(ctx, "p1", repo)
		if got.Guard.RetryLimit.Value != 5 || got.Guard.RetryLimit.Source != domain.SourceProject {
			t.Errorf("%s retry limit = %+v, want 5 from project", repo, got.Guard.RetryLimit)
		}
		if got.Learn.ContextMap.Enabled.Value || got.Learn.ContextMap.Enabled.Source != domain.SourceProject {
			t.Errorf("%s context map = %+v, want off from project", repo, got.Learn.ContextMap.Enabled)
		}
	}

	if _, err := svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.retry_limit": "null"})); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Effective(ctx, "p1", "r1")
	if got.Guard.RetryLimit.Value != 3 || got.Guard.RetryLimit.Source != domain.SourceHarness {
		t.Errorf("after reset retry limit = %+v, want 3 from harness", got.Guard.RetryLimit)
	}
	if got.Learn.ContextMap.Enabled.Source != domain.SourceProject {
		t.Error("resetting one field dropped another")
	}
}

func TestASaveWithOneInvalidFieldStoresNoneOfThem(t *testing.T) {
	d := world()
	rec := &recorder{}
	d.Events, d.Feed = rec, rec
	svc := agentsystem.New(d)
	ctx := context.Background()

	_, err := svc.SaveProject(ctx, "p1", patch(t, map[string]string{
		"learn.context_map.enabled": "false", "guard.retry_limit": "11", "nope.field": "1",
	}))

	got := codes(t, err)
	if got["guard.retry_limit"] != "OUT_OF_RANGE" || got["nope.field"] != "UNKNOWN_FIELD" || len(got) != 2 {
		t.Fatalf("errors = %v", got)
	}
	eff, _ := svc.Effective(ctx, "p1", "r1")
	if !eff.Learn.ContextMap.Enabled.Value || eff.Learn.ContextMap.Enabled.Source != domain.SourceHarness {
		t.Error("a refused save stored a field")
	}
	if len(rec.events) != 0 || len(rec.changes) != 0 {
		t.Error("a refused save announced a change")
	}
}

func TestRetryLimitBoundsAreOneAndTen(t *testing.T) {
	svc := agentsystem.New(world())
	for v, ok := range map[string]bool{"0": false, "1": true, "10": true, "11": false, "2.5": false, `"3"`: false} {
		_, err := svc.SaveProject(context.Background(), "p1", patch(t, map[string]string{"guard.retry_limit": v}))
		if (err == nil) != ok {
			t.Errorf("retry_limit %s: error = %v, want ok=%v", v, err, ok)
		}
	}
}

func TestSavedSettingsSurviveARestart(t *testing.T) {
	store := newStore()
	first := agentsystem.New(worldOn(store))
	if _, err := first.SaveProject(context.Background(), "p1", patch(t, map[string]string{"review.critic.enabled": "false"})); err != nil {
		t.Fatal(err)
	}

	second := agentsystem.New(worldOn(store))
	got, _ := second.Effective(context.Background(), "p1", "r1")
	if got.Review.Critic.Enabled.Value || got.Review.Critic.Enabled.Source != domain.SourceProject {
		t.Fatalf("critic enabled = %+v, want off from project", got.Review.Critic.Enabled)
	}
}

func TestEachSaveIsAnnouncedOnTheEventBusAndTheProjectFeed(t *testing.T) {
	d := world()
	rec := &recorder{}
	d.Events, d.Feed = rec, rec
	svc := agentsystem.New(d)

	if _, err := svc.SaveProject(context.Background(), "p1", patch(t, map[string]string{"guard.retry_limit": "4"})); err != nil {
		t.Fatal(err)
	}

	if len(rec.events) != 1 || rec.events[0] != (domain.AgentSystemSettingsChanged{ProjectID: "p1"}) {
		t.Fatalf("events = %+v, want one AgentSystemSettingsChanged for p1", rec.events)
	}
	if len(rec.changes) != 1 || rec.changes[0].AgentSystemSettings == nil || rec.changes[0].ProjectID() != "p1" {
		t.Fatalf("feed changes = %+v", rec.changes)
	}
}

func TestPrecedenceIsRepositoryThenProjectThenHarnessPerField(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	if _, err := svc.SaveProject(ctx, "p1", patch(t, map[string]string{
		"guard.retry_limit": "5", "guard.protected_paths": `["a/**","b/**"]`, "review.critic.enabled": "false",
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{
		"guard.retry_limit": "8", "guard.protected_paths": `["c/**"]`,
		"review.visual_qa.ui": `{"start_command":"npm start","url":"http://localhost:3000","routes":["/"],"viewports":["1280x800"],"ui_paths":["web/**"]}`,
	})); err != nil {
		t.Fatal(err)
	}

	r1, _ := svc.Effective(ctx, "p1", "r1")
	if r1.Guard.RetryLimit.Value != 8 || r1.Guard.RetryLimit.Source != domain.SourceRepository {
		t.Errorf("r1 retry limit = %+v, want 8 from repository", r1.Guard.RetryLimit)
	}
	if got := r1.Guard.ProtectedPaths; len(got.Value) != 1 || got.Value[0] != "c/**" || got.Source != domain.SourceRepository {
		t.Errorf("r1 protected paths = %+v, want the repository's list alone, never merged", got)
	}
	if r1.Review.Critic.Enabled.Value || r1.Review.Critic.Enabled.Source != domain.SourceProject {
		t.Errorf("r1 critic enabled = %+v, want off from project", r1.Review.Critic.Enabled)
	}
	if ui := r1.Review.VisualQA.UI; ui.Value == nil || ui.Value.StartCommand != "npm start" || ui.Source != domain.SourceRepository {
		t.Errorf("r1 visual_qa.ui = %+v", ui)
	}
	if r1.Learn.ContextMap.Enabled.Source != domain.SourceHarness {
		t.Errorf("r1 context map source = %q, want harness", r1.Learn.ContextMap.Enabled.Source)
	}

	r2, _ := svc.Effective(ctx, "p1", "r2")
	if r2.Guard.RetryLimit.Value != 5 || r2.Guard.RetryLimit.Source != domain.SourceProject || r2.Review.VisualQA.UI.Value != nil {
		t.Errorf("sibling r2 = %+v / %+v, want the project's default and no UI", r2.Guard.RetryLimit, r2.Review.VisualQA.UI)
	}
}

func TestResettingAnOverrideFollowsTheProjectDefaultAgain(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	_, _ = svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.retry_limit": "5"}))
	_, _ = svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{"guard.retry_limit": "8", "guard.lockfile_rule.enabled": "false"}))

	if _, err := svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{"guard.retry_limit": "null"})); err != nil {
		t.Fatal(err)
	}
	_, _ = svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.retry_limit": "6"}))

	got, _ := svc.Effective(ctx, "p1", "r1")
	if got.Guard.RetryLimit.Value != 6 || got.Guard.RetryLimit.Source != domain.SourceProject {
		t.Errorf("retry limit = %+v, want the project's later 6", got.Guard.RetryLimit)
	}
	if got.Guard.LockfileRule.Enabled.Value || got.Guard.LockfileRule.Enabled.Source != domain.SourceRepository {
		t.Errorf("resetting one override changed another: %+v", got.Guard.LockfileRule.Enabled)
	}
}

func TestARepositoryViewHoldsItsOwnOverridesAndAnnouncesTheRepository(t *testing.T) {
	d := world()
	rec := &recorder{}
	d.Events, d.Feed = rec, rec
	svc := agentsystem.New(d)

	view, err := svc.SaveRepository(context.Background(), "p1", "r1", patch(t, map[string]string{"guard.retry_limit": "8"}))
	if err != nil {
		t.Fatal(err)
	}
	if string(view.Values["guard.retry_limit"]) != "8" || len(view.Values) != 1 || len(view.Effective.MissingRequiredGates) != 5 {
		t.Fatalf("view = %+v", view)
	}
	if len(rec.events) != 1 || rec.events[0] != (domain.AgentSystemSettingsChanged{ProjectID: "p1", RepositoryID: "r1"}) {
		t.Fatalf("events = %+v", rec.events)
	}
	if rec.changes[0].AgentSystemSettings.RepositoryID != "r1" {
		t.Fatalf("feed = %+v", rec.changes[0].AgentSystemSettings)
	}
}

func TestARepositorySaveIsRefusedForARepositoryOfAnotherProject(t *testing.T) {
	svc := agentsystem.New(world())
	_, err := svc.SaveRepository(context.Background(), "p1", "other", patch(t, map[string]string{"guard.retry_limit": "8"}))
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "REPOSITORY_NOT_FOUND" {
		t.Fatalf("error = %v", err)
	}
}

func TestDeletingARepositoryOrProjectRemovesWhatItSet(t *testing.T) {
	d := world()
	bus := eventbus.New()
	svc := agentsystem.New(d)
	svc.Subscribe(bus)
	ctx := context.Background()
	_, _ = svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.retry_limit": "5"}))
	_, _ = svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{"guard.retry_limit": "8"}))
	_, _ = svc.SaveRepository(ctx, "p1", "r2", patch(t, map[string]string{"guard.retry_limit": "9"}))

	_ = bus.Publish(ctx, domain.RepositoryDeleted{RepositoryID: "r1", ProjectID: "p1"})
	if v, _ := d.Store.Values(ctx, domain.SourceRepository, "r1"); len(v) != 0 {
		t.Errorf("r1 still holds %v", v)
	}
	if v, _ := d.Store.Values(ctx, domain.SourceRepository, "r2"); len(v) != 1 {
		t.Errorf("r2 lost its override: %v", v)
	}

	_ = bus.Publish(ctx, domain.ProjectDeleted{ProjectID: "p1"})
	for _, c := range []struct {
		level domain.AgentSystemSource
		id    string
	}{{domain.SourceProject, "p1"}, {domain.SourceRepository, "r2"}} {
		if v, _ := d.Store.Values(ctx, c.level, c.id); len(v) != 0 {
			t.Errorf("%s %s still holds %v", c.level, c.id, v)
		}
	}
}
