package domain

import "testing"

func TestWhichSessionsAreGated(t *testing.T) {
	cases := []struct {
		mode    SessionMode
		hasDiff bool
		want    bool
	}{
		{SessionModeImplement, false, true},
		{SessionModeTest, false, true},
		{SessionModeDefault, true, true},
		{SessionModeDefault, false, false},
		{SessionModeArchitect, true, false},
		{SessionModeProduct, true, false},
		{SessionModeProductOwner, true, false},
		{SessionModeDesign, true, false},
		{SessionModeDesignSystem, true, false},
		{SessionModeReview, true, false},
		{SessionModeQA, true, false},
		{SessionModeOperate, true, false},
	}
	for _, c := range cases {
		if got := SessionGated(c.mode, c.hasDiff); got != c.want {
			t.Errorf("SessionGated(%q, diff=%v) = %v, want %v", c.mode, c.hasDiff, got, c.want)
		}
	}
}

func TestGuardConfigKeepsOnlyTheGuardPartOfTheEffectiveConfiguration(t *testing.T) {
	eff := ResolveAgentSystem(AgentSystemHarnessDefaults{Gates: map[GateKind]GateDefault{
		GateBuild: {Command: "go build ./..."}, GateTest: {Command: "go test ./..."}, GateLint: {Command: "go vet ./..."},
		GateLockfile: {Command: "go mod verify"},
	}})
	eff.Guard.RetryLimit = AgentSystemField[int]{Value: 5, Source: SourceProject}
	cfg := NewGuardConfig(eff, SessionModeImplement)

	if cfg.RetryLimit != 5 || !cfg.LockfileRule || cfg.TestsFailFirstAvailable {
		t.Errorf("config = %+v", cfg)
	}
	if len(cfg.Gates) != 5 || cfg.Gates[0].Kind != GateBuild || cfg.Gates[0].Command != "go build ./..." || cfg.Gates[0].TimeoutSeconds != DefaultGateTimeoutSeconds {
		t.Errorf("gates = %+v", cfg.Gates)
	}
	if len(cfg.MissingRequiredGates) != 1 || cfg.MissingRequiredGates[0] != GateSecretScan {
		t.Errorf("missing = %v", cfg.MissingRequiredGates)
	}
	if len(cfg.ProtectedPaths) == 0 || cfg.Mode != SessionModeImplement {
		t.Errorf("protected %v mode %q", cfg.ProtectedPaths, cfg.Mode)
	}
	// The kept value is the session's own: changing the effective value later changes nothing.
	eff.Guard.ProtectedPaths.Value[0] = "changed"
	eff.Guard.Gates[0].Command.Value = "changed"
	if cfg.ProtectedPaths[0] == "changed" || cfg.Gates[0].Command == "changed" {
		t.Error("the kept configuration shares memory with the effective one")
	}
}
