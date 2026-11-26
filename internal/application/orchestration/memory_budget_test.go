//go:build !windows

package orchestration

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const gb = int64(1) << 30

func setOverride(t *testing.T, w *saverWorld, level domain.ActivationLevel, id string, bytes int64) {
	t.Helper()
	if err := w.activations.Put(domain.Activation{Saver: domain.MemoryBudgetSaver, Owner: domain.ActivationOwner{Level: level, ID: id}, ProjectID: "p1", Value: domain.ActivationValue(strconv.FormatInt(bytes, 10))}); err != nil {
		t.Fatal(err)
	}
}

func setGlobalBudget(t *testing.T, w *saverWorld, value string) {
	t.Helper()
	if _, err := w.settings.UpdateSettings(map[string]string{domain.SettingMemoryBudget: value}); err != nil {
		t.Fatal(err)
	}
}

// The orçamento de memória da sessão is the global 4 GB when nothing overrides it.
func TestASessionsOrcamentoDeMemoriaIsTheGlobalDefaultWhenNothingOverridesIt(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	withTokenSavers(t, svc)

	sess, _ := startHeadless(t, svc, w.task.ID)
	if sess.MemoryBudgetBytes != 4*gb {
		t.Errorf("orçamento = %d, want 4 GB", sess.MemoryBudgetBytes)
	}
}

// Task override, else repository override, else the global value.
func TestASessionsOrcamentoDeMemoriaFollowsTaskThenRepositoryThenGlobal(t *testing.T) {
	cases := []struct {
		name             string
		global           string
		repository, task int64
		want             int64
	}{
		{"global setting", "6442450944", 0, 0, 6 * gb},
		{"repository beats global", "6442450944", 8 * gb, 0, 8 * gb},
		{"task beats repository", "6442450944", 8 * gb, 2 * gb, 2 * gb},
		{"invalid global falls back to 4 GB", "lots", 0, 0, 4 * gb},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, w := newHeadlessShellService(t)
			savers := withTokenSavers(t, svc)
			// An invalid global bypasses validation, as a value stored by an older build would.
			if err := savers.settingsRepo.Set(domain.SettingMemoryBudget, c.global); err != nil {
				t.Fatal(err)
			}
			if c.repository != 0 {
				setOverride(t, savers, domain.ActivationLevelRepository, "r1", c.repository)
			}
			if c.task != 0 {
				setOverride(t, savers, domain.ActivationLevelTask, w.task.ID, c.task)
			}
			sess, _ := startHeadless(t, svc, w.task.ID)
			if sess.MemoryBudgetBytes != c.want {
				t.Errorf("orçamento = %d, want %d", sess.MemoryBudgetBytes, c.want)
			}
		})
	}
}

// The orçamento is fixed when the session starts and re-resolved when it resumes.
func TestAnInteractiveSessionKeepsItsOrcamentoUntilItResumes(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.SessionShellURL = func(id string) string { return testShellBase + id + "/shell" }
	w := withShells(t, svc)
	savers := withTokenSavers(t, svc)
	ctx := context.Background()

	sess, _ := startInteractive(t, svc, InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: w.task.ID})
	if sess.MemoryBudgetBytes != 4*gb {
		t.Fatalf("orçamento at start = %d, want 4 GB", sess.MemoryBudgetBytes)
	}
	setGlobalBudget(t, savers, "8589934592")
	got, err := svc.Get(ctx, sess.ID)
	if err != nil || got.MemoryBudgetBytes != 4*gb {
		t.Errorf("running session's orçamento = %d (%v), want 4 GB still", got.MemoryBudgetBytes, err)
	}
	if _, err := svc.EndInteractive(ctx, sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	resumed, _, err := svc.ResumeInteractive(ctx, ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = svc.EndInteractive(ctx, sess.ID, 0, false) })
	if resumed.MemoryBudgetBytes != 8*gb {
		t.Errorf("resumed orçamento = %d, want 8 GB", resumed.MemoryBudgetBytes)
	}
}

// fakeWatcher records the sessions handed to the vigia and which are stopped.
type fakeWatcher struct {
	mu      sync.Mutex
	watched []ports.MemoryWatch
	stopped int
}

func (f *fakeWatcher) Watch(m ports.MemoryWatch) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watched = append(f.watched, m)
	return func() { f.mu.Lock(); f.stopped++; f.mu.Unlock() }
}

// A running session is watched with the orçamento it started with, and its end stops the watch.
func TestASessionIsWatchedWithItsOrcamentoAndItsEndStopsTheWatch(t *testing.T) {
	svc, w := newHeadlessShellService(t)
	withTokenSavers(t, svc)
	fw := &fakeWatcher{}
	svc.MemoryWatcher = fw

	sess, _ := startHeadless(t, svc, w.task.ID)
	fw.mu.Lock()
	if len(fw.watched) != 1 || fw.watched[0].SessionID != sess.ID || fw.watched[0].BudgetBytes != 4*gb || fw.watched[0].ClaudeID != sess.ID || fw.watched[0].RootPID != 0 {
		t.Errorf("watched = %+v, want the headless session at 4 GB, by descendancy", fw.watched)
	}
	fw.mu.Unlock()

	if err := svc.Stop(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		fw.mu.Lock()
		n := fw.stopped
		fw.mu.Unlock()
		if n == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("stopping the session did not stop its watch")
}
