package agentready_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/adapter/out/releases"
	"operators-mcp/internal/application/agentready"
	"operators-mcp/internal/application/managedclaude"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const platform = "darwin-arm64"

// releaseServer is a fake claude release server for one version; the build is
// held back until release is closed, and may be broken.
type releaseServer struct {
	*httptest.Server
	mu      sync.Mutex
	hold    chan struct{}
	missing bool // the manifest 404s: the install fails
}

func newReleaseServer(t *testing.T) *releaseServer {
	t.Helper()
	body := "#!/bin/sh\necho 2.1.52\n"
	sum := sha256.Sum256([]byte(body))
	rs := &releaseServer{hold: make(chan struct{})}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		rs.mu.Lock()
		broken := rs.missing
		rs.mu.Unlock()
		switch {
		case len(parts) == 1:
			fmt.Fprint(w, "2.1.52")
		case parts[len(parts)-1] == "manifest.json" && !broken:
			fmt.Fprintf(w, `{"platforms":{%q:{"checksum":%q,"size":%d}}}`, platform, hex.EncodeToString(sum[:]), len(body))
		case parts[len(parts)-1] == "claude":
			<-rs.hold
			fmt.Fprint(w, body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		select {
		case <-rs.hold:
		default:
			close(rs.hold)
		}
		rs.Close()
	})
	return rs
}

func (rs *releaseServer) release() { close(rs.hold) }

type world struct {
	gate    *agentready.Gate
	managed *managedclaude.Service
	cred    bool
	sbOn    bool
	driver  bool
}

func newWorld(t *testing.T, rs *releaseServer) *world {
	t.Helper()
	m := managedclaude.New(managedclaude.Deps{Downloads: releases.NewClient(), Dir: filepath.Join(t.TempDir(), "claude"), BaseURL: rs.URL, Platform: platform})
	t.Cleanup(m.Close)
	w := &world{managed: m, cred: true, driver: true}
	w.gate = &agentready.Gate{
		Credential:    func() bool { return w.cred },
		Bin:           managedclaude.Resolver{Managed: m},
		Managed:       m,
		SandboxOn:     func() bool { return w.sbOn },
		SandboxDriver: func() bool { return w.driver },
	}
	return w
}

func settle(t *testing.T, m *managedclaude.Service) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if m.State().Installing == nil {
			return
		}
	}
	t.Fatal("the install never ended")
}

func details(t *testing.T, err error) []map[string]string {
	t.Helper()
	var de *domain.DetailedError
	if !errors.As(err, &de) {
		t.Fatalf("%v carries no details", err)
	}
	return de.Details.(map[string]any)["missing"].([]map[string]string)
}

func TestWithEverythingInPlaceTheStartIsNotRefused(t *testing.T) {
	rs := newReleaseServer(t)
	w := newWorld(t, rs)
	rs.release()
	if err := w.gate.Check(context.Background()); err != nil {
		// managed claude is missing, so this begins an install; install it fully first.
		settle(t, w.managed)
	}
	if err := w.gate.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	var nilGate *agentready.Gate
	if err := nilGate.Check(context.Background()); err != nil {
		t.Fatalf("a nil gate enforces nothing: %v", err)
	}
	if err := (&agentready.Gate{}).Check(context.Background()); err != nil {
		t.Fatalf("an empty gate enforces nothing: %v", err)
	}
}

func TestAMissingCredentialIsRefusedNamingItAndTheCredentialAnchor(t *testing.T) {
	rs := newReleaseServer(t)
	w := newWorld(t, rs)
	rs.release()
	w.gate.Bin = managedclaude.Resolver{Override: "/bin/claude", Managed: w.managed}
	w.cred = false
	err := w.gate.Check(context.Background())
	if errs.Code(err) != "AGENT_CREDENTIAL_MISSING" || !strings.Contains(err.Error(), "no agent credential") || !strings.Contains(err.Error(), "/agent-isolation#credential") {
		t.Fatalf("err = %v (%s)", err, errs.Code(err))
	}
}

func TestASandboxThatIsOnWithoutADriverIsRefusedAndOffIsNot(t *testing.T) {
	rs := newReleaseServer(t)
	w := newWorld(t, rs)
	rs.release()
	w.gate.Bin = managedclaude.Resolver{Override: "/bin/claude", Managed: w.managed}
	w.driver = false
	if err := w.gate.Check(context.Background()); err != nil {
		t.Fatalf("sandbox off: %v", err)
	}
	w.sbOn = true
	err := w.gate.Check(context.Background())
	if errs.Code(err) != "SANDBOX_DRIVER_MISSING" || !strings.Contains(err.Error(), "/agent-isolation#sandbox") {
		t.Fatalf("err = %v", err)
	}
}

func TestSeveralMissingThingsAreNamedInOneRefusal(t *testing.T) {
	rs := newReleaseServer(t)
	w := newWorld(t, rs)
	w.cred, w.sbOn, w.driver = false, true, false
	err := w.gate.Check(context.Background())
	var got []string
	for _, m := range details(t, err) {
		got = append(got, m["code"]+" "+m["anchor"])
	}
	want := "AGENT_CREDENTIAL_MISSING /agent-isolation#credential,MANAGED_CLAUDE_INSTALLING /agent-isolation#claude,SANDBOX_DRIVER_MISSING /agent-isolation#sandbox"
	if strings.Join(got, ",") != want {
		t.Fatalf("missing = %v", got)
	}
	for _, s := range []string{"no agent credential", "managed claude is installing", "no sandbox driver"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("%q not in %q", s, err)
		}
	}
}

func TestAMissingManagedClaudeStartsInstallingStableAndRefusesWhileItRuns(t *testing.T) {
	rs := newReleaseServer(t)
	w := newWorld(t, rs)
	ctx := context.Background()

	err := w.gate.Check(ctx) // the first start begins the install on its own
	if errs.Code(err) != "MANAGED_CLAUDE_INSTALLING" || strings.Contains(err.Error(), "no managed claude") || !strings.Contains(err.Error(), "/agent-isolation#claude") {
		t.Fatalf("first start: %v (%s)", err, errs.Code(err))
	}
	if in := w.managed.State().Installing; in == nil {
		t.Fatal("no install began")
	}

	err = w.gate.Check(ctx) // a second one during the install
	if errs.Code(err) != "MANAGED_CLAUDE_INSTALLING" {
		t.Fatalf("second start: %v", err)
	}

	rs.release()
	settle(t, w.managed)
	if err := w.gate.Check(ctx); err != nil {
		t.Fatalf("after the install: %v", err)
	}
	if !w.managed.Installed() || w.managed.State().Current != "2.1.52" {
		t.Fatalf("state = %+v", w.managed.State())
	}
}

func TestAFailedInstallIsRefusedWithItsErrorAndNotRetriedBehindThePersonsBack(t *testing.T) {
	rs := newReleaseServer(t)
	rs.missing = true
	w := newWorld(t, rs)
	rs.release()
	ctx := context.Background()
	_ = w.gate.Check(ctx)
	settle(t, w.managed)

	err := w.gate.Check(ctx)
	if errs.Code(err) != "MANAGED_CLAUDE_INSTALL_FAILED" || !strings.Contains(err.Error(), "does not exist") || !strings.Contains(err.Error(), "/agent-isolation#claude") {
		t.Fatalf("err = %v (%s)", err, errs.Code(err))
	}
	if w.managed.State().Installing != nil {
		t.Fatal("a failed install was retried without the person")
	}
}

func TestAnOverriddenClaudeIsNeverInstalled(t *testing.T) {
	rs := newReleaseServer(t)
	w := newWorld(t, rs)
	w.gate.Bin = managedclaude.Resolver{Override: "/opt/claude", Managed: w.managed}
	if err := w.gate.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.managed.State().Installing != nil {
		t.Fatal("installed although the person set -claude.bin")
	}
}

var _ ports.ManagedClaude = (*managedclaude.Service)(nil)

// Peek is what a page render asks: it names what is missing like Check, but
// never starts anything.
func TestPeekReportsAMissingManagedClaudeWithoutStartingAnInstall(t *testing.T) {
	rs := newReleaseServer(t)
	w := newWorld(t, rs)
	ctx := context.Background()

	for range 2 {
		err := w.gate.Peek(ctx)
		if errs.Code(err) != "MANAGED_CLAUDE_MISSING" || !strings.Contains(err.Error(), "/agent-isolation#claude") {
			t.Fatalf("peek = %v (%s)", err, errs.Code(err))
		}
	}
	if w.managed.State().Installing != nil {
		t.Fatal("peek began an install")
	}
}

func TestPeekReportsARunningInstallAsUnavailableAndOtherGapsLikeCheck(t *testing.T) {
	rs := newReleaseServer(t)
	w := newWorld(t, rs)
	ctx := context.Background()
	_ = w.gate.Check(ctx) // starts the install
	if err := w.gate.Peek(ctx); errs.Code(err) != "MANAGED_CLAUDE_INSTALLING" {
		t.Fatalf("peek during the install = %v", err)
	}
	rs.release()
	settle(t, w.managed)
	if err := w.gate.Peek(ctx); err != nil {
		t.Fatalf("peek when ready = %v", err)
	}
	w.cred = false
	if got := details(t, w.gate.Peek(ctx)); len(got) != 1 || got[0]["code"] != agentready.CodeCredentialMissing {
		t.Fatalf("peek without a credential = %v", got)
	}
	var nilGate *agentready.Gate
	if nilGate.Peek(ctx) != nil {
		t.Fatal("a nil gate enforces nothing")
	}
}
