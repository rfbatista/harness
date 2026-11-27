package agentsystem_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/domain"
)

// Seam A, edge cases the first tickets did not cover.

func TestCheckShNamingSecretScanFillsItWhateverGitleaksIs(t *testing.T) {
	files := map[string]string{"check.sh": "#!/bin/sh\ncase \"$1\" in\n  secret_scan) ./scan ;;\n  build) make ;;\nesac\n"}
	for name, look := range map[string]func(string) (string, error){"gitleaks absent": noGitleaks, "gitleaks present": withGitleaks} {
		t.Run(name, func(t *testing.T) {
			got := detect(t, files, look)
			wantGate(t, got, domain.GateSecretScan, "./check.sh secret_scan", "detected from check.sh")
		})
	}
}

func TestCheckShAlternationNamesEachKind(t *testing.T) {
	files := map[string]string{"check.sh": "#!/bin/sh\ncase \"$1\" in\n  build|compile) make ;;\n  test|tests) make test ;;\nesac\n"}
	got := detect(t, files, noGitleaks)
	wantGate(t, got, domain.GateBuild, "./check.sh build", "detected from check.sh")
	wantGate(t, got, domain.GateTest, "./check.sh test", "detected from check.sh")
}

func TestCheckShDispatchingOnAParameterExpansionIsRecognised(t *testing.T) {
	files := map[string]string{"check.sh": "#!/bin/sh\ncase \"${1:-all}\" in\n  lint) x ;;\nesac\n"}
	wantGate(t, detect(t, files, noGitleaks), domain.GateLint, "./check.sh lint", "detected from check.sh")
}

func TestCheckShWordInsideAnotherWordIsNotAKind(t *testing.T) {
	files := map[string]string{"check.sh": "#!/bin/sh\ncase \"$1\" in\n  golint) x ;;\n  --test) y ;;\nesac\n"}
	got := detect(t, files, noGitleaks)
	wantGate(t, got, domain.GateLint, "", noDetection)
	wantGate(t, got, domain.GateTest, "./check.sh", "detected from check.sh") // names no kind: fills test
}

func TestDetectionAnswerIsTheCallersToKeep(t *testing.T) {
	d := agentsystem.NewDetector(&fakeCheckout{files: map[string]string{"go.mod": ""}, commit: "c1"}, noGitleaks)
	repo := &domain.Repository{ID: "r1", RootDir: "/repo"}
	first := d.Detect(context.Background(), repo)
	first[domain.GateBuild] = domain.GateDefault{Command: "poisoned"}
	delete(first, domain.GateTest)
	again := d.Detect(context.Background(), repo)
	wantGate(t, again, domain.GateBuild, "go build ./...", "detected from go.mod")
	wantGate(t, again, domain.GateTest, "go test ./...", "detected from go.mod")
}

func TestDetectionIsPerRepositoryEvenAtTheSameCommit(t *testing.T) {
	co := &fakeCheckout{files: map[string]string{"go.mod": ""}, commit: "same"}
	d := agentsystem.NewDetector(co, noGitleaks)
	ctx := context.Background()
	d.Detect(ctx, &domain.Repository{ID: "a", RootDir: "/a"})
	co.files = map[string]string{"Cargo.toml": ""} // repository b is another stack at the same commit id
	got := d.Detect(ctx, &domain.Repository{ID: "b", RootDir: "/b"})
	wantGate(t, got, domain.GateBuild, "cargo build --locked", "detected from Cargo.toml")
}

func TestInstallingGitleaksNeedsNoNewCommit(t *testing.T) {
	installed := false
	look := func(f string) (string, error) {
		if installed {
			return withGitleaks(f)
		}
		return noGitleaks(f)
	}
	d := agentsystem.NewDetector(&fakeCheckout{files: map[string]string{"go.mod": ""}, commit: "c1"}, look)
	repo := &domain.Repository{ID: "r1", RootDir: "/repo"}
	wantGate(t, d.Detect(context.Background(), repo), domain.GateSecretScan, "", "gitleaks not installed")
	installed = true
	wantGate(t, d.Detect(context.Background(), repo), domain.GateSecretScan, "gitleaks detect --no-git --redact --exit-code 1", "gitleaks found on PATH")
}

type failingGit struct{ fakeCheckout }

func (failingGit) DefaultBranch(context.Context, string) (string, error) {
	return "", errors.New("no such repository")
}

func TestAGitFailureLeavesEveryRequiredGateMissingWithoutError(t *testing.T) {
	d := world()
	d.Detector = agentsystem.NewDetector(&failingGit{}, noGitleaks)
	got, err := agentsystem.New(d).Effective(context.Background(), "p1", "r1")
	if err != nil {
		t.Fatalf("Effective failed on a git error: %v", err)
	}
	if len(got.MissingRequiredGates) != len(domain.RequiredGateKinds) {
		t.Errorf("missing = %v, want all required kinds", got.MissingRequiredGates)
	}
}

func TestDetectedNpmPackageManagersChooseTheirOwnCommands(t *testing.T) {
	got := detect(t, map[string]string{"package.json": `{"scripts":{"build":"b","test":"t","lint":"l"}}`, "pnpm-lock.yaml": ""}, noGitleaks)
	wantGate(t, got, domain.GateBuild, "pnpm run build", "detected from package.json")
	wantGate(t, got, domain.GateTest, "pnpm test", "detected from package.json")
	wantGate(t, got, domain.GateLockfile, "pnpm install --frozen-lockfile", "detected from pnpm-lock.yaml")
	got = detect(t, map[string]string{"package.json": `{"scripts":{"build":"b"}}`, "yarn.lock": ""}, noGitleaks)
	wantGate(t, got, domain.GateBuild, "yarn build", "detected from package.json")
	wantGate(t, got, domain.GateLockfile, "yarn install --frozen-lockfile", "detected from yarn.lock")
}

func TestGoWithoutAGoSumHasNoLockfileGate(t *testing.T) {
	got := detect(t, map[string]string{"go.mod": "module x"}, noGitleaks)
	wantGate(t, got, domain.GateLockfile, "", noDetection)
}

func TestAMalformedPackageJSONDetectsNothingAndDoesNotPanic(t *testing.T) {
	got := detect(t, map[string]string{"package.json": `{not json`}, noGitleaks)
	wantGate(t, got, domain.GateBuild, "", noDetection)
}

func TestAnOptionalGateIsRemovedPerLevelAndInheritedAcrossLevels(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	has := func(repo string) bool {
		t.Helper()
		eff, err := svc.Effective(ctx, "p1", repo)
		if err != nil {
			t.Fatal(err)
		}
		_, ok := gate(eff, domain.GateSAST)
		return ok
	}
	if has("r1") {
		t.Fatal("sast is present with nothing set")
	}
	_, _ = svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.gates.sast.command": `"semgrep"`}))
	if !has("r1") || !has("r2") {
		t.Fatal("a project optional gate must reach every repository")
	}
	_, _ = svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{"guard.gates.sast.enabled": `false`}))
	if has("r1") || !has("r2") {
		t.Fatal("a repository disables sast for itself only")
	}
	_, _ = svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{"guard.gates.sast.enabled": `null`}))
	if !has("r1") {
		t.Fatal("resetting enabled must restore the project's sast")
	}
	_, _ = svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.gates.sast.enabled": `false`}))
	if has("r1") || has("r2") {
		t.Fatal("a project-level removal must reach every repository")
	}
	// a repository that sets its own command while the project disabled the kind
	_, _ = svc.SaveRepository(ctx, "p1", "r2", patch(t, map[string]string{"guard.gates.sast.enabled": `true`, "guard.gates.sast.command": `"own"`}))
	eff, _ := svc.Effective(ctx, "p1", "r2")
	if g, ok := gate(eff, domain.GateSAST); !ok || g.Command.Value != "own" || g.Command.Source != domain.SourceRepository {
		t.Errorf("repository re-enabling sast: %+v ok=%v", g, ok)
	}
}

func TestADetectedOptionalGateCanBeDisabledForOneRepository(t *testing.T) {
	d := world()
	d.Detector = agentsystem.NewDetector(&fakeCheckout{files: map[string]string{"package.json": `{"scripts":{"typecheck":"tsc"}}`}, commit: "c"}, noGitleaks)
	svc := agentsystem.New(d)
	ctx := context.Background()
	eff, _ := svc.Effective(ctx, "p1", "r1")
	if _, ok := gate(eff, domain.GateTypecheck); !ok {
		t.Fatal("typecheck not detected")
	}
	_, _ = svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{"guard.gates.typecheck.enabled": `false`}))
	eff, _ = svc.Effective(ctx, "p1", "r1")
	if _, ok := gate(eff, domain.GateTypecheck); ok {
		t.Error("typecheck still present after disabling")
	}
}

func TestATimeoutIsResolvedPerKindSeparatelyFromTheCommand(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	_, _ = svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.gates.build.command": `"make"`, "guard.gates.build.timeout_seconds": `60`, "guard.gates.test.timeout_seconds": `120`}))
	_, _ = svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{"guard.gates.build.command": `"ninja"`}))
	eff, _ := svc.Effective(ctx, "p1", "r1")
	b, _ := gate(eff, domain.GateBuild)
	if b.Command.Source != domain.SourceRepository || b.TimeoutSeconds.Source != domain.SourceProject || b.TimeoutSeconds.Value != 60 {
		t.Errorf("build = %+v", b)
	}
	l, _ := gate(eff, domain.GateLint)
	if l.TimeoutSeconds.Value != domain.DefaultGateTimeoutSeconds || l.TimeoutSeconds.Source != domain.SourceHarness {
		t.Errorf("lint timeout = %+v", l.TimeoutSeconds)
	}
}

func TestEveryRefusalCodeOfTheContractHasAReachableCase(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	for _, c := range []struct{ field, value, code string }{
		{"guard.retry_limit", `0`, "OUT_OF_RANGE"},
		{"guard.retry_limit", `11`, "OUT_OF_RANGE"},
		{"guard.retry_limit", `-1`, "OUT_OF_RANGE"},
		{"guard.retry_limit", `"3"`, "INVALID_TYPE"},
		{"guard.retry_limit", `3.5`, "INVALID_TYPE"},
		{"guard.retry_limit", `true`, "INVALID_TYPE"},
		{"guard.retry_limit", `[3]`, "INVALID_TYPE"},
		{"guard.gates.test.timeout_seconds", `2147483648`, "OUT_OF_RANGE"},
		{"guard.gates.test.timeout_seconds", `"60"`, "INVALID_TYPE"},
		{"guard.gates.test.command", `5`, "INVALID_TYPE"},
		{"guard.gates.test.enabled", `"false"`, "INVALID_TYPE"},
		{"learn.context_map.enabled", `"yes"`, "INVALID_TYPE"},
		{"guard.protected_paths", `"check.sh"`, "INVALID_TYPE"},
		{"guard.protected_paths", `["ok", ""]`, "INVALID_GLOB"},
		{"review.critic.standards", `["a[b"]`, "INVALID_GLOB"},
		{"review.critic.model", `""`, "MODEL_UNAVAILABLE"},
		{"review.critic.model", `7`, "INVALID_TYPE"},
		{"review.visual_qa.ui", `{"url":"ftp://x"}`, "INVALID_ROUTE"},
		{"review.visual_qa.ui", `{"url":"http://"}`, "INVALID_ROUTE"},
		{"review.visual_qa.ui", `{"url":"http://x","viewports":["0x5"]}`, "INVALID_ROUTE"},
		{"review.visual_qa.ui", `{"url":"http://x","viewports":["1280x"]}`, "INVALID_ROUTE"},
		{"review.visual_qa.ui", `{"url":"http://x","viewports":["wide"]}`, "INVALID_ROUTE"},
		{"review.visual_qa.ui", `{"url":"http://x","ui_paths":["a[b"]}`, "INVALID_GLOB"},
		{"review.visual_qa.ui", `{"url":"http://x","bogus":1}`, "INVALID_TYPE"},
		{"review.visual_qa.ui", `"http://x"`, "INVALID_TYPE"},
		{"guard.nope", `1`, "UNKNOWN_FIELD"},
		{"guard.nope", `null`, "UNKNOWN_FIELD"}, // resetting a field that does not exist is still refused
		{"", `1`, "UNKNOWN_FIELD"},
	} {
		for level, save := range map[string]func() error{
			"project":    func() error { _, err := svc.SaveProject(ctx, "p1", patch(t, map[string]string{c.field: c.value})); return err },
			"repository": func() error { _, err := svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{c.field: c.value})); return err },
		} {
			var ve *domain.AgentSystemValidationError
			err := save()
			if !errors.As(err, &ve) {
				t.Errorf("%s %s=%s: err = %v, want a validation error", level, c.field, c.value, err)
				continue
			}
			found := false
			for _, e := range ve.Errors {
				found = found || e.Code == c.code
			}
			if !found {
				t.Errorf("%s %s=%s: %+v, want code %s", level, c.field, c.value, ve.Errors, c.code)
			}
		}
	}
}

func TestASaveNamesEveryRefusedFieldAtOnce(t *testing.T) {
	svc := agentsystem.New(world())
	_, err := svc.SaveProject(context.Background(), "p1", patch(t, map[string]string{
		"guard.retry_limit": `99`, "guard.gates.lint.enabled": `false`, "review.critic.model": `"gpt-9"`, "guard.retry_limit_typo": `1`, "learn.context_map.enabled": `true`,
	}))
	got := codes(t, err)
	want := map[string]string{"guard.retry_limit": "OUT_OF_RANGE", "guard.gates.lint.enabled": "REQUIRED_GATE_DISABLED", "review.critic.model": "MODEL_UNAVAILABLE", "guard.retry_limit_typo": "UNKNOWN_FIELD"}
	if len(got) != len(want) {
		t.Fatalf("errors = %v, want %v", got, want)
	}
	for f, c := range want {
		if got[f] != c {
			t.Errorf("%s = %s, want %s", f, got[f], c)
		}
	}
	eff, _ := svc.Effective(context.Background(), "p1", "")
	if eff.Learn.ContextMap.Enabled.Source != domain.SourceHarness {
		t.Error("the valid field of a refused save was stored")
	}
}

func TestBoundaryValuesAreAccepted(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	for _, kv := range []map[string]string{
		{"guard.retry_limit": `1`}, {"guard.retry_limit": `10`},
		{"guard.gates.test.timeout_seconds": `1`}, {"guard.gates.test.timeout_seconds": `2147483647`},
		{"guard.protected_paths": `[]`}, {"guard.protected_paths": `["**", "a/**/b", "*.lock"]`},
		{"review.visual_qa.ui": `{"url":"https://localhost:3000","routes":["/"],"viewports":["1x1","1280x800"],"ui_paths":["web/**"]}`},
		{"review.visual_qa.ui": `null`},
	} {
		if _, err := svc.SaveProject(ctx, "p1", patch(t, kv)); err != nil {
			t.Errorf("%v refused: %v", kv, err)
		}
	}
}

func TestAnEmptyPatchSavesNothingAndKeepsWhatWasSet(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	_, _ = svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.retry_limit": `4`}))
	view, err := svc.SaveProject(ctx, "p1", patch(t, map[string]string{}))
	if err != nil || view.Effective.Guard.RetryLimit.Value != 4 || len(view.Values) != 1 {
		t.Errorf("view = %+v err = %v", view, err)
	}
}

func TestResettingAFieldNeverSetIsNotAnError(t *testing.T) {
	svc := agentsystem.New(world())
	if _, err := svc.SaveRepository(context.Background(), "p1", "r1", patch(t, map[string]string{"guard.retry_limit": `null`})); err != nil {
		t.Fatal(err)
	}
}

func TestSavingTheSameValueTwiceIsIdempotent(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	for range 2 {
		if _, err := svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.retry_limit": `4`})); err != nil {
			t.Fatal(err)
		}
	}
	view, _ := svc.ProjectView(ctx, "p1")
	if len(view.Values) != 1 || view.Effective.Guard.RetryLimit.Value != 4 {
		t.Errorf("view = %+v", view)
	}
}

func TestOverridesOfOneRepositoryNeverLeakToItsSibling(t *testing.T) {
	svc := agentsystem.New(world())
	ctx := context.Background()
	_, _ = svc.SaveRepository(ctx, "p1", "r1", patch(t, map[string]string{"guard.retry_limit": `7`, "guard.gates.build.command": `"x"`}))
	eff, _ := svc.Effective(ctx, "p1", "r2")
	if eff.Guard.RetryLimit.Value != 3 || eff.Guard.RetryLimit.Source != domain.SourceHarness {
		t.Errorf("r2 retry = %+v", eff.Guard.RetryLimit)
	}
	if g, _ := gate(eff, domain.GateBuild); g.Command.Value != "" {
		t.Errorf("r2 build = %+v", g)
	}
}

func TestAFailedSaveAnnouncesNothing(t *testing.T) {
	rec := &recorder{}
	d := world()
	d.Events, d.Feed = rec, rec
	svc := agentsystem.New(d)
	_, _ = svc.SaveProject(context.Background(), "p1", patch(t, map[string]string{"guard.retry_limit": `99`}))
	_, _ = svc.SaveRepository(context.Background(), "p1", "other", patch(t, map[string]string{"guard.retry_limit": `5`}))
	_, _ = svc.SaveProject(context.Background(), "nope", patch(t, map[string]string{"guard.retry_limit": `5`}))
	if len(rec.events) != 0 || len(rec.changes) != 0 {
		t.Errorf("announced %v / %v", rec.events, rec.changes)
	}
}

func TestEffectiveIsSafeForConcurrentLayers(t *testing.T) {
	d := world()
	d.Detector = agentsystem.NewDetector(&fakeCheckout{files: map[string]string{"go.mod": "", "go.sum": ""}, commit: "c"}, noGitleaks)
	svc := agentsystem.New(d)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%4 == 0 {
				_, _ = svc.SaveRepository(context.Background(), "p1", "r1", patch(t, map[string]string{"guard.retry_limit": `5`}))
			}
			if _, err := svc.Effective(context.Background(), "p1", "r1"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestEffectiveAfterAProjectDeleteIsNotFoundNotStale(t *testing.T) {
	d := world()
	svc := agentsystem.New(d)
	_, err := svc.Effective(context.Background(), "gone", "")
	var se *domain.StructuredError
	if !errors.As(err, &se) || se.Code != "PROJECT_NOT_FOUND" {
		t.Errorf("err = %v", err)
	}
	_, err = svc.Effective(context.Background(), "p1", "nope")
	if !errors.As(err, &se) || se.Code != "REPOSITORY_NOT_FOUND" {
		t.Errorf("err = %v", err)
	}
}
