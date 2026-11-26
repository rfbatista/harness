package agentsystem_test

import (
	"context"
	"errors"
	"testing"

	"operators-mcp/internal/application/agentsystem"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeCheckout is a repository's default branch as the git port reads it.
type fakeCheckout struct {
	ports.ArchitectureGit
	files  map[string]string
	commit string
	reads  int
}

func (f *fakeCheckout) DefaultBranch(context.Context, string) (string, error) { return "main", nil }
func (f *fakeCheckout) Files(context.Context, string, string) ([]string, error) {
	var out []string
	for p := range f.files {
		out = append(out, p)
	}
	return out, nil
}
func (f *fakeCheckout) ReadFile(_ context.Context, _, _, path string) ([]byte, bool, error) {
	f.reads++
	c, ok := f.files[path]
	return []byte(c), ok, nil
}
func (f *fakeCheckout) LastCommit(context.Context, string, string, string) (string, error) {
	return f.commit, nil
}

func withGitleaks(string) (string, error) { return "/usr/local/bin/gitleaks", nil }
func noGitleaks(string) (string, error)   { return "", errors.New("not found") }

func detect(t *testing.T, files map[string]string, lookPath func(string) (string, error)) map[domain.GateKind]domain.GateDefault {
	t.Helper()
	d := agentsystem.NewDetector(&fakeCheckout{files: files, commit: "c1"}, lookPath)
	return d.Detect(context.Background(), &domain.Repository{ID: "r1", RootDir: "/repo"})
}

func wantGate(t *testing.T, got map[domain.GateKind]domain.GateDefault, kind domain.GateKind, command, reason string) {
	t.Helper()
	if g := got[kind]; g.Command != command || g.Reason != reason {
		t.Errorf("%s = %+v, want %q (%s)", kind, g, command, reason)
	}
}

func TestDetectionPerStack(t *testing.T) {
	t.Run("go", func(t *testing.T) {
		got := detect(t, map[string]string{"go.mod": "module x", "go.sum": ""}, noGitleaks)
		wantGate(t, got, domain.GateBuild, "go build ./...", "detected from go.mod")
		wantGate(t, got, domain.GateTest, "go test ./...", "detected from go.mod")
		wantGate(t, got, domain.GateLint, "go vet ./...", "detected from go.mod")
		wantGate(t, got, domain.GateLockfile, "go mod verify", "detected from go.sum")
	})
	t.Run("node with scripts", func(t *testing.T) {
		got := detect(t, map[string]string{
			"package.json":      `{"scripts":{"build":"vite build","test":"vitest","lint":"eslint .","typecheck":"tsc --noEmit"}}`,
			"package-lock.json": "{}",
		}, noGitleaks)
		wantGate(t, got, domain.GateBuild, "npm run build", "detected from package.json")
		wantGate(t, got, domain.GateTest, "npm test", "detected from package.json")
		wantGate(t, got, domain.GateLint, "npm run lint", "detected from package.json")
		wantGate(t, got, domain.GateTypecheck, "npm run typecheck", "detected from package.json")
		wantGate(t, got, domain.GateLockfile, "npm ci --dry-run", "detected from package-lock.json")
	})
	t.Run("node without scripts", func(t *testing.T) {
		got := detect(t, map[string]string{"package.json": `{"name":"x"}`}, noGitleaks)
		for _, k := range []domain.GateKind{domain.GateBuild, domain.GateTest, domain.GateLint, domain.GateLockfile} {
			wantGate(t, got, k, "", "nothing detected for this repository")
		}
	})
	t.Run("python", func(t *testing.T) {
		got := detect(t, map[string]string{"pyproject.toml": "[tool.ruff]\n[tool.pytest.ini_options]\n", "uv.lock": ""}, noGitleaks)
		wantGate(t, got, domain.GateTest, "python -m pytest", "detected from pyproject.toml")
		wantGate(t, got, domain.GateLint, "python -m ruff check .", "detected from pyproject.toml")
		wantGate(t, got, domain.GateLockfile, "uv lock --check", "detected from uv.lock")
	})
	t.Run("rust", func(t *testing.T) {
		got := detect(t, map[string]string{"Cargo.toml": "", "Cargo.lock": ""}, noGitleaks)
		wantGate(t, got, domain.GateBuild, "cargo build --locked", "detected from Cargo.toml")
		wantGate(t, got, domain.GateTest, "cargo test --locked", "detected from Cargo.toml")
		wantGate(t, got, domain.GateLint, "cargo clippy --all-targets -- -D warnings", "detected from Cargo.toml")
		wantGate(t, got, domain.GateLockfile, "cargo metadata --locked --format-version 1", "detected from Cargo.lock")
	})
	t.Run("empty repository", func(t *testing.T) {
		got := detect(t, map[string]string{"README.md": "hi"}, noGitleaks)
		for _, k := range domain.RequiredGateKinds {
			if got[k].Command != "" {
				t.Errorf("%s = %+v, want nothing detected", k, got[k])
			}
		}
	})
}

func TestCheckShFillsTheKindsItNamesOrTest(t *testing.T) {
	stack := map[string]string{"go.mod": "module x"}

	t.Run("dispatching script", func(t *testing.T) {
		files := map[string]string{"go.mod": "module x", "check.sh": "#!/bin/sh\ncase \"$1\" in\n  build) go build ./... ;;\n  lint) golangci-lint run ;;\nesac\n"}
		got := detect(t, files, noGitleaks)
		wantGate(t, got, domain.GateBuild, "./check.sh build", "detected from check.sh")
		wantGate(t, got, domain.GateLint, "./check.sh lint", "detected from check.sh")
		wantGate(t, got, domain.GateTest, "go test ./...", "detected from go.mod") // not named: the stack
	})
	t.Run("plain script", func(t *testing.T) {
		files := map[string]string{"go.mod": stack["go.mod"], "check.sh": "#!/bin/sh\ngo build ./... && go test ./...\n"}
		got := detect(t, files, noGitleaks)
		wantGate(t, got, domain.GateTest, "./check.sh", "detected from check.sh")
		wantGate(t, got, domain.GateBuild, "go build ./...", "detected from go.mod")
	})
	t.Run("without a stack", func(t *testing.T) {
		got := detect(t, map[string]string{"check.sh": "#!/bin/sh\nexit 0\n"}, noGitleaks)
		wantGate(t, got, domain.GateTest, "./check.sh", "detected from check.sh")
		wantGate(t, got, domain.GateBuild, "", "nothing detected for this repository")
	})
}

func TestSecretScanIsGitleaksWhenInstalled(t *testing.T) {
	got := detect(t, map[string]string{"go.mod": ""}, withGitleaks)
	wantGate(t, got, domain.GateSecretScan, "gitleaks detect --no-git --redact --exit-code 1", "gitleaks found on PATH")

	got = detect(t, map[string]string{"go.mod": ""}, noGitleaks)
	wantGate(t, got, domain.GateSecretScan, "", "gitleaks not installed")
}

func TestDetectionIsReusedWhileTheDefaultBranchStandsStillAndRerunWhenItMoves(t *testing.T) {
	co := &fakeCheckout{files: map[string]string{"go.mod": "module x"}, commit: "c1"}
	d := agentsystem.NewDetector(co, noGitleaks)
	repo := &domain.Repository{ID: "r1", RootDir: "/repo"}
	ctx := context.Background()

	d.Detect(ctx, repo)
	reads := co.reads
	d.Detect(ctx, repo)
	if co.reads != reads {
		t.Fatalf("the unchanged default branch was read again (%d -> %d reads)", reads, co.reads)
	}

	co.commit = "c2"
	co.files = map[string]string{"Cargo.toml": ""}
	got := d.Detect(ctx, repo)
	wantGate(t, got, domain.GateBuild, "cargo build --locked", "detected from Cargo.toml")
}

func TestDetectedGatesAreTheHarnessDefaultsAndAStoredOverrideWins(t *testing.T) {
	d := world()
	d.Detector = agentsystem.NewDetector(&fakeCheckout{files: map[string]string{"go.mod": "module x", "go.sum": ""}, commit: "c1"}, noGitleaks)
	svc := agentsystem.New(d)
	ctx := context.Background()

	got, _ := svc.Effective(ctx, "p1", "r1")
	test, _ := gate(got, domain.GateTest)
	if test.Command.Value != "go test ./..." || test.Command.Source != domain.SourceHarness || test.Command.Reason != "detected from go.mod" {
		t.Fatalf("test gate = %+v", test)
	}
	if len(got.MissingRequiredGates) != 1 || got.MissingRequiredGates[0] != domain.GateSecretScan {
		t.Fatalf("missing = %v, want only secret_scan (gitleaks not installed)", got.MissingRequiredGates)
	}
	if g, _ := gate(got, domain.GateSecretScan); g.Command.Reason != "gitleaks not installed" {
		t.Errorf("secret_scan reason = %q", g.Command.Reason)
	}

	_, _ = svc.SaveProject(ctx, "p1", patch(t, map[string]string{"guard.gates.test.command": `"make test"`}))
	got, _ = svc.Effective(ctx, "p1", "r1")
	if test, _ = gate(got, domain.GateTest); test.Command.Value != "make test" || test.Command.Source != domain.SourceProject {
		t.Errorf("project command must beat detection: %+v", test)
	}
}
