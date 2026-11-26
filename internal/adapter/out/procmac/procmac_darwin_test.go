//go:build darwin

package procmac

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/unix"

	"operators-mcp/internal/ports"
)

func find(rows []ports.ProcessInfo, pid int) (ports.ProcessInfo, bool) {
	for _, r := range rows {
		if r.PID == pid {
			return r, true
		}
	}
	return ports.ProcessInfo{}, false
}

func spawn(t *testing.T, name string, args ...string) *exec.Cmd {
	t.Helper()
	c := exec.Command(name, args...)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
	return c
}

func TestTheTestsOwnProcessIsListedWithItsParentAndItsPosixSession(t *testing.T) {
	rows, err := Table{}.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	me, ok := find(rows, os.Getpid())
	if !ok {
		t.Fatalf("own pid %d not in %d rows", os.Getpid(), len(rows))
	}
	sid, _ := unix.Getsid(os.Getpid())
	if me.PPID != os.Getppid() || me.SID != sid || me.SID == 0 {
		t.Errorf("own row = %+v, want ppid %d sid %d", me, os.Getppid(), sid)
	}
	if me.RSSBytes < 1<<20 || me.Name == "" || me.Started == "" {
		t.Errorf("own row = %+v", me)
	}
}

func TestAChildHoldingAbout50MBIsInASensibleRSSRangeAndItsStartTimeIsStable(t *testing.T) {
	child := spawn(t, "perl", "-e", `my $x = q(a) x 50_000_000; sleep 30`)
	var first ports.ProcessInfo
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := Table{}.Read(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if r, ok := find(rows, child.Process.Pid); ok && r.RSSBytes > 50<<20 {
			first = r
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if first.PID == 0 {
		t.Fatal("the child never showed more than 50 MB")
	}
	if first.RSSBytes > 200<<20 {
		t.Errorf("rss = %d MB, want under 200", first.RSSBytes>>20)
	}
	if first.Name != "perl" || first.PPID != os.Getpid() {
		t.Errorf("row = %+v", first)
	}
	rows, _ := Table{}.Read(context.Background())
	if again, _ := find(rows, child.Process.Pid); again.Started != first.Started {
		t.Errorf("start time moved: %q then %q", first.Started, again.Started)
	}
}

func TestTheConversationAndTheFirstArgumentComeFromTheArgumentList(t *testing.T) {
	child := spawn(t, "perl", "-e", "sleep 30", "x", "--session-id", "conv-7")
	rows, err := Table{}.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r, ok := find(rows, child.Process.Pid)
	if !ok {
		t.Fatal("child not listed")
	}
	if r.Session != "conv-7" || r.Arg != "-e" || r.Name != "perl" {
		t.Errorf("row = %+v", r)
	}
}

func TestAnAlreadyCancelledContextReturnsItsErrorAtOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := Table{}.Read(ctx)
	if !errors.Is(err, context.Canceled) || time.Since(start) > 500*time.Millisecond {
		t.Errorf("err = %v after %v", err, time.Since(start))
	}
}

// fakeSource lets a test make a process vanish or belong to someone else.
type fakeSource struct {
	procs []unix.KinfoProc
	gone  map[int]bool
}

func (f fakeSource) list() ([]unix.KinfoProc, error) { return f.procs, nil }
func (f fakeSource) rss(pid int) (int64, error) {
	if f.gone[pid] {
		return 0, syscall.ESRCH
	}
	return 1 << 20, nil
}
func (f fakeSource) sid(pid int) (int, error) { return 5, nil }
func (f fakeSource) args(pid int) ([]byte, error) {
	if f.gone[pid] {
		return nil, syscall.EPERM
	}
	return nil, syscall.EINVAL
}

func kinfo(pid int32) unix.KinfoProc {
	var k unix.KinfoProc
	k.Proc.P_pid = pid
	copy((*[16]byte)(unsafe.Pointer(&k.Proc.P_comm))[:], "proc")
	return k
}

func TestAProcessThatExitedOrBelongsToAnotherUserIsSkippedAndTheSampleStillSucceeds(t *testing.T) {
	src := fakeSource{procs: []unix.KinfoProc{kinfo(10), kinfo(11), kinfo(12)}, gone: map[int]bool{11: true}}
	rows, err := read(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].PID != 10 || rows[1].PID != 12 {
		t.Errorf("rows = %+v", rows)
	}
}

func TestTheTaskInfoLayoutIs96Bytes(t *testing.T) {
	if n := unsafe.Sizeof(taskInfo{}); n != 96 {
		t.Fatalf("proc_taskinfo is %d bytes, the kernel's is 96", n)
	}
}

func TestParseProcArgsSkipsTheExecPathAndPaddingAndStopsAtArgc(t *testing.T) {
	buf := []byte("\x03\x00\x00\x00/usr/bin/node\x00\x00\x00node\x00/repo/jest\x00--runInBand\x00HOME=/secret\x00")
	args := parseProcArgs(buf)
	if strings.Join(args, "|") != "node|/repo/jest|--runInBand" {
		t.Errorf("args = %q", args)
	}
	if parseProcArgs([]byte("\x01")) != nil || parseProcArgs(nil) != nil {
		t.Error("a short buffer must give no arguments")
	}
}

func TestTheFirstArgumentIsCutAt80BytesOnARuneBoundary(t *testing.T) {
	got := firstArg([]string{"x", strings.Repeat("é", 100)})
	if len(got) > 80 || !utf8.ValidString(got) || len(got) < 78 {
		t.Errorf("len %d valid %v", len(got), utf8.ValidString(got))
	}
	if firstArg([]string{"x"}) != "" {
		t.Error("no second argument must give none")
	}
}

func TestAReparentedBackgroundProcessKeepsTheSessionOfItsLeader(t *testing.T) {
	leader := exec.Command("sh", "-c", `(perl -e 'sleep 30' x reparented &) ; sleep 30`)
	leader.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := leader.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-leader.Process.Pid, syscall.SIGKILL); _ = leader.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := Table{}.Read(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if r.Name == "perl" && r.Arg == "-e" && r.SID == leader.Process.Pid {
				if r.PPID == leader.Process.Pid {
					t.Fatalf("not reparented: %+v", r)
				}
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the reparented process never showed in the leader's session")
}

func BenchmarkReadTheWholeTable(b *testing.B) {
	for i := 0; i < b.N; i++ {
		rows, err := Table{}.Read(context.Background())
		if err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(len(rows)), "rows")
	}
}
