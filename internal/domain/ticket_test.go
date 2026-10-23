package domain

import (
	"encoding/json"
	"testing"

	"github.com/rfbatista/harnesskit/errs"
)

func TestTicket_CheckParent(t *testing.T) {
	feature := &Ticket{ID: "feat", ProjectID: "p1"}
	cases := map[string]struct {
		child       Ticket
		parent      *Ticket
		hasChildren bool
		wantCode    string
	}{
		"a new task takes a top-level task of its project": {
			child: Ticket{ProjectID: "p1"}, parent: feature,
		},
		"an existing task takes a top-level task of its project": {
			child: Ticket{ID: "t1", ProjectID: "p1"}, parent: feature,
		},
		"a missing parent": {
			child: Ticket{ProjectID: "p1"}, parent: nil, wantCode: CodeTicketParentInvalid,
		},
		"a parent in another project": {
			child: Ticket{ProjectID: "p2"}, parent: feature, wantCode: CodeTicketParentInvalid,
		},
		"itself": {
			child: Ticket{ID: "feat", ProjectID: "p1"}, parent: feature, wantCode: CodeTicketParentInvalid,
		},
		"a parent that is already a child": {
			child: Ticket{ID: "t1", ProjectID: "p1"}, parent: &Ticket{ID: "c1", ProjectID: "p1", ParentID: "feat"},
			wantCode: CodeTicketParentInvalid,
		},
		"a task that already has children": {
			child: Ticket{ID: "t1", ProjectID: "p1"}, parent: feature, hasChildren: true,
			wantCode: CodeTicketParentInvalid,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.child.CheckParent(tc.parent, tc.hasChildren)
			if got := errs.Code(err); got != tc.wantCode {
				t.Fatalf("CheckParent error code = %q (%v), want %q", got, err, tc.wantCode)
			}
		})
	}
}

func TestTicket_ParentIDIsAlwaysOnTheWire(t *testing.T) {
	b, err := json.Marshal(Ticket{ID: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if v, ok := got["parent_id"]; !ok || v != "" {
		t.Fatalf("parent_id = %v (present %v), want an empty string", v, ok)
	}
}
