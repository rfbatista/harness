package claudetext

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("CLAUDE_FAKE") == "oneshot" {
		runFakeOneShot()
		return
	}
	os.Exit(m.Run())
}

// oneShotEcho is what the fake one-shot CLI reports back about how it was
// invoked, so a test can assert on the argv and the prompt the adapter sent.
type oneShotEcho struct {
	Args   []string `json:"args"`
	Stdin  string   `json:"stdin"`
	Dir    string   `json:"dir"`
	Marker string   `json:"marker"`
}

// runFakeOneShot behaves like `claude --print --output-format json`: it reads
// the whole prompt from stdin and prints one result envelope. The result text
// is a JSON oneShotEcho of the invocation, so tests can inspect flags, the
// system prompt and the piped text without a second fake.
//
// CLAUDE_FAKE_MODE selects a failure instead: "exit" dies with a stderr line
// the way an unauthenticated CLI does, "is_error" exits 0 with an error
// envelope (what --max-turns overflow looks like), "empty" returns a blank
// result, and "garbage" writes something that is not the JSON envelope.
func runFakeOneShot() {
	in, _ := io.ReadAll(os.Stdin)

	switch os.Getenv("CLAUDE_FAKE_MODE") {
	case "exit":
		fmt.Fprintln(os.Stderr, "fatal: no auth token found")
		os.Exit(3)
	case "is_error":
		fmt.Println(`{"type":"result","subtype":"error_max_turns","is_error":true,"result":"turn limit"}`)
		return
	case "empty":
		fmt.Println(`{"type":"result","subtype":"success","is_error":false,"result":"   "}`)
		return
	case "garbage":
		fmt.Println("not json at all")
		return
	}

	dir, _ := os.Getwd()
	echo, _ := json.Marshal(oneShotEcho{
		Args:   os.Args[1:],
		Stdin:  string(in),
		Dir:    dir,
		Marker: os.Getenv("CLAUDE_FAKE_MARKER"),
	})
	out, _ := json.Marshal(map[string]any{
		"type":     "result",
		"subtype":  "success",
		"is_error": false,
		"result":   string(echo),
	})
	fmt.Println(string(out))
}

// fakeOneShotCmd returns (binPath, env) to launch this test binary as a fake
// one-shot claude. extraEnv tunes its behaviour (CLAUDE_FAKE_MODE, ...).
func fakeOneShotCmd(extraEnv ...string) (string, []string) {
	exe, _ := os.Executable()
	return exe, append(append(os.Environ(), "CLAUDE_FAKE=oneshot"), extraEnv...)
}
