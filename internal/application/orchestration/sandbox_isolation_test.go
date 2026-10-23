package orchestration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

func isOff(t *testing.T, raw json.RawMessage) bool {
	t.Helper()
	var sb struct {
		Enabled *bool `json:"enabled"`
	}
	if raw == nil || json.Unmarshal(raw, &sb) != nil {
		return false
	}
	return sb.Enabled != nil && !*sb.Enabled
}

func TestSandboxIsolation_ClaudesOwnSandboxIsOffInsideTheAgentSandbox(t *testing.T) {
	t.Run("headless", func(t *testing.T) {
		w := newSandboxWorld(t)
		w.isolate(t)
		if _, err := w.startHeadless(t); err != nil {
			t.Fatal(err)
		}
		got := settingsOf(t, w.driver.Calls()[0].Command.Args)
		if got.Sandbox == nil || got.Sandbox.Enabled == nil || *got.Sandbox.Enabled {
			t.Errorf("headless settings %+v do not turn claude's sandbox off", got)
		}
	})
	for _, mode := range []string{"implement", "design", "", "architect", "product"} {
		t.Run("interactive "+mode, func(t *testing.T) {
			w := newSandboxWorld(t)
			req := InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", Mode: mode, RunsOn: domain.RunnerTUI}
			if mode != "product" {
				req.TicketID = "tk1"
			}
			_, spec, err := w.svc.StartInteractive(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if !isOff(t, spec.Settings["sandbox"]) {
				t.Errorf("spec settings %v do not turn claude's sandbox off", spec.Settings)
			}
		})
	}
	t.Run("not set when the agent sandbox is off", func(t *testing.T) {
		w := newSandboxWorld(t)
		w.off = true
		_, spec, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
		if err != nil {
			t.Fatal(err)
		}
		if spec.Settings != nil {
			t.Errorf("settings %v with the sandbox off", spec.Settings)
		}
	})
}

func tempWorld(t *testing.T) *sandboxWorld {
	t.Helper()
	w := newSandboxWorld(t)
	root := t.TempDir()
	w.svc.Sandbox.TempDir = func(id string) string { return filepath.Join(root, "s-"+id) }
	return w
}

func TestSandboxIsolation_EachSessionHasItsOwnTempDir(t *testing.T) {
	w := tempWorld(t)
	var specs []ports.AgentSpec
	var policies []ports.SandboxPolicy
	var ids []string
	for range 2 {
		sess, spec, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, sess.ID)
		specs = append(specs, spec)
		policies = append(policies, *spec.Sandbox)
	}
	sys := filepath.Clean(os.TempDir())
	for i, p := range policies {
		own := w.svc.Sandbox.TempDir(ids[i])
		if !slices.Contains(p.Writable, own) {
			t.Errorf("session %d: own temp dir %s not writable: %v", i, own, p.Writable)
		}
		if slices.Contains(p.Writable, sys) || slices.Contains(p.Readable, sys) {
			t.Errorf("session %d: the shared system temp dir %s is allowed: %v", i, sys, p)
		}
		other := w.svc.Sandbox.TempDir(ids[1-i])
		if hasPath(p, other) {
			t.Errorf("session %d reaches the other's temp dir %s", i, other)
		}
		for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
			var got string
			for _, e := range specs[i].Env {
				if v, ok := strings.CutPrefix(e, name+"="); ok {
					got = v
				}
			}
			if got != own {
				t.Errorf("session %d: %s=%q, want %q", i, name, got, own)
			}
		}
		fi, err := os.Stat(own)
		if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
			t.Errorf("session %d: temp dir %v %v, want an existing 0700 dir", i, fi, err)
		}
	}
}

func TestSandboxIsolation_TheTempDirGoesWhenTheSessionEnds(t *testing.T) {
	w := tempWorld(t)
	sess, _, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	dir := w.svc.Sandbox.TempDir(sess.ID)
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the temp dir removed", func() bool { _, err := os.Stat(dir); return os.IsNotExist(err) })
}

func TestSandboxIsolation_AHeadlessSessionGetsItsTempDirInItsEnv(t *testing.T) {
	w := tempWorld(t)
	w.isolate(t)
	sess, err := w.startHeadless(t)
	if err != nil {
		t.Fatal(err)
	}
	dir := w.svc.Sandbox.TempDir(sess.ID)
	call := w.driver.Calls()[0]
	if !slices.Contains(call.Command.Env, "TMPDIR="+dir) {
		t.Errorf("env lacks TMPDIR=%s", dir)
	}
	if !slices.Contains(call.Policy.Writable, dir) {
		t.Errorf("policy %v lacks %s", call.Policy.Writable, dir)
	}
}

func TestSandboxViolations_ASourceThatNeverComesUpDelaysNoStart(t *testing.T) {
	w := newViolationWorld(t)
	w.source.Block = make(chan struct{}) // never closed: the log stream never attaches
	w.isolate(t)
	start := time.Now()
	if _, err := w.startHeadless(t); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "implement", RunsOn: domain.RunnerTUI}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("starting two sessions took %v behind a source that never comes up", d)
	}
	eventually(t, "both follows attempted", func() bool { return w.source.Follows() == 2 })
}
