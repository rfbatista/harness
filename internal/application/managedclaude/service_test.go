package managedclaude_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/releases"
	"operators-mcp/internal/application/managedclaude"
	"operators-mcp/internal/ports"
)

const platform = "darwin-arm64"

// releaseServer is a fake claude release server: /latest, /stable, then
// /<version>/manifest.json and /<version>/<platform>/claude.
type releaseServer struct {
	*httptest.Server
	mu       sync.Mutex
	builds   map[string]string // version -> script body
	badSum   map[string]bool
	latest   string
	hold     chan struct{} // when set, a build is sent in two halves with this between
	halfSent chan struct{}
}

func newReleaseServer(t *testing.T, versions ...string) *releaseServer {
	t.Helper()
	rs := &releaseServer{builds: map[string]string{}, badSum: map[string]bool{}}
	for _, v := range versions {
		rs.builds[v] = "#!/bin/sh\necho " + v + "\n"
		rs.latest = v
	}
	rs.Server = httptest.NewServer(http.HandlerFunc(rs.serve))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *releaseServer) serve(w http.ResponseWriter, r *http.Request) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case len(parts) == 1 && (parts[0] == "latest" || parts[0] == "stable"):
		fmt.Fprint(w, rs.latest)
	case len(parts) == 2 && parts[1] == "manifest.json":
		body, ok := rs.builds[parts[0]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		sum := sha256.Sum256([]byte(body))
		digest := hex.EncodeToString(sum[:])
		if rs.badSum[parts[0]] {
			digest = strings.Repeat("0", 64)
		}
		fmt.Fprintf(w, `{"version":%q,"platforms":{%q:{"checksum":%q,"size":%d}}}`, parts[0], platform, digest, len(body))
	case len(parts) == 3 && parts[1] == platform && parts[2] == "claude":
		body, ok := rs.builds[parts[0]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if rs.hold == nil {
			fmt.Fprint(w, body)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		half := len(body) / 2
		fmt.Fprint(w, body[:half])
		w.(http.Flusher).Flush()
		close(rs.halfSent)
		rs.mu.Unlock()
		<-rs.hold
		rs.mu.Lock()
		fmt.Fprint(w, body[half:])
	default:
		http.NotFound(w, r)
	}
}

func newService(t *testing.T, rs *releaseServer) *managedclaude.Service {
	t.Helper()
	s := managedclaude.New(managedclaude.Deps{Downloads: releases.NewClient(), Dir: filepath.Join(t.TempDir(), "claude"), BaseURL: rs.URL, Platform: platform})
	t.Cleanup(s.Close)
	return s
}

// settle waits for the running install to end.
func settle(t *testing.T, s *managedclaude.Service) ports.ManagedClaudeState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st := s.State(); st.Installing == nil {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the install never ended")
	return ports.ManagedClaudeState{}
}

func install(t *testing.T, s *managedclaude.Service, version string) ports.ManagedClaudeState {
	t.Helper()
	if _, err := s.Install(context.Background(), version); err != nil {
		t.Fatal(err)
	}
	return settle(t, s)
}

// run executes the managed current path and answers what it prints.
func run(t *testing.T, s *managedclaude.Service) string {
	t.Helper()
	out, err := exec.Command(s.Path()).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestTheFirstInstallDownloadsVerifiesAndBecomesTheCurrentClaude(t *testing.T) {
	rs := newReleaseServer(t, "2.1.52")
	s := newService(t, rs)
	if s.Installed() {
		t.Fatal("installed before any install")
	}
	st := install(t, s, "latest")
	if st.Failed != nil {
		t.Fatalf("install failed: %+v", st.Failed)
	}
	if st.Current != "2.1.52" || len(st.Installed) != 1 || !st.Installed[0].Active {
		t.Fatalf("state = %+v", st)
	}
	if !s.Installed() || run(t, s) != "2.1.52" {
		t.Fatal("the current path does not run the installed version")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(s.Path()), "versions", "2.1.52", "claude")); err != nil {
		t.Fatalf("the version is not under versions/: %v", err)
	}
}

func TestAChecksumMismatchInstallsNothingAndReportsIt(t *testing.T) {
	rs := newReleaseServer(t, "2.1.40")
	rs.badSum["2.1.40"] = true
	s := newService(t, rs)
	st := install(t, s, "2.1.40")
	if st.Failed == nil || st.Failed.Version != "2.1.40" || !strings.Contains(st.Failed.Message, "checksum") {
		t.Fatalf("failed = %+v", st.Failed)
	}
	if len(st.Installed) != 0 || st.Current != "" || s.Installed() {
		t.Fatalf("a bad download was installed: %+v", st)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(s.Path()), "versions", "*")); len(left) != 0 {
		t.Fatalf("files left behind: %v", left)
	}
}

func TestInstallingAnotherVersionKeepsTheCurrentOneUntilTheSwitch(t *testing.T) {
	rs := newReleaseServer(t, "2.1.47", "2.1.52")
	s := newService(t, rs)
	install(t, s, "2.1.47")
	st := install(t, s, "2.1.52")
	if st.Current != "2.1.47" || len(st.Installed) != 2 {
		t.Fatalf("state = %+v", st)
	}
	if run(t, s) != "2.1.47" {
		t.Fatal("installing changed the current version")
	}

	st, err := s.Switch("2.1.52")
	if err != nil {
		t.Fatal(err)
	}
	if st.Current != "2.1.52" || run(t, s) != "2.1.52" {
		t.Fatalf("after the switch current = %q, runs %q", st.Current, run(t, s))
	}
	if len(st.Installed) != 1 || st.Installed[0].Version != "2.1.52" {
		t.Fatalf("the old version was not removed: %+v", st.Installed)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(s.Path()), "versions", "2.1.47")); !os.IsNotExist(err) {
		t.Fatalf("the old version's files remain: %v", err)
	}
}

func TestSwitchingToAVersionThatIsNotInstalledIsRefused(t *testing.T) {
	rs := newReleaseServer(t, "2.1.47")
	s := newService(t, rs)
	install(t, s, "2.1.47")
	if _, err := s.Switch("9.9.9"); err == nil || !strings.Contains(err.Error(), "MANAGED_CLAUDE_VERSION_NOT_FOUND") {
		t.Fatalf("err = %v", err)
	}
	if run(t, s) != "2.1.47" {
		t.Fatal("a refused switch changed the current version")
	}
}

func TestAnInstallReportsItsProgressWhileDownloading(t *testing.T) {
	rs := newReleaseServer(t, "2.1.52")
	rs.hold, rs.halfSent = make(chan struct{}), make(chan struct{})
	s := newService(t, rs)
	if _, err := s.Install(context.Background(), "2.1.52"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rs.halfSent:
	case <-time.After(5 * time.Second):
		t.Fatal("the download never started")
	}
	var in *ports.ManagedClaudeInstall
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if in = s.State().Installing; in != nil && in.Done > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if in == nil || in.Version != "2.1.52" || in.Done <= 0 || in.Total != int64(len(rs.builds["2.1.52"])) || in.Done >= in.Total {
		t.Fatalf("progress = %+v", in)
	}
	if _, err := s.Install(context.Background(), "2.1.40"); err == nil || !strings.Contains(err.Error(), "MANAGED_CLAUDE_BUSY") {
		t.Fatalf("a second install while one runs: %v", err)
	}
	close(rs.hold)
	if st := settle(t, s); st.Current != "2.1.52" {
		t.Fatalf("state = %+v", st)
	}
}

func TestReleasesNameTheLatestVersion(t *testing.T) {
	rs := newReleaseServer(t, "2.1.60")
	s := newService(t, rs)
	got, err := s.Releases(context.Background())
	if err != nil || got.Latest != "2.1.60" {
		t.Fatalf("releases = %+v, %v", got, err)
	}
}

func TestAVersionThatDoesNotExistFailsTheInstall(t *testing.T) {
	rs := newReleaseServer(t, "2.1.52")
	s := newService(t, rs)
	st := install(t, s, "1.0.0")
	if st.Failed == nil || !strings.Contains(st.Failed.Message, "does not exist") {
		t.Fatalf("failed = %+v", st.Failed)
	}
}

func TestOnlyVersionsAndChannelsCanBeInstalled(t *testing.T) {
	s := newService(t, newReleaseServer(t))
	for _, v := range []string{"", "../x", "2.1", "1.2.3/../../x"} {
		if _, err := s.Install(context.Background(), v); err == nil || !strings.Contains(err.Error(), "INVALID_INPUT") {
			t.Errorf("Install(%q) err = %v", v, err)
		}
	}
}

func TestTheOverrideWinsOverTheManagedClaude(t *testing.T) {
	rs := newReleaseServer(t, "2.1.52")
	s := newService(t, rs)
	for _, tc := range []struct {
		override   string
		wantBin    string
		overridden bool
	}{{"", s.Path(), false}, {"claude", s.Path(), false}, {"/opt/dev/claude", "/opt/dev/claude", true}} {
		r := managedclaude.Resolver{Override: tc.override, Managed: s}
		if r.Bin() != tc.wantBin || r.Overridden() != tc.overridden {
			t.Errorf("override %q: bin %q overridden %v", tc.override, r.Bin(), r.Overridden())
		}
	}
	if (managedclaude.Resolver{Managed: s}).Installed() {
		t.Error("managed claude reads as installed before an install")
	}
	if !(managedclaude.Resolver{Override: "/opt/dev/claude", Managed: s}).Installed() {
		t.Error("an override should count as installed")
	}
	install(t, s, "latest")
	if !(managedclaude.Resolver{Managed: s}).Installed() {
		t.Error("not installed after the install")
	}
}

func TestCancellingARunningInstallLeavesNothingBehind(t *testing.T) {
	rs := newReleaseServer(t, "2.1.52")
	rs.hold, rs.halfSent = make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(rs.hold) })
	s := newService(t, rs)
	if _, err := s.Install(context.Background(), "2.1.52"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rs.halfSent:
	case <-time.After(5 * time.Second):
		t.Fatal("the download never started")
	}
	st, err := s.Cancel()
	if err != nil {
		t.Fatal(err)
	}
	if st.Installing != nil || st.Failed != nil || len(st.Installed) != 0 || st.Current != "" {
		t.Fatalf("after Cancel: %+v", st)
	}
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(s.Path()), "versions", "*"))
	if len(left) != 0 {
		t.Fatalf("a cancelled install left %v", left)
	}
	if _, err := s.Install(context.Background(), "2.1.52"); err != nil {
		t.Fatalf("a new install after Cancel: %v", err)
	}
}

func TestCancelWithNoInstallRunningChangesNothing(t *testing.T) {
	rs := newReleaseServer(t, "2.1.47")
	s := newService(t, rs)
	install(t, s, "2.1.47")
	st, err := s.Cancel()
	if err != nil || st.Current != "2.1.47" || st.Failed != nil {
		t.Fatalf("Cancel = %+v, %v", st, err)
	}
}

func TestANonCurrentVersionCanBeRemovedAndTheCurrentOneCannot(t *testing.T) {
	rs := newReleaseServer(t, "2.1.47", "2.1.52")
	s := newService(t, rs)
	install(t, s, "2.1.47")
	install(t, s, "2.1.52")

	if _, err := s.Remove("2.1.47"); err == nil || !strings.Contains(err.Error(), "MANAGED_CLAUDE_VERSION_CURRENT") {
		t.Fatalf("removing the current version: %v", err)
	}
	if _, err := s.Remove("9.9.9"); err == nil || !strings.Contains(err.Error(), "MANAGED_CLAUDE_VERSION_NOT_FOUND") {
		t.Fatalf("removing a version that is not installed: %v", err)
	}
	st, err := s.Remove("2.1.52")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Installed) != 1 || st.Installed[0].Version != "2.1.47" || st.Current != "2.1.47" || run(t, s) != "2.1.47" {
		t.Fatalf("after Remove: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(s.Path()), "versions", "2.1.52")); !os.IsNotExist(err) {
		t.Fatalf("the removed version's files remain: %v", err)
	}
}

func TestASwitchCountsTheRunningSessionsThatKeepTheirVersion(t *testing.T) {
	rs := newReleaseServer(t, "2.1.47", "2.1.52")
	running := 3
	s := managedclaude.New(managedclaude.Deps{Downloads: releases.NewClient(), Dir: filepath.Join(t.TempDir(), "claude"), BaseURL: rs.URL, Platform: platform,
		RunningSessions: func() int { return running }})
	t.Cleanup(s.Close)
	install(t, s, "2.1.47")
	install(t, s, "2.1.52")
	st, err := s.Switch("2.1.52")
	if err != nil {
		t.Fatal(err)
	}
	if st.KeepingSessions != 3 {
		t.Fatalf("KeepingSessions = %d, want 3", st.KeepingSessions)
	}
	if again := s.State(); again.KeepingSessions != 0 {
		t.Fatalf("a plain State carries the switch's count: %d", again.KeepingSessions)
	}
}
