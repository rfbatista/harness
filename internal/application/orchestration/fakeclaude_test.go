package orchestration

import (
	"bufio"
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("CLAUDE_FAKE") == "1" {
		runFakeClaude()
		return
	}
	os.Exit(m.Run())
}

func runFakeClaude() {
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
