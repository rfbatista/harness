package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Load of the artifacts test: designers sessions publish, revise, move and
// remove designs for designLoad while each one streams events, and one more
// keeps revising a design whose directory is large, so each of its
// revisions copies heavyFiles files of heavyFileSize bytes.
const (
	designers     = 5
	designLoad    = 4 * time.Second
	heavyFiles    = 3000
	heavyFileSize = 32 << 10
)

// timed runs op and fails the test when it errors or takes over readBound.
func (e *freezeE2E) timed(what string, op func() error) bool {
	e.t.Helper()
	start := time.Now()
	err := op()
	took := time.Since(start)
	e.mu.Lock()
	if took > e.slowest {
		e.slowest = took
	}
	e.reads++
	e.mu.Unlock()
	if err != nil {
		e.t.Errorf("%s: %v", what, err)
		return false
	}
	if took > readBound {
		e.t.Errorf("%s took %v, over %v", what, took, readBound)
	}
	return true
}

// startStreaming starts a session on the freeze task that streams events
// until the test ends.
func (e *freezeE2E) startStreaming(name string) *domain.Session {
	e.t.Helper()
	sess, err := e.orch.Start(e.ctx, orchestration.StartRequest{
		ProjectID: e.project, RepositoryID: e.repo, TicketID: e.ticket,
		Task: name, Branch: name, AutoAccept: "all",
	})
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = e.orch.Stop(context.Background(), sess.ID) })
	return sess
}

// With sessions streaming into a database file, several sessions publish,
// revise, move and remove designs at once while another revises a design
// whose every revision copies a large directory. Each operation answers
// within the bound, and afterwards every design still published serves its
// last revision at the revision its row names.
func TestConcurrentDesignWorkOnAFileDatabaseAnswersPromptly(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(prev) })
	dir := t.TempDir()
	e := startFreezeE2E(t, dir, filepath.Join(dir, "data.db"))

	heavy := e.startStreaming("heavy")
	big := filepath.Join(heavy.WorkingDir, "big")
	if err := os.MkdirAll(big, 0o755); err != nil {
		t.Fatal(err)
	}
	filler := []byte(strings.Repeat("x", heavyFileSize))
	for i := range heavyFiles {
		if err := os.WriteFile(filepath.Join(big, fmt.Sprintf("part-%03d.bin", i)), filler, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(big, "index.html"), []byte("<p>big</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	bigArt, err := e.art.Publish(e.ctx, ports.PublishArtifactRequest{SessionID: heavy.ID, Path: "big/index.html", Title: "Big"})
	if err != nil {
		t.Fatal(err)
	}

	sessions := make([]*domain.Session, designers)
	for i := range sessions {
		sessions[i] = e.startStreaming(fmt.Sprintf("designer-%d", i))
	}

	// want is each design's last content and revision; a removed design
	// wants nothing.
	type wanted struct {
		content  string
		revision int
		removed  bool
	}
	var (
		wantMu sync.Mutex
		want   = map[string]wanted{}
	)
	deadline := time.Now().Add(designLoad)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() { // the slow copy: exempt from the bound, it is the load
		defer wg.Done()
		for n := 2; time.Now().Before(deadline) && !t.Failed(); n++ {
			content := fmt.Sprintf("<p>big %d</p>", n)
			a, err := e.art.ReviseDesign(e.ctx, bigArt.ID, []byte(content))
			if err != nil {
				t.Errorf("revise the big design: %v", err)
				return
			}
			wantMu.Lock()
			want[bigArt.ID] = wanted{content: content, revision: a.Revision}
			wantMu.Unlock()
		}
	}()

	for i, sess := range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := 0; time.Now().Before(deadline) && !t.Failed(); round++ {
				p := fmt.Sprintf("d%d-%d.html", i, round)
				var a *domain.Artifact
				if !e.timed("publish "+p, func() (err error) {
					a, err = e.art.PublishDesign(e.ctx, sess.ID, p, "Design", []byte("<p>v1</p>"))
					return err
				}) {
					return
				}
				content := "<p>v1</p>"
				for v := 2; v <= 3; v++ {
					content = fmt.Sprintf("<p>%s v%d</p>", p, v)
					if !e.timed("revise "+p, func() (err error) {
						a, err = e.art.ReviseDesign(e.ctx, a.ID, []byte(content))
						return err
					}) {
						return
					}
				}
				if !e.timed("move "+p+" to the project", func() error {
					_, err := e.art.SetArtifactScope(e.ctx, a.ID, domain.ArtifactScopeProject)
					return err
				}) || !e.timed("move "+p+" back to the task", func() error {
					_, err := e.art.SetArtifactScope(e.ctx, a.ID, domain.ArtifactScopeTask)
					return err
				}) {
					return
				}
				w := wanted{content: content, revision: a.Revision}
				if round%3 == 2 {
					if !e.timed("remove "+p, func() error { return e.art.RemoveDesign(e.ctx, a.ID) }) {
						return
					}
					w = wanted{removed: true}
				}
				wantMu.Lock()
				want[a.ID] = w
				wantMu.Unlock()
			}
		}()
	}
	wg.Wait()
	t.Logf("%d design operations, slowest %v", e.reads, e.slowest)
	if t.Failed() {
		return
	}

	for id, w := range want {
		if w.removed {
			if a, _ := e.art.GetArtifact(e.ctx, id); a != nil {
				t.Errorf("removed design %s is still published: %+v", id, a)
			}
			continue
		}
		var a domain.Artifact
		if err := json.Unmarshal(e.get("/api/artifacts/"+id), &a); err != nil || a.Revision != w.revision {
			t.Errorf("design %s names revision %d, want %d (%v)", id, a.Revision, w.revision, err)
		}
		if body := string(e.get("/api/artifacts/" + id + "/view/")); body != w.content {
			t.Errorf("design %s serves %q, want %q", id, body, w.content)
		}
	}
}
