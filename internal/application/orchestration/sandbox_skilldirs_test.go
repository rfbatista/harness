package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// readOnlyIn reports whether the policy grants dir for reading and not for
// writing.
func readOnlyIn(p ports.SandboxPolicy, dir string) (granted, writable bool) {
	return slices.Contains(p.Readable, dir), slices.Contains(p.Writable, dir)
}

// skilledModes are the modes that bring skills of their own (ADR 0028).
var skilledModes = []string{"architect", "implement", "design", "product"}

func TestSandboxSkillDirs_AnInteractiveSessionReadsItsSkillDirsReadOnly(t *testing.T) {
	for _, mode := range skilledModes {
		t.Run("mode "+mode, func(t *testing.T) {
			w := newSandboxWorld(t)
			req := InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", Mode: mode, RunsOn: domain.RunnerTUI}
			if mode != "product" {
				req.TicketID = "tk1"
			}
			_, spec, err := w.svc.StartInteractive(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if len(spec.SkillDirs) == 0 {
				t.Fatalf("mode %s has no skill dirs", mode)
			}
			p := w.driver.Calls()[0].Policy
			for _, dir := range spec.SkillDirs {
				if granted, writable := readOnlyIn(p, dir); !granted || writable {
					t.Errorf("skill dir %s: read-only granted=%v writable=%v, want granted and not writable", dir, granted, writable)
				}
			}
		})
	}
}

func TestSandboxSkillDirs_AResumedSessionReadsItsSkillDirsReadOnly(t *testing.T) {
	w := newSandboxWorld(t)
	sess, _, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "architect", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.svc.sessions.UpdateStatus(sess.ID, domain.SessionDone); err != nil {
		t.Fatal(err)
	}
	_, spec, err := w.svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.SkillDirs) == 0 {
		t.Fatal("resumed session has no skill dirs")
	}
	calls := w.driver.Calls()
	p := calls[len(calls)-1].Policy
	for _, dir := range spec.SkillDirs {
		if granted, writable := readOnlyIn(p, dir); !granted || writable {
			t.Errorf("skill dir %s: read-only granted=%v writable=%v, want granted and not writable", dir, granted, writable)
		}
	}
}

func TestSandboxSkillDirs_AHeadlessSessionReadsItsSkillDirsReadOnly(t *testing.T) {
	for _, mode := range append(skilledModes, "") {
		t.Run("mode "+mode, func(t *testing.T) {
			w := newSandboxWorld(t)
			w.isolate(t)
			in := prepareInput{ProjectID: "p1", RepositoryID: "r1", Branch: "agent/skills", Mode: domain.SessionMode(mode), Headless: true}
			p, err := w.svc.prepare(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if p.cleanup != nil {
					p.cleanup()
				}
			})
			if _, err := p.cfg.Wrap(llmkit.Command{Path: "claude"}); err != nil {
				t.Fatal(err)
			}
			policy := w.driver.Calls()[0].Policy
			dirs := p.cfg.Driver.(claude.Config).SkillDirs
			if mode == "" && len(dirs) != 0 {
				t.Fatalf("default mode has skill dirs %v", dirs)
			}
			if mode != "" && len(dirs) == 0 {
				t.Fatalf("mode %s has no skill dirs", mode)
			}
			for _, dir := range dirs {
				if granted, writable := readOnlyIn(policy, dir); !granted || writable {
					t.Errorf("skill dir %s: read-only granted=%v writable=%v, want granted and not writable", dir, granted, writable)
				}
			}
		})
	}
}

func TestSandboxSkillDirs_TheResolvedPathIsGrantedWhenItDiffers(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	got := skillDirGrants([]string{link, filepath.Join(real, "gone")})
	want := []ports.SandboxPath{{Path: link}, {Path: resolved}, {Path: filepath.Join(real, "gone")}}
	if !slices.Equal(got, want) {
		t.Errorf("grants %v, want %v (no resolved path for a dir that is gone)", got, want)
	}
	for _, g := range got {
		if g.Write {
			t.Errorf("%s granted writable", g.Path)
		}
	}
}

func TestSandboxSkillDirs_ASessionWithoutSkillsAndAnUnsandboxedOneAreUnchanged(t *testing.T) {
	w := newSandboxWorld(t)
	w.svc.Sandbox.TempDir = nil
	sess, spec, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", Mode: "", TicketID: "tk1", RunsOn: domain.RunnerTUI})
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.SkillDirs) != 0 {
		t.Fatalf("default mode has skill dirs %v", spec.SkillDirs)
	}
	for _, r := range w.driver.Calls()[0].Policy.Readable {
		if strings.Contains(r, "harnesskit-skills") {
			t.Errorf("session %s without skills reads %s", sess.ID, r)
		}
	}

	w = newSandboxWorld(t)
	w.off = true
	if _, _, err := w.svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1", Mode: "architect", RunsOn: domain.RunnerTUI}); err != nil {
		t.Fatal(err)
	}
	if n := len(w.driver.Calls()); n != 0 {
		t.Errorf("driver called %d times with the sandbox off", n)
	}
}
