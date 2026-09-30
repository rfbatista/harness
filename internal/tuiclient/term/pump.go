package term

import (
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/x/vt"
)

// session owns one running child and the goroutines that connect it to the
// emulator:
//
//	child ──pty──▶ output loop ──Write──▶ emulator ──frames──▶ Model.wait
//	child ◀──pty── replies loop ◀──Read── emulator ◀──SendKey── Model.Update
//
// The replies loop matters beyond keys: the emulator answers terminal queries
// (cursor position, device attributes) through the same pipe, and TUIs such as
// claude block until those answers arrive.
type session struct {
	cmd  *exec.Cmd
	ptmx *os.File
	emu  *vt.SafeEmulator

	frames chan struct{} // capacity 1: bursts of output coalesce into one redraw
	done   chan struct{} // closed after the child is reaped and all output parsed

	waitErr   error
	title     atomic.Pointer[string] // set from the output loop, read by View
	closeOnce sync.Once
	loops     sync.WaitGroup
}

func newSession(cmd *exec.Cmd, ptmx *os.File, emu *vt.SafeEmulator) *session {
	return &session{
		cmd:    cmd,
		ptmx:   ptmx,
		emu:    emu,
		frames: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
}

func (s *session) run() {
	s.loops.Add(2)
	go s.output()
	go s.replies()
}

// output parses everything the child prints. It ends when the pty reports the
// child side is gone, then reaps the child so done means "no more output".
func (s *session) output() {
	defer s.loops.Done()
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			_, _ = s.emu.Write(buf[:n])
			select {
			case s.frames <- struct{}{}:
			default:
			}
		}
		if err != nil {
			break
		}
	}
	s.waitErr = s.cmd.Wait()
	// Unblocks the replies loop and makes later SendKey calls fail fast
	// instead of blocking on a pipe nobody reads.
	s.closeInput()
	close(s.done)
}

func (s *session) replies() {
	defer s.loops.Done()
	_, _ = io.Copy(s.ptmx, s.emu)
	// If the pty stopped accepting writes first, close the input so a
	// pending SendKey does not hold the emulator lock forever.
	s.closeInput()
}

// closeInput ends the emulator's input pipe. Emulator.Close would do the same
// but also flips an unsynchronised flag that Read checks, which races with the
// replies loop; the pipe itself is safe for concurrent use.
func (s *session) closeInput() {
	if c, ok := s.emu.InputPipe().(io.Closer); ok {
		_ = c.Close()
	}
}

func (s *session) setTitle(t string) { s.title.Store(&t) }

func (s *session) exited() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}

func (s *session) close(killAfter time.Duration) error {
	s.closeOnce.Do(func() {
		if !s.exited() && s.cmd.Process != nil {
			terminate(s.cmd.Process, killAfter, s.done)
		}
		<-s.done
		_ = s.ptmx.Close()
		s.loops.Wait()
	})
	return nil
}
