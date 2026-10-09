package shell

import (
	"context"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestParseCollapsedReadsStatusKeys(t *testing.T) {
	for _, c := range []struct {
		value string
		want  map[string]bool
	}{
		{"", map[string]bool{}},
		{"backlog,done", map[string]bool{"backlog": true, "done": true}},
		{" backlog , in_progress ,", map[string]bool{"backlog": true, "in_progress": true}},
		{"done,done,bogus", map[string]bool{"done": true, "bogus": true}}, // the rail ignores keys no group has
		{"backlog;x", map[string]bool{}},                                  // malformed: nothing is collapsed
		{"Backlog", map[string]bool{}},
		{"backlog,%20", map[string]bool{}},
	} {
		if got := ParseCollapsed(c.value); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseCollapsed(%q) = %v, want %v", c.value, got, c.want)
		}
	}
}

func TestCollapsedTravelsFromTheRequestToTheLayout(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Cookie", CollapsedCookie+"=backlog,done")
	ctx := WithCollapsed(context.Background(), r)
	if got := CollapsedFrom(ctx); !reflect.DeepEqual(got, map[string]bool{"backlog": true, "done": true}) {
		t.Fatalf("collapsed = %v", got)
	}
	if got := CollapsedFrom(context.Background()); len(got) != 0 {
		t.Errorf("no cookie: %v", got)
	}
}
