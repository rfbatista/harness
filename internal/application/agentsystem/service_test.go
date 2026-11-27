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
			"r1":    {ID: "r1", ProjectID: "p1", RootDir: "/repos/r1"},
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

func gate(eff domain.EffectiveAgentSystem, kind domain.GateKind) (domain.EffectiveGate, bool) {
	for _, g := range eff.Guard.Gates {
		if g.Kind == kind {
			return g, true
		}
	}
	return domain.EffectiveGate{}, false
}

func TestAGateIsOverriddenPerKindAndTheOthersAreInherited(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	_, err := svc.SaveProject(ctx, "p1", patch(t, map[string]string{
		"guard.gates.build.command": `"make build"`, "guard.gates.test.command": `"make test"`, "guard.gates.test.timeout_seconds": "900",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{
		"guard.gates.test.command": `"go test ./..."`, "guard.gates.build.timeout_seconds": "30",
	})); err != nil {
		t.Fatal(err)
	}

	got, _ := svc.Effective(ctx, "p1", "r1")
	test, _ := gate(got, domain.GateTest)
	if test.Command.Value != "go test ./..." || test.Command.Source != domain.SourceRepository || test.TimeoutSeconds.Value != 900 || test.TimeoutSeconds.Source != domain.SourceProject {
		t.Errorf("test gate = %+v, want the repository's command with the project's timeout", test)
	}
	build, _ := gate(got, domain.GateBuild)
	if build.Command.Value != "make build" || build.Command.Source != domain.SourceProject || build.TimeoutSeconds.Value != 30 || build.TimeoutSeconds.Source != domain.SourceRepository {
		t.Errorf("build gate = %+v, want the project's command with the repository's timeout", build)
	}
	if len(got.MissingRequiredGates) != 3 {
		t.Errorf("missing = %v, want lint, secret_scan, lockfile", got.MissingRequiredGates)
	}
	other, _ := svc.Effective(ctx, "p1", "r2")
	if g, _ := gate(other, domain.GateTest); g.Command.Value != "make test" {
		t.Errorf("sibling test gate = %+v", g)
	}
}

func TestRequiredGateRulesRefuseWithTheKindNamed(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	for _, c := range []struct{ field, value, code string }{
		{"guard.gates.test.command", `""`, "REQUIRED_GATE_MISSING"},
		{"guard.gates.lint.command", `"  "`, "REQUIRED_GATE_MISSING"},
		{"guard.gates.secret_scan.enabled", `false`, "REQUIRED_GATE_DISABLED"},
		{"guard.gates.nope.command", `"x"`, "UNKNOWN_FIELD"},
		{"guard.gates.test.nope", `"x"`, "UNKNOWN_FIELD"},
		{"guard.gates.test.timeout_seconds", `0`, "OUT_OF_RANGE"},
		{"guard.gates.sast.command", `""`, "OUT_OF_RANGE"},
	} {
		for _, save := range []func() error{
			func() error {
				_, err := svc.SaveProject(ctx, "p1", patch(t, map[string]string{c.field: c.value}))
				return err
			},
			func() error {
				_, err := svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{c.field: c.value}))
				return err
			},
		} {
			if got := codes(t, save()); got[c.field] != c.code {
				t.Errorf("%s = %s: %v, want %s", c.field, c.value, got, c.code)
			}
		}
	}
}

func TestResettingARequiredGateOverrideFallsBackToTheNextLevel(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	_, _ = svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.gates.test.command": `"make test"`}))
	_, _ = svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{"guard.gates.test.command": `"go test"`}))

	if _, err := svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{"guard.gates.test.command": "null"})); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Effective(ctx, "p1", "r1")
	if g, _ := gate(got, domain.GateTest); g.Command.Value != "make test" || g.Command.Source != domain.SourceProject {
		t.Errorf("test gate = %+v", g)
	}
	if _, err := svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.gates.test.command": "null"})); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.Effective(ctx, "p1", "r1")
	if len(got.MissingRequiredGates) != 5 {
		t.Errorf("a required gate with no command anywhere must be missing: %v", got.MissingRequiredGates)
	}
}

func TestOptionalGatesCanBeAddedChangedAndRemoved(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	if got, _ := svc.Effective(ctx, "p1", "r1"); len(got.Guard.Gates) != 5 {
		t.Fatalf("an optional gate with no command must be absent, got %d gates", len(got.Guard.Gates))
	}
	_, _ = svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.gates.sast.command": `"semgrep ci"`}))
	got, _ := svc.Effective(ctx, "p1", "r1")
	if g, ok := gate(got, domain.GateSAST); !ok || g.Command.Value != "semgrep ci" || g.TimeoutSeconds.Value != 600 {
		t.Fatalf("sast gate = %+v, %v", g, ok)
	}
	_, _ = svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{"guard.gates.sast.enabled": "false"}))
	got, _ = svc.Effective(ctx, "p1", "r1")
	if _, ok := gate(got, domain.GateSAST); ok {
		t.Error("an optional gate disabled by the repository is still listed")
	}
	if other, _ := svc.Effective(ctx, "p1", "r2"); len(other.Guard.Gates) != 6 {
		t.Error("the sibling lost the project's optional gate")
	}
}

const goodUI = `{"start_command":"npm start","url":"http://localhost:3000","routes":["/","/settings"],"viewports":["1280x800","375x667"],"ui_paths":["web/**/*.tsx"]}`

func TestGlobsModelsAndRoutesAreValidatedAtBothLevels(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	saves := map[string]func(map[string]json.RawMessage) error{
		"project":    func(p map[string]json.RawMessage) error { _, err := svc.SaveProject(ctx, "p1", p); return err },
		"repository": func(p map[string]json.RawMessage) error { _, err := svc.SaveRepository(ctx, "p1", "r1", p); return err },
	}
	invalid := []struct{ field, value, wantField, code string }{
		{"guard.protected_paths", `["ok/**","bad[x"]`, "guard.protected_paths[1]", "INVALID_GLOB"},
		{"guard.protected_paths", `[""]`, "guard.protected_paths[0]", "INVALID_GLOB"},
		{"review.critic.standards", `["AGENTS.md","["]`, "review.critic.standards[1]", "INVALID_GLOB"},
		{"review.critic.model", `"gpt-9"`, "review.critic.model", "MODEL_UNAVAILABLE"},
		{"review.critic.model", `""`, "review.critic.model", "MODEL_UNAVAILABLE"},
		{"review.visual_qa.ui", `{"url":"http://x","routes":["settings"],"viewports":["1x1"]}`, "review.visual_qa.ui.routes[0]", "INVALID_ROUTE"},
		{"review.visual_qa.ui", `{"url":"ftp://x","routes":["/"],"viewports":["1x1"]}`, "review.visual_qa.ui.url", "INVALID_ROUTE"},
		{"review.visual_qa.ui", `{"url":"http://x","routes":["/"],"viewports":["1280x800","wide"]}`, "review.visual_qa.ui.viewports[1]", "INVALID_ROUTE"},
		{"review.visual_qa.ui", `{"url":"http://x","routes":["/"],"viewports":["0x800"]}`, "review.visual_qa.ui.viewports[0]", "INVALID_ROUTE"},
		{"review.visual_qa.ui", `{"url":"http://x","routes":["/"],"viewports":["1x1"],"ui_paths":["a[b"]}`, "review.visual_qa.ui.ui_paths[0]", "INVALID_GLOB"},
	}
	for level, save := range saves {
		for _, c := range invalid {
			if got := codes(t, save(patch(t, map[string]string{c.field: c.value}))); got[c.wantField] != c.code {
				t.Errorf("%s %s = %s: %v, want %s at %s", level, c.field, c.value, got, c.code, c.wantField)
			}
		}
		for field, value := range map[string]string{
			"guard.protected_paths":   `["check.sh",".github/workflows/**","docs/*.md","src/[ab]?.go"]`,
			"review.critic.standards": `["AGENTS.md","docs/**"]`,
			"review.critic.model":     `"sonnet"`,
			"review.visual_qa.ui":     goodUI,
		} {
			if err := save(patch(t, map[string]string{field: value})); err != nil {
				t.Errorf("%s %s = %s refused: %v", level, field, value, err)
			}
		}
	}
}

func TestEveryModelTheHarnessOffersIsACriticModel(t *testing.T) {
	svc := agentsystem.New(world())
	for _, m := range domain.ModelChoices {
		if _, err := svc.SaveProject(context.Background(), "p1", patch(t, map[string]string{"review.critic.model": `"` + m + `"`})); err != nil {
			t.Errorf("model %q refused: %v", m, err)
		}
	}
	if got, _ := svc.Effective(context.Background(), "p1", "r1"); got.Review.Critic.Model.Value != "haiku" {
		t.Errorf("model = %+v", got.Review.Critic.Model)
	}
}

func TestResettingAListOrObjectFieldRemovesIt(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	_, _ = svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.protected_paths": `["x/**"]`, "review.critic.model": `"haiku"`, "review.visual_qa.ui": goodUI}))

	if _, err := svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.protected_paths": "null", "review.critic.model": "null", "review.visual_qa.ui": "null"})); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Effective(ctx, "p1", "r1")
	if got.Guard.ProtectedPaths.Source != domain.SourceHarness || got.Review.Critic.Model.Source != domain.SourceHarness || got.Review.VisualQA.UI.Source != domain.SourceHarness {
		t.Errorf("reset fields still set: %+v %+v %+v", got.Guard.ProtectedPaths, got.Review.Critic.Model, got.Review.VisualQA.UI)
	}
}

// vanishingRepositories answers the repository once, then reports it gone, as
// when it is deleted between two lookups of one call.
type vanishingRepositories struct {
	fakeRepositories
	calls int
}

func (v *vanishingRepositories) Get(ctx context.Context, id string) *domain.Repository {
	v.calls++
	if v.calls > 1 {
		return nil
	}
	return v.fakeRepositories.Get(ctx, id)
}

func TestEffectiveOfARepositoryDeletedMidCallIsNotFound(t *testing.T) {
	deps := world()
	deps.Repositories = &vanishingRepositories{fakeRepositories: deps.Repositories.(fakeRepositories)}
	deps.Detector = agentsystem.NewDetector(&fakeCheckout{files: map[string]string{"go.mod": "module x"}, commit: "c1"}, noGitleaks)

	_, err := agentsystem.New(deps).Effective(context.Background(), "p1", "r1")

	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "REPOSITORY_NOT_FOUND" {
		t.Fatalf("err = %v, want REPOSITORY_NOT_FOUND", err)
	}
}
