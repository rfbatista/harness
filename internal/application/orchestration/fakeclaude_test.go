package orchestration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// fakeLaunchFile is where the fake CLI, run with CLAUDE_FAKE_RECORD=1, writes
// how it was launched, in its working directory: what a headless session's
// CLI gets.
const fakeLaunchFile = "fake-claude-launch.json"

// fakeLaunch is what fakeLaunchFile holds.
type fakeLaunch struct {
	Args     []string `json:"args"`
	Shell    string   `json:"shell"`     // HARNESS_SHELL
	ShellURL string   `json:"shell_url"` // HARNESS_SHELL_URL
	Path     string   `json:"path"`      // PATH
}

func TestMain(m *testing.M) {
	if os.Getenv("CLAUDE_FAKE") == "1" {
		runFakeClaude()
		return
	}
	os.Exit(m.Run())
}

func runFakeClaude() {
	if os.Getenv("CLAUDE_FAKE_RECORD") == "1" {
		b, _ := json.Marshal(fakeLaunch{Args: os.Args[1:], Shell: os.Getenv("HARNESS_SHELL"),
			ShellURL: os.Getenv("HARNESS_SHELL_URL"), Path: os.Getenv("PATH")})
		_ = os.WriteFile(fakeLaunchFile+".tmp", b, 0o644)
		_ = os.Rename(fakeLaunchFile+".tmp", fakeLaunchFile)
	}
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	fmt.Fprintln(out, `{"type":"system","subtype":"init","session_id":"fake"}`)
	out.Flush()
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		if sc.Text() == "" {
			continue
		}
		fmt.Fprintln(out, `{"type":"assistant","session_id":"fake","message":{"role":"assistant","content":[{"type":"text","text":"working"}]}}`)
		fmt.Fprintln(out, `{"type":"result","subtype":"success","session_id":"fake","result":"ok","total_cost_usd":0.02,"usage":{"input_tokens":5,"output_tokens":9}}`)
		out.Flush()
	}
	os.Exit(0)
}
