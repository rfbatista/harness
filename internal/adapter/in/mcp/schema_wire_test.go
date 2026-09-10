package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/server"
	"github.com/rfbatista/harnesskit/tool"

	"operators-mcp/internal/domain"
)

// Schema constraints have to survive registration, not merely exist on the
// tool. This registers a tool carrying every awkward JSON Schema construct and
// reads the schema back off the wire.
func TestRegisteredToolKeepsItsWholeSchema(t *testing.T) {
	const raw = `{"type":"object",` +
		`"properties":{"on_duplicate":{"type":"string","enum":["skip","update","rename"]},` +
		`"metadata":{"type":"object","additionalProperties":{"type":"string"}}},` +
		`"required":["on_duplicate"],` +
		`"oneOf":[{"required":["on_duplicate"]},{"required":["metadata"]}],` +
		`"$defs":{"node":{"type":"object"}}}`

	s := server.NewMCPServer("test", "0.0.1", server.WithToolCapabilities(true))
	RegisterDomainTools(s, []domain.Tool{{
		Name:        "demo",
		Description: "d",
		InputSchema: tool.SchemaFromJSON(raw),
		Source:      "code",
		Handler:     func(context.Context, map[string]any) (any, error) { return nil, nil },
	}})

	// Ask the server for its tool list the way a client would.
	res := s.HandleMessage(context.Background(),
		json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}

	var envelope struct {
		Result struct {
			Tools []struct {
				Name        string         `json:"name"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		t.Fatalf("decode tools/list: %v (body=%s)", err, b)
	}
	if len(envelope.Result.Tools) != 1 {
		t.Fatalf("tools/list returned %d tools: %s", len(envelope.Result.Tools), b)
	}

	var want map[string]any
	if err := json.Unmarshal([]byte(raw), &want); err != nil {
		t.Fatal(err)
	}
	got := envelope.Result.Tools[0].InputSchema

	for _, key := range []string{"type", "properties", "required", "oneOf", "$defs"} {
		gotJSON, _ := json.Marshal(got[key])
		wantJSON, _ := json.Marshal(want[key])
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("schema key %q did not survive registration\n got: %s\nwant: %s",
				key, gotJSON, wantJSON)
		}
	}
}
