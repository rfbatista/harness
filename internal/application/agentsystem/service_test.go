package agentsystem_test

import (
	"context"
	"errors"
	"testing"

	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/domain"
)

type fakeProjects map[string]*domain.Project

func (f fakeProjects) Get(_ context.Context, id string) *domain.Project { return f[id] }

type fakeRepositories map[string]*domain.Repository

func (f fakeRepositories) Get(_ context.Context, id string) *domain.Repository { return f[id] }

// world is a project "p1" with repositories "r1" and "r2", and a repository
// "other" that belongs to project "p2".
func world() agentsystem.Deps {
	return agentsystem.Deps{
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
