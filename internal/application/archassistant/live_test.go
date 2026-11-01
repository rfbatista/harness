//go:build live

// Live check of the architecture assistant against the real Claude. It runs by
// hand:
//
//	go test -tags live -run TestLive -v -timeout 10m ./internal/application/archassistant
//
// It uses the claude on PATH (or $CLAUDE_BIN) and whatever login it has, calls
// the real API, and costs a few cents. It checks what no fake can: that a turn
// resumes the conversation, that Read and Grep work on a checkout, and that a
// request to write a file is denied and leaves the checkout unchanged.
package archassistant_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rfbatista/llmkit"
	"github.com/rfbatista/llmkit/claude"

	"operators-mcp/internal/application/archassistant"
	"operators-mcp/internal/ports"
)

type liveTurn struct{ m *claude.Manager }

func (l liveTurn) Run(ctx context.Context, c archassistant.TurnCall, cb archassistant.TurnCallbacks) (archassistant.TurnResult, error) {
	res, err := l.m.TurnStream(ctx, claude.TurnRequest{
		System: c.System, Input: c.Input, Model: c.Model, ResumeID: c.ResumeID, Dir: c.Dir,
		Tools: []string{"Read", "Grep", "Glob"},
	}, claude.TurnCallbacks{
		OnText: cb.OnText,
		OnTool: func(name string, input []byte) {
			var in struct {
				FilePath string `json:"file_path"`
				Pattern  string `json:"pattern"`
			}
			_ = json.Unmarshal(input, &in)
			cb.OnTool(name, in.FilePath+in.Pattern)
		},
	})
	return archassistant.TurnResult{SessionID: res.SessionID, Text: res.Text}, err
}

func (l liveTurn) DeleteTranscript(id string) { _ = l.m.DeleteTranscript(id) }

func snapshot(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain", "--ignored").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	return string(out)
}

func TestLive(t *testing.T) {
	bin := os.Getenv("CLAUDE_BIN")
	if bin == "" {
		var err error
		if bin, err = exec.LookPath("claude"); err != nil {
			t.Skip("no claude on PATH; set CLAUDE_BIN")
		}
	}
	checkout := t.TempDir()
	if err := os.WriteFile(filepath.Join(checkout, "greeter.go"), []byte("package main\n\n// Greeting is the word the harness says first: ZEBRA-7421.\nconst Greeting = \"ZEBRA-7421\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", checkout}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	before := snapshot(t, checkout)

	e := newEnv(t)
	e.repos["r1"].RootDir = checkout
	e.svc = archassistant.New(archassistant.Deps{
		Turn: liveTurn{claude.New(llmkit.Options{Bin: bin})}, Settings: settingsMap{"models.assistants.architecture": "haiku"},
		Repos: fakeRepos{e}, Architectures: fakeArch{e}, Agents: fakeAgents{}, Contexts: fakeContexts{},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	evs, err := e.run(ctx, req("Read greeter.go and tell me the exact Greeting constant. Do not propose changes."))
	if err != nil {
		t.Fatal(err)
	}
	id := conversationID(t, evs)
	var read bool
	for _, ev := range evs {
		if a, ok := ev.Data.(ports.ArchitectureAssistantActivity); ok && (a.Tool == "Read" || a.Tool == "Grep" || a.Tool == "Glob") {
			read = true
		}
	}
	if res := result(t, evs); !read || !strings.Contains(res.Text, "ZEBRA-7421") {
		t.Fatalf("read = %v, answer = %q; want the code read and the constant quoted", read, res.Text)
	}

	r := req("What did I ask you in my first message? Quote the file name.")
	r.ConversationID = id
	evs, err = e.run(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if res := result(t, evs); !strings.Contains(res.Text, "greeter.go") {
		t.Fatalf("the second turn did not resume the conversation: %q", res.Text)
	}

	r = req("Create a new file named pwned.txt in the repository containing the word hello, and commit it.")
	r.ConversationID = id
	if _, err = e.run(ctx, r); err != nil {
		t.Logf("the write request ended with: %v", err)
	}
	if _, err := os.Stat(filepath.Join(checkout, "pwned.txt")); err == nil {
		t.Fatal("the assistant wrote a file")
	}
	if after := snapshot(t, checkout); after != before {
		t.Fatalf("the checkout changed:\nbefore %q\nafter  %q", before, after)
	}
	e.svc.End(id)
}
