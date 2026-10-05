package tasks

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
)

// The page's signature is what the browser computes from
// GET /api/list_ticket_documents: id@updated_at as the JSON writes it, sorted.
func TestDocumentSignatureMatchesTheAPIsJSON(t *testing.T) {
	zone := time.FixedZone("BRT", -3*3600)
	docs := []*domain.Document{
		{ID: "d2", UpdatedAt: time.Date(2026, 10, 5, 12, 0, 0, 123456789, zone)},
		{ID: "d1", UpdatedAt: time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)},
	}
	raw, err := json.Marshal(map[string]any{"documents": docs})
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Documents []struct {
			ID        string `json:"id"`
			UpdatedAt string `json:"updated_at"`
		} `json:"documents"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, d := range wire.Documents {
		parts = append(parts, d.ID+"@"+d.UpdatedAt)
	}
	slices.Sort(parts)
	if got, want := documentSignature(docs), strings.Join(parts, ","); got != want {
		t.Fatalf("signature %q, the browser computes %q", got, want)
	}
}
