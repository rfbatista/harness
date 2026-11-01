package domain_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"operators-mcp/internal/domain"
)

// The fixed cases are shared with the browser module
// (design-system/preview-parity.test.mjs): the Go rule and the JS copy of it
// must answer the same.
func TestPreviewRuleAnswersTheFixedCases(t *testing.T) {
	raw, err := os.ReadFile("testdata/design_preview_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string                     `json:"name"`
		Previous *domain.DesignSystemRecord `json:"previous"`
		Record   domain.DesignSystemRecord  `json:"record"`
		Root     string                     `json:"root"`
		Document string                     `json:"document"`
		Errors   []string                   `json:"errors"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			c.Record.Normalize()
			if got := domain.RootRule(c.Record.Tokens); got != c.Root {
				t.Errorf("root rule\n got %s\nwant %s", got, c.Root)
			}
			if c.Document != "" {
				if got := domain.PreviewDocument(c.Record.Tokens, c.Record.Components[0].Previews[0]); got != c.Document {
					t.Errorf("document\n got %s\nwant %s", got, c.Document)
				}
			}
			var previous domain.DesignSystemRecord
			if c.Previous != nil {
				previous = *c.Previous
			}
			got := []string{}
			for _, e := range c.Record.Validate(previous) {
				got = append(got, e.Code+" "+e.Path)
			}
			if !slices.Equal(got, c.Errors) {
				t.Errorf("errors = %v, want %v", got, c.Errors)
			}
		})
	}
}
