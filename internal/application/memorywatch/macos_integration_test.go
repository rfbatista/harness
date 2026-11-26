//go:build darwin

package memorywatch_test

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/psprocs"
	"operators-mcp/internal/application/memorywatch"
	"operators-mcp/internal/ports"
)

// A real child allocates past a small orçamento under a stand-in agent CLI:
// the vigia, on the real process table, ends the child and the agent survives.
func TestRealProcessPastASmallOrcamentoIsEndedAndTheAgentSurvives(t *testing.T) {
	const id = "itg-memory-watch-1"
	if _, err := (psprocs.Table{}).Read(context.Background()); err != nil {
		t.Skipf("the process table cannot be read here (an agent sandbox forbids ps): %v", err)
	}
	agent := exec.Command("sh", "-c", "perl -e '$x = q(a) x 300000000; sleep 120' & wait", "--session-id", id)
	agent.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = agent.Wait(); close(exited) }()
	t.Cleanup(func() { _ = syscall.Kill(-agent.Process.Pid, syscall.SIGKILL) })

	sink := &sink{}
	w := memorywatch.New(memorywatch.Deps{Table: psprocs.Table{}, Signal: psprocs.Signaler{}, Sink: sink})
	w.Watch(ports.MemoryWatch{SessionID: "s1", TaskID: "t1", BudgetBytes: 100 << 20, ClaudeID: id})

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		w.Poll(time.Now())
		sink.mu.Lock()
		n := len(sink.kills)
		sink.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(time.Second)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.kills) != 1 {
		t.Fatalf("kills = %+v, want one", sink.kills)
	}
	if k := sink.kills[0]; k.Command[:4] != "perl" || k.RSSBytes < 200<<20 || k.Signal != "SIGTERM" {
		t.Errorf("kill = %+v, want the perl child ended with SIGTERM", k)
	}
	select {
	case <-exited:
		t.Error("the agent stand-in exited with its child")
	case <-time.After(500 * time.Millisecond):
	}
}
