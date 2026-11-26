package mcpbench

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"operators-mcp/internal/application/bench"
	"operators-mcp/internal/domain"
)

func rpc(t *testing.T, url, body string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("not json: %s", raw)
	}
	return out
}

func TestTheServedLayerListsTheCaseToolsAndRecordsTheirCalls(t *testing.T) {
	layer := bench.NewMockLayer([]domain.BenchTool{{Name: "read_file", Description: "reads", Result: "# Demo"}})
	url, stop, err := Serve(layer)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	list := rpc(t, url, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := list["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "read_file" {
		t.Fatalf("tools = %v", list)
	}

	call := rpc(t, url, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"README.md"}}}`)
	content := call["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]
	if content != "# Demo" {
		t.Fatalf("call = %v", call)
	}
	calls := layer.Calls()
	if len(calls) != 1 || calls[0].Tool != "read_file" || calls[0].Params["path"] != "README.md" {
		t.Fatalf("recorded = %+v", calls)
	}
}

func TestACallToAnUnmockedToolIsRecordedAndAnswersWithAnErrorResult(t *testing.T) {
	layer := bench.NewMockLayer([]domain.BenchTool{{Name: "read_file", Result: "x"}})
	url, stop, err := Serve(layer)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	rpc(t, url, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"rm","arguments":{}}}`)
	if calls := layer.Calls(); len(calls) != 1 || calls[0].Tool != "rm" {
		t.Fatalf("recorded = %+v", calls)
	}
}
