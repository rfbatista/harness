package httpapi

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/app/runtime"
	"operators-mcp/internal/application/services"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

func runOf(out map[string]any) map[string]any {
	return out["service"].(map[string]any)["run"].(map[string]any)
}

// With the sandbox on, a run is confined by the policy the agent builder makes
// of the working directory, the service folder and the allowlist: what the
// driver is asked to wrap is checked here, what it confines further down.
func TestServicesSandbox_APolicyHoldsTheWorkingDirTheFolderAndTheAllowlist(t *testing.T) {
	driver := &portstest.RecordingSandboxDriver{DriverName: "fake-driver"}
	folders, logs, extra, readOnly := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	checkouts := []string{"/checkouts/api", "/checkouts/web"}
	f := newServicesFixture(t, services.Options{
		Sandbox: fakeSandboxSwitch(true), Driver: func() ports.SandboxDriver { return driver },
		FolderDir: folders, LogDir: logs,
		Allow:     func() []ports.SandboxPath { return []ports.SandboxPath{{Path: extra, Write: true}, {Path: readOnly}} },
		Checkouts: func(string) []string { return checkouts },
	})
	id := f.command("web", "echo hi")

	code, out := f.operate("start_service", id)
	if code != 200 || runOf(out)["sandboxed"] != true {
		t.Fatalf("start = %d %v", code, out)
	}
	f.awaitStatus(id, "exited")

	calls := driver.Calls()
	if len(calls) != 1 {
		t.Fatalf("the driver wrapped %d commands, want 1", len(calls))
	}
	p, c := calls[0].Policy, calls[0].Command
	folder := filepath.Join(folders, id)
	if p.WorkingDir != f.repoDir() || c.Dir != f.repoDir() {
		t.Fatalf("working dir = %q, command dir %q, want %q", p.WorkingDir, c.Dir, f.repoDir())
	}
	for _, w := range []string{f.repoDir(), folder, extra} {
		if !slices.Contains(p.Writable, w) {
			t.Errorf("%s is not writable: %v", w, p.Writable)
		}
	}
	for _, r := range append([]string{readOnly}, checkouts...) {
		if !slices.Contains(p.Readable, r) {
			t.Errorf("%s is not readable: %v", r, p.Readable)
		}
	}
	for _, w := range p.Writable {
		if w == logs || strings.HasPrefix(f.svc.LogPath(id), w+"/") {
			t.Errorf("the log %s is writable through %s", f.svc.LogPath(id), w)
		}
	}
	if len(p.Hidden) != 0 || len(p.CreateOnly) != 0 {
		t.Errorf("a service hides and creates nothing: %+v", p)
	}
	if c.Path != "/bin/sh" || len(c.Args) != 2 || c.Args[1] != "echo hi" {
		t.Errorf("command = %+v", c)
	}
	if !slices.Contains(c.Env, "HARNESS_SANDBOX=fake-driver") || !slices.Contains(c.Env, "TMPDIR="+filepath.Join(folder, "tmp")) {
		t.Errorf("env = %v", c.Env)
	}
	if text := f.logText(id); !strings.Contains(text, "[harness] sandboxed (fake-driver)\n") || strings.Contains(text, "not sandboxed") {
		t.Errorf("log = %q", text)
	}
}

func TestServicesSandbox_AServiceWithNoRepositoryWritesItsOwnFolder(t *testing.T) {
	driver := &portstest.RecordingSandboxDriver{}
	folders := t.TempDir()
	f := newServicesFixture(t, services.Options{
		Sandbox: fakeSandboxSwitch(true), Driver: func() ports.SandboxDriver { return driver }, FolderDir: folders,
	})
	id := f.createID(map[string]any{"name": "solo", "kind": "command", "command": map[string]any{"line": "true"}})
	f.operate("start_service", id)
	f.awaitStatus(id, "exited")
	p := driver.Calls()[0].Policy
	if dir := filepath.Join(folders, id); p.WorkingDir != dir || !slices.Contains(p.Writable, dir) {
		t.Fatalf("policy = %+v", p)
	}
}

func TestServicesSandbox_SettingOffRunsUnconfinedAndSaysSo(t *testing.T) {
	driver := &portstest.RecordingSandboxDriver{}
	f := newServicesFixture(t, services.Options{Sandbox: fakeSandboxSwitch(false), Driver: func() ports.SandboxDriver { return driver }})
	id := f.command("web", "echo hi")
	_, out := f.operate("start_service", id)
	if runOf(out)["sandboxed"] != false {
		t.Fatalf("run = %v", runOf(out))
	}
	f.awaitStatus(id, "exited")
	if text := f.awaitLog(id, "hi\n"); !strings.Contains(text, "[harness] not sandboxed\n") {
		t.Fatalf("log = %q", text)
	}
	if len(driver.Calls()) != 0 {
		t.Fatalf("the driver was used with the setting off")
	}
	_, list := f.call("GET", "/api/list_services?project_id="+f.projectID, nil)
	if list["sandbox"] != "off" {
		t.Fatalf("sandbox = %v", list["sandbox"])
	}
}

func TestServicesSandbox_OnWithNoDriverRefusesStartAndRestartAndCreatesNoRun(t *testing.T) {
	f := newServicesFixture(t, services.Options{Sandbox: fakeSandboxSwitch(true), Driver: func() ports.SandboxDriver { return nil }})
	id := f.command("web", "echo should-not-run")
	for _, verb := range []string{"start_service", "restart_service"} {
		code, out := f.operate(verb, id)
		if c, _ := errorOf(out); code != 503 || c != "SANDBOX_UNAVAILABLE" {
			t.Fatalf("%s = %d %v", verb, code, out)
		}
	}
	svc := f.service(id)
	if svc["status"] != "stopped" || svc["run"] != nil {
		t.Fatalf("a refused start left %v", svc)
	}
	if _, err := os.Stat(f.svc.LogPath(id)); err == nil {
		t.Fatalf("a refused start wrote a log")
	}
	_, list := f.call("GET", "/api/list_services?project_id="+f.projectID, nil)
	if list["sandbox"] != "unavailable" {
		t.Fatalf("sandbox = %v", list["sandbox"])
	}
}

func TestServicesSandbox_ARefusedRestartLeavesTheRunningServiceRunning(t *testing.T) {
	var driver ports.SandboxDriver
	sw := &toggleSwitch{}
	f := newServicesFixture(t, services.Options{Sandbox: sw, Driver: func() ports.SandboxDriver { return driver }})
	id := f.command("web", "sleep 30")
	if code, out := f.operate("start_service", id); code != 200 {
		t.Fatalf("start = %d %v", code, out)
	}
	f.awaitStatus(id, "running")
	sw.on = true
	if code, out := f.operate("restart_service", id); code != 503 {
		t.Fatalf("restart = %d %v", code, out)
	}
	if f.status(id) != "running" {
		t.Fatalf("status = %s", f.status(id))
	}
	f.operate("stop_service", id)
}

type toggleSwitch struct{ on bool }

func (s *toggleSwitch) Enabled() bool { return s.on }

// With the real driver, a service writes where its policy says and nowhere
// else under the person's home.
func TestServicesSandbox_ConfinesAServiceUnderTheRealDriver_EndToEnd(t *testing.T) {
	driver := runtime.PlatformSandboxDriver()
	if driver == nil {
		t.Skip("this platform has no sandbox driver")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	extra := t.TempDir()
	outside, err := os.MkdirTemp(home, ".harness-services-sandbox-test-")
	if err != nil {
		t.Skipf("cannot make a directory under the home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(outside) })
	// The file the person's home would be hurt by: unique, so a stray write
	// is found and removed.
	stray := filepath.Join(outside, "stray.txt")

	folders := t.TempDir()
	f := newServicesFixture(t, services.Options{
		Sandbox: fakeSandboxSwitch(true), Driver: func() ports.SandboxDriver { return driver }, FolderDir: folders,
		Allow: func() []ports.SandboxPath { return []ports.SandboxPath{{Path: extra, Write: true}} },
	})
	// The id is unknown before create: the line names the folder through $PWD
	// for a service with no repository, and the repository's directory by path.
	id := f.createID(map[string]any{"name": "writer", "kind": "command", "command": map[string]any{"line": strings.Join([]string{
		"echo in-folder > in-dir.txt && echo WROTE-DIR",
		"echo allowed > " + shellQuote(filepath.Join(extra, "extra.txt")) + " && echo WROTE-EXTRA",
		"echo stray > " + shellQuote(stray) + " || echo STRAY-DENIED",
	}, "; ")}})

	f.operate("start_service", id)
	f.awaitStatus(id, "exited")
	text := f.logText(id)
	if strings.Contains(text, "sandbox_apply: Operation not permitted") || strings.Contains(text, "bwrap:") {
		t.Skipf("the real driver cannot apply inside this environment (run the test outside an agent sandbox): %s", text)
	}
	for _, want := range []string{"WROTE-DIR\n", "WROTE-EXTRA\n", "STRAY-DENIED\n", "Operation not permitted", "[harness] sandboxed (" + driver.Name() + ")\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("log lacks %q: %q", want, text)
		}
	}
	if _, err := os.Stat(stray); err == nil {
		t.Errorf("the service wrote under the home: %s", stray)
	}
	if b, err := os.ReadFile(filepath.Join(folders, id, "in-dir.txt")); err != nil || string(b) != "in-folder\n" {
		t.Errorf("the service did not write its folder: %q %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(extra, "extra.txt")); err != nil || string(b) != "allowed\n" {
		t.Errorf("the allowlist's writable path was not honoured: %q %v", b, err)
	}
}

// A service of a repository writes in the repository's directory, and cannot
// write its own log.
func TestServicesSandbox_ARepositoryServiceWritesItsDirAndNotItsLog_EndToEnd(t *testing.T) {
	driver := runtime.PlatformSandboxDriver()
	if driver == nil {
		t.Skip("this platform has no sandbox driver")
	}
	logs := t.TempDir()
	f := newServicesFixture(t, services.Options{Sandbox: fakeSandboxSwitch(true), Driver: func() ports.SandboxDriver { return driver }, LogDir: logs})
	id := f.command("tamper", "echo mine > made.txt && echo WROTE-DIR; for l in "+shellQuote(logs)+"/*.log; do echo for\"\"ged >> \"$l\" || echo LOG-DENIED; done")
	f.operate("start_service", id)
	f.awaitStatus(id, "exited")
	text := f.logText(id)
	if strings.Contains(text, "sandbox_apply: Operation not permitted") || strings.Contains(text, "bwrap:") {
		t.Skipf("the real driver cannot apply inside this environment (run the test outside an agent sandbox): %s", text)
	}
	if !strings.Contains(text, "WROTE-DIR\n") || !strings.Contains(text, "LOG-DENIED\n") || strings.Contains(text, "forged") {
		t.Fatalf("log = %q", text)
	}
	if _, err := os.Stat(filepath.Join(f.repoDir(), "made.txt")); err != nil {
		t.Fatalf("the service did not write its directory: %v", err)
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
