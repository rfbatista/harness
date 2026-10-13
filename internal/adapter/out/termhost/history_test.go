package termhost

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

func TestHistoryKeepsTheNewestLinesInOrder(t *testing.T) {
	h := newHistory(3)
	if got := h.snapshot(); len(got) != 0 {
		t.Fatalf("empty history = %q", got)
	}
	for i := 1; i <= 7; i++ {
		h.push(fmt.Sprint(i))
		want := []string{}
		for j := max(1, i-2); j <= i; j++ {
			want = append(want, fmt.Sprint(j))
		}
		if got := h.snapshot(); !slices.Equal(got, want) {
			t.Fatalf("after %d pushes: %q, want %q", i, got, want)
		}
	}
	snap := h.snapshot()
	h.push("8")
	if !slices.Equal(snap, []string{"5", "6", "7"}) {
		t.Errorf("a snapshot changed after a push: %q", snap)
	}
}

// fullTerminal is a terminal with no process whose history is full of
// styled 80-column lines.
func fullTerminal(b *testing.B) (*terminal, []byte) {
	b.Helper()
	t := newTerminal(nil, ports.TermSize{Cols: 80, Rows: 24}, 0, 1)
	var chunk strings.Builder
	for i := range 200 {
		fmt.Fprintf(&chunk, "\x1b[1;3%dm%-74d\x1b[0m end\r\n", i%8, i)
	}
	out := []byte(chunk.String())
	for t.history.len() < historyLines {
		t.feed(out)
	}
	return t, out
}

func BenchmarkFeedWithFullHistory(b *testing.B) {
	t, out := fullTerminal(b)
	b.SetBytes(int64(len(out)))
	b.ResetTimer()
	for b.Loop() {
		t.feed(out)
	}
}

func BenchmarkSubscribeWithFullHistory(b *testing.B) {
	t, _ := fullTerminal(b)
	var size int
	for _, l := range t.history.snapshot() {
		size += len(l)
	}
	b.ResetTimer()
	for b.Loop() {
		_, sub := t.Subscribe()
		sub.Close()
	}
	b.ReportMetric(float64(size)/historyLines, "history-bytes/line")
}
