package ports_test

import (
	"encoding/json"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

// The attach protocol's snapshot carries scrollback as an optional field:
// absent when there is none, so consumers treat missing and empty alike.
func TestTerminalSnapshotJSONOmitsEmptyScrollback(t *testing.T) {
	b, err := json.Marshal(ports.TerminalMessage{Type: "snapshot", Snapshot: &ports.TerminalSnapshot{Screen: "a\nb"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "scrollback") {
		t.Fatalf("empty scrollback was sent: %s", b)
	}

	b, err = json.Marshal(ports.TerminalMessage{Type: "snapshot", Snapshot: &ports.TerminalSnapshot{Screen: "c", Scrollback: "a\nb"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"scrollback":"a\nb"`) {
		t.Fatalf("scrollback missing from %s", b)
	}
}

// A client built before the field (the tui-client) decodes a snapshot that
// carries it with everything else intact.
func TestTerminalSnapshotJSONRoundTripsScrollback(t *testing.T) {
	wire := `{"type":"snapshot","snapshot":{"screen":"row","scrollback":"old\nolder","cursor_x":1,"cursor_y":2,"size":{"cols":80,"rows":24}}}`
	var m ports.TerminalMessage
	if err := json.Unmarshal([]byte(wire), &m); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot == nil || m.Snapshot.Screen != "row" || m.Snapshot.Scrollback != "old\nolder" || m.Snapshot.CursorX != 1 || m.Snapshot.CursorY != 2 || m.Snapshot.Size != (ports.TermSize{Cols: 80, Rows: 24}) {
		t.Fatalf("decoded %+v", m.Snapshot)
	}
}
