package bench

import (
	"reflect"
	"testing"

	"operators-mcp/internal/domain"
)

func TestMockLayerRecordsEachCallAndAnswersWithTheCannedResult(t *testing.T) {
	m := NewMockLayer([]domain.BenchTool{
		{Name: "read_file", Result: "# Demo"},
		{Name: "flaky", Result: "boom", IsError: true},
	})

	res := m.Call("read_file", map[string]any{"path": "README.md"})
	if res.Text != "# Demo" || res.IsError {
		t.Fatalf("read_file = %+v", res)
	}
	if res := m.Call("flaky", nil); res.Text != "boom" || !res.IsError {
		t.Fatalf("flaky = %+v", res)
	}

	want := []domain.BenchCall{{Tool: "read_file", Params: map[string]any{"path": "README.md"}}, {Tool: "flaky"}}
	if got := m.Calls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %+v, want %+v", got, want)
	}
}

func TestMockLayerRecordsACallToAnUnmockedToolAndAnswersWithAnError(t *testing.T) {
	m := NewMockLayer([]domain.BenchTool{{Name: "read_file", Result: "x"}})

	res := m.Call("delete_everything", map[string]any{"force": true})
	if !res.IsError || res.Text == "" {
		t.Fatalf("res = %+v, want an error result", res)
	}
	if got := m.Calls(); len(got) != 1 || got[0].Tool != "delete_everything" {
		t.Fatalf("calls = %+v", got)
	}
}

func TestMockLayerCallsAreACopy(t *testing.T) {
	m := NewMockLayer([]domain.BenchTool{{Name: "a", Result: "r"}})
	m.Call("a", nil)
	got := m.Calls()
	got[0].Tool = "changed"
	if m.Calls()[0].Tool != "a" {
		t.Fatal("the recorded calls were changed through the copy")
	}
}
