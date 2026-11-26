//go:build darwin

package memorywatch_test

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/procmac"
	"operators-mcp/internal/adapter/out/psprocs"
	"operators-mcp/internal/application/memorywatch"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The real process table, end to end: a wrapper leads its own POSIX session
// (as a PTY's process does) and runs a stand-in agent CLI, which runs a perl
// child that holds about 300 MB. Over a 100 MB orçamento the vigia ends the
// perl child, and the stand-in agent and the wrapper above it live on.
func TestRealProcessPastASmallOrcamentoIsEndedAndTheAgentSurvives(t *testing.T) {
	const id = "itg-memory-watch-1"
	if _, err := (procmac.Table{}).Read(context.Background()); err != nil {
		t.Skipf("the process table cannot be read here (the kernel refused the sysctl): %v", err)
	}
	// The agent outlives its child (sleep 120 after wait), as claude outlives a test run.
	agentScript := `perl -e '$x = q(a) x 300000000; sleep 120' & wait; sleep 120`
	wrapper := exec.Command("sh", "-c", `sh -c "$0" --session-id `+id+`; sleep 120`, agentScript)
	wrapper.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := wrapper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-wrapper.Process.Pid, syscall.SIGKILL)
		_ = wrapper.Wait()
	})

	sink := &sink{}
	w := memorywatch.New(memorywatch.Deps{Table: procmac.Table{}, Signal: psprocs.Signaler{}, Sink: sink})
	w.Watch(ports.MemoryWatch{SessionID: "s1", TaskID: "t1", BudgetBytes: 100 << 20, ClaudeID: id, RootPID: wrapper.Process.Pid})

	deadline := time.Now().Add(30 * time.Second)
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
	kills := append([]domain.MemoryKill(nil), sink.kills...)
	sink.mu.Unlock()
	if len(kills) != 1 {
		t.Fatalf("kills = %+v, want one", kills)
	}
	if k := kills[0]; len(k.Command) < 4 || k.Command[:4] != "perl" || k.RSSBytes < 200<<20 || k.Signal != "SIGTERM" {
		t.Errorf("kill = %+v, want the perl child ended with SIGTERM", k)
	}

	table, err := (procmac.Table{}).Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var agentAlive bool
	for _, p := range table {
		if p.Session == id {
			agentAlive = true
		}
	}
	if !agentAlive {
		t.Error("the stand-in agent CLI is gone")
	}
	if err := syscall.Kill(wrapper.Process.Pid, 0); err != nil {
		t.Errorf("the wrapper above the agent is gone: %v", err)
	}
}
