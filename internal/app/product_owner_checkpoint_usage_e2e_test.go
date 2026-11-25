//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeToolHeavyClaude answers each message the way the real CLI answers a
// turn that uses tools: three API calls, each an assistant message with its
// own usage, and a result whose usage is the SUM of the three (claude reports
// the turn's total there, not the context of its last call). Each call reads
// 50,000 tokens from the cache, so the context is about 50,000 tokens, a
// quarter of the 200,000 window, while the result's usage adds up to 150,006,
// past 60% of it.
const fakeToolHeavyClaude = `#!/bin/sh
dir=$(dirname "$0")
echo "$*" > "$dir/launch.$$"
printf '%s\n' '{"type":"system","subtype":"init","session_id":"fake"}'
while IFS= read -r line; do
  text=$(printf '%s' "$line" | sed -n 's/.*"text":"\([^"]*\)".*/\1/p')
  printf '%s\n' "$text" >> "$dir/received"
  printf '%s\n' '{"type":"assistant","session_id":"fake","message":{"id":"m1","role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"project/docs/a.md"}}],"usage":{"input_tokens":2,"cache_read_input_tokens":50000,"output_tokens":40}}}'
  printf '%s\n' '{"type":"user","session_id":"fake","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"a"}]}}'
  printf '%s\n' '{"type":"assistant","session_id":"fake","message":{"id":"m2","role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Read","input":{"file_path":"project/docs/b.md"}}],"usage":{"input_tokens":2,"cache_read_input_tokens":50000,"output_tokens":40}}}'
  printf '%s\n' '{"type":"user","session_id":"fake","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t2","content":"b"}]}}'
  printf '%s\n' '{"type":"stream_event","session_id":"fake","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"answer to: '"$text"'"}}}'
  printf '%s\n' '{"type":"assistant","session_id":"fake","message":{"id":"m3","role":"assistant","content":[{"type":"text","text":"answer to: '"$text"'"}],"usage":{"input_tokens":2,"cache_read_input_tokens":50000,"output_tokens":9}}}'
  printf '%s\n' '{"type":"result","subtype":"success","session_id":"fake","result":"ok","total_cost_usd":0.02,"usage":{"input_tokens":6,"cache_read_input_tokens":150000,"output_tokens":89}}'
done
`

// The context a turn leaves is what its last API call read, not the sum the
// turn's result reports across its calls: a thread whose tool-heavy turn ends
// well under 60% of the window does not checkpoint (ADR 0112).
func TestAToolHeavyTurnWhoseLastCallIsUnderTheCheckpointSizeDoesNotCheckpoint(t *testing.T) {
	e := newPOE2E(t)
	if err := os.WriteFile(filepath.Join(e.fakeDir, "claude"), []byte(fakeToolHeavyClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	threadID, _ := e.startThread("what changed this week?")
	th := e.waitTurn(threadID, "idle")
	if got := checkpointsOf(th.Items); len(got) != 0 {
		t.Errorf("items = %v, want no checkpoint: the context is 50,002 tokens, the result's 150,006 is a sum over three calls", kinds(th.Items))
	}
	if n := e.checkpointsListed(threadID); n != 0 {
		t.Errorf("checkpoints = %d, want 0", n)
	}
	if l := e.launches(); len(l) != 1 {
		t.Errorf("%d launches, want the one: a checkpoint starts a new conversation", len(l))
	}
}
