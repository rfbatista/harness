//go:build darwin

// Package procmac reads the macOS process table inside the harness process,
// for the vigia de memória (ADR 0005). It forks nothing and needs no cgo: the
// list is the kern.proc.all sysctl, the session is getsid(2), the resident
// size is proc_info(PROC_PIDTASKINFO) and the arguments are kern.procargs2.
// /bin/ps is setuid root, which a sandboxed process can never exec (ADR 0010);
// these calls need neither root nor a task port, so they work in a sandbox.
package procmac

import (
	"context"
	"fmt"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/unix"

	"operators-mcp/internal/ports"
)

var _ ports.ProcessTable = Table{}

const (
	// readBound is the longest a sample may take.
	readBound = 2 * time.Second
	// argMax is the longest first argument that leaves the adapter.
	argMax = 80

	sysProcInfo        = 336 // proc_info(2)
	procInfoCallPIDInf = 2   // PROC_INFO_CALL_PIDINFO
	procPIDTaskInfo    = 4   // PROC_PIDTASKINFO
)

// taskInfo is the kernel's struct proc_taskinfo; a test pins its 96 bytes.
type taskInfo struct {
	VirtualSize, ResidentSize, TotalUser, TotalSystem, ThreadsUser, ThreadsSystem uint64
	Policy, Faults, Pageins, CowFaults, MessagesSent, MessagesRecv                int32
	SyscallsMach, SyscallsUnix, Csw, Threadnum, Numrunning, Priority              int32
}

// Table is the process table read in this process.
type Table struct{}

// source is what a sample reads from; the real one asks the kernel.
type source interface {
	list() ([]unix.KinfoProc, error)
	rss(pid int) (int64, error)
	sid(pid int) (int, error)
	args(pid int) ([]byte, error)
}

// Read lists every process this user can read. A process of another user, or
// one that exits between the list and the reads, is skipped, never an error.
func (Table) Read(ctx context.Context) ([]ports.ProcessInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	return read(ctx, kernel{})
}

func read(ctx context.Context, src source) ([]ports.ProcessInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read the process table: %w", err)
	}
	procs, err := src.list()
	if err != nil {
		return nil, fmt.Errorf("read the process table: %w", err)
	}
	rows := make([]ports.ProcessInfo, 0, len(procs))
	for i := range procs {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("read the process table: %w", err)
		}
		k := &procs[i]
		pid := int(k.Proc.P_pid)
		rss, err := src.rss(pid)
		if err != nil {
			continue
		}
		sid, _ := src.sid(pid)
		p := ports.ProcessInfo{
			PID: pid, PPID: int(k.Eproc.Ppid), SID: sid, RSSBytes: rss,
			Started: fmt.Sprintf("%d.%06d", k.Proc.P_starttime.Sec, k.Proc.P_starttime.Usec),
			Name:    comm(k),
		}
		if buf, err := src.args(pid); err == nil {
			if args := parseProcArgs(buf); len(args) > 0 {
				p.Name = baseName(args[0])
				p.Arg = firstArg(args)
				p.Session = conversation(args)
			}
		}
		rows = append(rows, p)
	}
	return rows, nil
}

func comm(k *unix.KinfoProc) string {
	var b []byte
	for _, c := range k.Proc.P_comm {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}

func baseName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// parseProcArgs reads a kern.procargs2 buffer: argc (int32), the exec path,
// its NUL padding, then argc arguments; the environment after them is ignored
// and never kept.
func parseProcArgs(buf []byte) []string {
	if len(buf) < 4 {
		return nil
	}
	argc := int(*(*int32)(unsafe.Pointer(&buf[0])))
	rest := buf[4:]
	i := 0
	for i < len(rest) && rest[i] != 0 {
		i++
	}
	for i < len(rest) && rest[i] == 0 {
		i++
	}
	var args []string
	for rest = rest[i:]; len(args) < argc && len(rest) > 0; {
		j := 0
		for j < len(rest) && rest[j] != 0 {
			j++
		}
		args = append(args, string(rest[:j]))
		if j == len(rest) {
			break
		}
		rest = rest[j+1:]
	}
	return args
}

// firstArg is the argument after the program name, cut at argMax bytes on a
// rune boundary.
func firstArg(args []string) string {
	if len(args) < 2 {
		return ""
	}
	a := args[1]
	if len(a) > argMax {
		cut := argMax
		for cut > 0 && !utf8.RuneStart(a[cut]) {
			cut--
		}
		a = a[:cut]
	}
	return a
}

// conversation is the claude conversation id a process was started for, from
// --session-id or --resume (also in their =value form).
func conversation(args []string) string {
	for i, a := range args {
		for _, flag := range []string{"--session-id", "--resume"} {
			if a == flag && i+1 < len(args) {
				return args[i+1]
			}
			if v, ok := strings.CutPrefix(a, flag+"="); ok {
				return v
			}
		}
	}
	return ""
}

// kernel is the real source.
type kernel struct{}

func (kernel) list() ([]unix.KinfoProc, error) { return unix.SysctlKinfoProcSlice("kern.proc.all") }

func (kernel) sid(pid int) (int, error) { return unix.Getsid(pid) }

func (kernel) args(pid int) ([]byte, error) { return unix.SysctlRaw("kern.procargs2", pid) }

func (kernel) rss(pid int) (int64, error) {
	var ti taskInfo
	n, _, e := syscall.Syscall6(sysProcInfo, procInfoCallPIDInf, uintptr(pid), procPIDTaskInfo, 0,
		uintptr(unsafe.Pointer(&ti)), unsafe.Sizeof(ti))
	if e != 0 {
		return 0, e
	}
	if n != unsafe.Sizeof(ti) {
		return 0, syscall.ESRCH // a zombie or a process that is going away answers nothing
	}
	return int64(ti.ResidentSize), nil
}
