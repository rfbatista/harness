package psprocs

import "testing"

func TestParseReadsPidSessionRSSNameFirstArgAndTheConversation(t *testing.T) {
	rows := Parse(`
    1     0 0x0      9840 Wed Nov 25 20:00:00 2026 /sbin/launchd
  102   101 0xabc1 409600 Thu Nov 26 08:59:31 2026 /usr/local/bin/claude --session-id c-1 --token s3cret
  201   102 0xabc1 307200 Thu Nov 26 09:00:02 2026 node /repo/node_modules/.bin/jest --runInBand
  bad line
`)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want 3", rows)
	}
	a := rows[1]
	if a.PID != 102 || a.PPID != 101 || a.RSSBytes != 400<<20 || a.Name != "claude" || a.Arg != "--session-id" || a.Session != "c-1" || a.Started != "Thu Nov 26 08:59:31 2026" {
		t.Errorf("agent row = %+v", a)
	}
	if rows[1].SID == 0 || rows[1].SID != rows[2].SID || rows[0].SID != 0 {
		t.Errorf("session keys = %d %d %d", rows[0].SID, rows[1].SID, rows[2].SID)
	}
	if rows[2].Name != "node" || rows[2].Arg != "/repo/node_modules/.bin/jest" || rows[2].Session != "" {
		t.Errorf("node row = %+v", rows[2])
	}
}
