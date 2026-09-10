package httpapi

import (
	"bufio"
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("CLAUDE_FAKE") == "1" {
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
			out.Flush()
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
