package httpapi

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("CLAUDE_FAKE") == "1" {
		// CLAUDE_FAKE_ENV_FILE: write the environment claude was started
		// with there, one variable per line.
		if path := os.Getenv("CLAUDE_FAKE_ENV_FILE"); path != "" {
			_ = os.WriteFile(path, []byte(strings.Join(os.Environ(), "\n")), 0o600)
		}
		// CLAUDE_FAKE_ARGS_FILE: write the arguments there, one per line.
		if path := os.Getenv("CLAUDE_FAKE_ARGS_FILE"); path != "" {
			_ = os.WriteFile(path, []byte(strings.Join(os.Args[1:], "\n")), 0o600)
		}
		out := bufio.NewWriter(os.Stdout)
		fmt.Fprintln(out, `{"type":"system","subtype":"init","session_id":"fake"}`)
		out.Flush()
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 1024*1024), 1024*1024)
		for sc.Scan() {
			if sc.Text() == "" {
				continue
			}
			fmt.Fprintln(out, `{"type":"assistant","session_id":"fake","message":{"role":"assistant","content":[{"type":"text","text":"working"}]}}`)
			// CLAUDE_FAKE_USAGE: end each turn with a result that reports
			// what it used, cache tokens included.
			if os.Getenv("CLAUDE_FAKE_USAGE") == "1" {
				fmt.Fprintln(out, `{"type":"result","subtype":"success","session_id":"fake","is_error":false,"result":"working","total_cost_usd":0.25,`+
					`"usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":300,"cache_read_input_tokens":4000}}`)
			}
			out.Flush()
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
