//go:build darwin

package memorywatch_test

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/psprocs"
	"operators-mcp/internal/application/memorywatch"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// realTable and realSignaler are the only places this test names an adapter:
// pointing it at another darwin adapter is a change to these two functions.
func realTable() ports.ProcessTable { return psprocs.Table{} }

func realSignaler() ports.ProcessSignaler { return psprocs.Signaler{} }

// The real process table, end to end: a wrapper leads its own POSIX session
// (as a PTY's process does) and runs a stand-in agent CLI, which runs a perl
// two perl children of about 300 MB each, one reparented to launchd. Over a
// 100 MB orçamento the vigia ends both, and the stand-in agent and the wrapper
// above it live on.
func TestRealProcessPastASmallOrcamentoIsEndedAndTheAgentSurvives(t *testing.T) {
	const id = "itg-memory-watch-1"
	if _, err := realTable().Read(context.Background()); err != nil {
		t.Skipf("the process table cannot be read here (an agent sandbox forbids ps): %v", err)
	}
	// The agent outlives its children (sleep 120 after wait), as claude outlives
	// a test run. One perl is a plain child; the other is started from a
	// subshell that exits at once, so launchd adopts it (a daemonized dev server).
	pidFile := t.TempDir() + "/orphan.pid"
	agentScript := `perl -e '$x = q(a) x 300000000; sleep 120' & (perl -e '$y = q(b) x 300000000; sleep 120' & echo $! > ` + pidFile + `); wait; sleep 120`
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
	w := memorywatch.New(memorywatch.Deps{Table: realTable(), Signal: realSignaler(), Sink: sink})
	w.Watch(ports.MemoryWatch{SessionID: "s1", TaskID: "t1", BudgetBytes: 100 << 20, ClaudeID: id, RootPID: wrapper.Process.Pid})

	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		w.Poll(time.Now())
		sink.mu.Lock()
		n := len(sink.kills)
		sink.mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(time.Second)
	}
	sink.mu.Lock()
	kills := append([]domain.MemoryKill(nil), sink.kills...)
	sink.mu.Unlock()
	if len(kills) != 2 {
		t.Fatalf("kills = %+v, want two: the plain child and the reparented one", kills)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	orphan, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	var orphanEnded bool
	for _, k := range kills {
		if len(k.Command) < 4 || k.Command[:4] != "perl" || k.RSSBytes < 200<<20 || k.Signal != "SIGTERM" {
			t.Errorf("kill = %+v, want a perl child ended with SIGTERM", k)
		}
		orphanEnded = orphanEnded || k.PID == orphan
	}
	if !orphanEnded {
		t.Errorf("the reparented perl (pid %d) was not counted and ended: %+v", orphan, kills)
	}

	table, err := realTable().Read(context.Background())
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
