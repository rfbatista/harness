// Command claude-pane is a terminal workbench over coding_pool: pick a
// project, then one of its tasks, then open agent sessions in it. Each session
// is interactive claude in its own git worktree, running in a pane.
//
//	claude-pane [--api http://localhost:8080]
//
// The coding_pool server must be running (`make air`); it provisions and
// records the sessions. The claude binary comes from $CLAUDE_BIN, else PATH.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	tea "charm.land/bubbletea/v2"

	"operators-mcp/internal/adapter/in/workbench"
	"operators-mcp/internal/domain"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "claude-pane:", err)
		os.Exit(1)
	}
}

func run() error {
	api := flag.String("api", envOr("CODING_POOL_API", "http://localhost:8080"), "coding_pool server URL")
	flag.Parse()

	bin, err := claudeBin()
	if err != nil {
		return err
	}
	be := workbench.NewHTTPClient(*api)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := be.Health(ctx); err != nil {
		return fmt.Errorf("%w\nstart the server first: make air", err)
	}
	if n, err := endOrphans(ctx, be); err != nil {
		return err
	} else if n > 0 {
		fmt.Fprintf(os.Stderr, "claude-pane: marked %d session(s) from a previous run as stopped\n", n)
	}

	final, err := tea.NewProgram(workbench.New(be, bin)).Run()
	if m, ok := final.(workbench.Model); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}
	return err
}

// endOrphans stops interactive sessions still recorded as running. Their
// terminal belonged to a claude-pane that is gone — it crashed or was killed
// before it could end them — since this one has not opened any yet. That holds
// as long as one claude-pane runs at a time.
func endOrphans(ctx context.Context, be workbench.Backend) (int, error) {
	running, err := be.ListSessions(ctx, workbench.SessionFilter{Statuses: []domain.SessionStatus{domain.SessionRunning}})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, s := range running {
		if s.Interactive {
			if err := be.EndSession(ctx, s.ID, 0, true); err == nil {
				n++
			}
		}
	}
	return n, nil
}

func claudeBin() (string, error) {
	if bin := os.Getenv("CLAUDE_BIN"); bin != "" {
		return bin, nil
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		return "", fmt.Errorf("claude not found on PATH (set CLAUDE_BIN): %w", err)
	}
	return bin, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
