package agentsystem

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const noDetection = "nothing detected for this repository"

// Detector reads a repository's stack from its default branch, without
// touching the checkout, and answers the gate commands that stack implies
// (the harness defaults of the gates). The result is kept per repository and
// default-branch commit, and read again when the branch moves.
type Detector struct {
	git      ports.ArchitectureGit
	lookPath func(file string) (string, error)

	mu    sync.Mutex
	cache map[string]detection
}

type detection struct {
	commit string
	gates  map[domain.GateKind]domain.GateDefault
}

// NewDetector returns a detector over the git port; lookPath finds an
// executable on the harness's PATH (exec.LookPath).
func NewDetector(git ports.ArchitectureGit, lookPath func(string) (string, error)) *Detector {
	return &Detector{git: git, lookPath: lookPath, cache: map[string]detection{}}
}

// Detect answers the default of every required gate kind and of the optional
// kinds the stack names. A required kind it found no command for is there with
// no command and the reason. The answer is the caller's to keep.
func (d *Detector) Detect(ctx context.Context, repo *domain.Repository) map[domain.GateKind]domain.GateDefault {
	out := map[domain.GateKind]domain.GateDefault{}
	for k, v := range d.stack(ctx, repo) {
		out[k] = v
	}
	// A check.sh that names secret_scan fills it; otherwise gitleaks, looked
	// for on every call: installing it needs no new commit.
	if _, filled := out[domain.GateSecretScan]; !filled {
		if _, err := d.lookPath("gitleaks"); err == nil {
			out[domain.GateSecretScan] = domain.GateDefault{Command: "gitleaks detect --no-git --redact --exit-code 1", Reason: "gitleaks found on PATH"}
		} else {
			out[domain.GateSecretScan] = domain.GateDefault{Reason: "gitleaks not installed"}
		}
	}
	for _, k := range domain.RequiredGateKinds {
		if _, ok := out[k]; !ok {
			out[k] = domain.GateDefault{Reason: noDetection}
		}
	}
	return out
}

func (d *Detector) stack(ctx context.Context, repo *domain.Repository) map[domain.GateKind]domain.GateDefault {
	if repo.RootDir == "" {
		return nil // git would run in the harness's own directory
	}
	branch, err := d.git.DefaultBranch(ctx, repo.RootDir)
	if err != nil || branch == "" {
		return nil
	}
	commit, err := d.git.LastCommit(ctx, repo.RootDir, branch, ".")
	if err != nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if c, ok := d.cache[repo.ID]; ok && c.commit == commit && commit != "" {
		return c.gates
	}
	files, err := d.git.Files(ctx, repo.RootDir, branch)
	if err != nil {
		return nil
	}
	r := &checkoutReader{ctx: ctx, git: d.git, dir: repo.RootDir, branch: branch, files: files}
	gates := detectGates(r)
	d.cache[repo.ID] = detection{commit: commit, gates: gates}
	return gates
}

// checkoutReader reads files of the default branch.
type checkoutReader struct {
	ctx    context.Context
	git    ports.ArchitectureGit
	dir    string
	branch string
	files  []string
}

func (r *checkoutReader) has(path string) bool {
	for _, f := range r.files {
		if f == path {
			return true
		}
	}
	return false
}

func (r *checkoutReader) read(path string) string {
	b, ok, err := r.git.ReadFile(r.ctx, r.dir, r.branch, path)
	if err != nil || !ok {
		return ""
	}
	return string(b)
}

// gateSet fills each kind once: what a source fills first stays.
type gateSet map[domain.GateKind]domain.GateDefault

func (g gateSet) fill(kind domain.GateKind, command, from string) {
	if _, taken := g[kind]; !taken && command != "" {
		g[kind] = domain.GateDefault{Command: command, Reason: "detected from " + from}
	}
}

// detectGates reads the checkout's files: check.sh first, then the stacks.
func detectGates(r *checkoutReader) map[domain.GateKind]domain.GateDefault {
	g := gateSet{}
	detectCheckSh(r, g)
	detectGo(r, g)
	detectNode(r, g)
	detectRust(r, g)
	detectPython(r, g)
	return g
}

var dispatchesOnArg = regexp.MustCompile(`\$\{?1\b`)

// detectCheckSh: a script that dispatches on its first argument and names
// gate kinds fills each named kind as "./check.sh <kind>"; any other script
// fills test as "./check.sh".
func detectCheckSh(r *checkoutReader, g gateSet) {
	if !r.has("check.sh") {
		return
	}
	script := r.read("check.sh")
	named := false
	if dispatchesOnArg.MatchString(script) {
		for _, kind := range append(append([]domain.GateKind{}, domain.RequiredGateKinds...), domain.OptionalGateKinds...) {
			word := regexp.MustCompile(`(^|[^a-z_-])(` + regexp.QuoteMeta(string(kind)) + `|` + regexp.QuoteMeta(strings.ReplaceAll(string(kind), "_", "-")) + `)\)`)
			if word.MatchString(script) {
				g.fill(kind, "./check.sh "+string(kind), "check.sh")
				named = true
			}
		}
	}
	if !named {
		g.fill(domain.GateTest, "./check.sh", "check.sh")
	}
}

func detectGo(r *checkoutReader, g gateSet) {
	if !r.has("go.mod") {
		return
	}
	g.fill(domain.GateBuild, "go build ./...", "go.mod")
	g.fill(domain.GateTest, "go test ./...", "go.mod")
	g.fill(domain.GateLint, "go vet ./...", "go.mod")
	if r.has("go.sum") {
		g.fill(domain.GateLockfile, "go mod verify", "go.sum")
	}
}

func detectRust(r *checkoutReader, g gateSet) {
	if !r.has("Cargo.toml") {
		return
	}
	g.fill(domain.GateBuild, "cargo build --locked", "Cargo.toml")
	g.fill(domain.GateTest, "cargo test --locked", "Cargo.toml")
	g.fill(domain.GateLint, "cargo clippy --all-targets -- -D warnings", "Cargo.toml")
	if r.has("Cargo.lock") {
		g.fill(domain.GateLockfile, "cargo metadata --locked --format-version 1", "Cargo.lock")
	}
}

// detectNode fills a kind for each script package.json defines, run by the
// package manager its lockfile names.
func detectNode(r *checkoutReader, g gateSet) {
	if !r.has("package.json") {
		return
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	_ = json.Unmarshal([]byte(r.read("package.json")), &pkg)
	run := func(script string) string { return "npm run " + script }
	test := "npm test"
	lock, lockCmd := "", ""
	switch {
	case r.has("pnpm-lock.yaml"):
		run = func(script string) string { return "pnpm run " + script }
		test, lock, lockCmd = "pnpm test", "pnpm-lock.yaml", "pnpm install --frozen-lockfile"
	case r.has("yarn.lock"):
		run = func(script string) string { return "yarn " + script }
		test, lock, lockCmd = "yarn test", "yarn.lock", "yarn install --frozen-lockfile"
	case r.has("package-lock.json"):
		lock, lockCmd = "package-lock.json", "npm ci --dry-run"
	}
	for kind, script := range map[domain.GateKind]string{domain.GateBuild: "build", domain.GateLint: "lint", domain.GateTypecheck: "typecheck"} {
		if _, ok := pkg.Scripts[script]; ok {
			g.fill(kind, run(script), "package.json")
		}
	}
	if _, ok := pkg.Scripts["test"]; ok {
		g.fill(domain.GateTest, test, "package.json")
	}
	if lock != "" {
		g.fill(domain.GateLockfile, lockCmd, lock)
	}
}

func detectPython(r *checkoutReader, g gateSet) {
	from := ""
	switch {
	case r.has("pyproject.toml"):
		from = "pyproject.toml"
	case r.has("requirements.txt"):
		from = "requirements.txt"
	default:
		return
	}
	text := r.read(from)
	g.fill(domain.GateTest, "python -m pytest", from)
	switch {
	case strings.Contains(text, "ruff"):
		g.fill(domain.GateLint, "python -m ruff check .", from)
	case strings.Contains(text, "flake8"):
		g.fill(domain.GateLint, "python -m flake8", from)
	}
	if from == "pyproject.toml" && strings.Contains(text, "[build-system]") {
		g.fill(domain.GateBuild, "python -m build", from)
	}
	switch {
	case r.has("uv.lock"):
		g.fill(domain.GateLockfile, "uv lock --check", "uv.lock")
	case r.has("poetry.lock"):
		g.fill(domain.GateLockfile, "poetry check --lock", "poetry.lock")
	}
}
