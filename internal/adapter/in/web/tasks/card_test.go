package tasks

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The browser's card (web/src/modules/tasks/domain/card.js) reads the same
// fixture, so the first paint and the live board say the same.
type cardViews struct {
	Now  time.Time `json:"now"`
	Ages []struct {
		Status string    `json:"status"`
		Since  time.Time `json:"since"`
		Age    string    `json:"age"`
		Stale  bool      `json:"stale"`
	} `json:"ages"`
	LastMoves []struct {
		LastMove *struct {
			Status       string    `json:"status"`
			By           string    `json:"by"`
			SessionLabel string    `json:"session_label"`
			Reason       string    `json:"reason"`
			At           time.Time `json:"at"`
		} `json:"last_move"`
		Lead, Who, Reason string
	} `json:"lastMoves"`
	Merges []struct {
		Status     string      `json:"status"`
		MergeState *mergeState `json:"merge_state"`
		Badge      *struct {
			State, Word, Rail string
		} `json:"badge"`
	} `json:"merges"`
}

func readCardViews(t *testing.T) cardViews {
	t.Helper()
	raw, err := os.ReadFile("../../../../../web/testdata/views/task-card.json")
	if err != nil {
		t.Fatal(err)
	}
	var v cardViews
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestACardsTimeInColumnReadsAsTheBoardSaysIt(t *testing.T) {
	v := readCardViews(t)
	for _, c := range v.Ages {
		text, stale := cardAge(c.Status, c.Since, v.Now)
		if text != c.Age || stale != c.Stale {
			t.Errorf("%s since %s = %q stale=%v, want %q stale=%v", c.Status, c.Since, text, stale, c.Age, c.Stale)
		}
	}
	if text, stale := cardAge("review", time.Time{}, v.Now); text != "" || stale {
		t.Errorf("unknown since = %q %v", text, stale)
	}
}

func TestACardsLastMoveSaysWhoMovedItHere(t *testing.T) {
	for _, c := range readCardViews(t).LastMoves {
		var m *lastMove
		if c.LastMove != nil {
			m = &lastMove{Status: c.LastMove.Status, By: c.LastMove.By, SessionLabel: c.LastMove.SessionLabel, Reason: c.LastMove.Reason, At: c.LastMove.At}
		}
		lead, who, reason := lastMoveLine(m)
		if lead != c.Lead || who != c.Who || reason != c.Reason {
			t.Errorf("%+v = %q %q %q, want %q %q %q", c.LastMove, lead, who, reason, c.Lead, c.Who, c.Reason)
		}
	}
}

func TestTheMergeBadgeShowsOnReviewAndDoneOnly(t *testing.T) {
	for _, c := range readCardViews(t).Merges {
		got := mergeBadge(c.Status, c.MergeState)
		switch {
		case c.Badge == nil && got != nil:
			t.Errorf("%s %+v = %+v, want none", c.Status, c.MergeState, got)
		case c.Badge != nil && (got == nil || *got != MergeBadge{State: c.Badge.State, Word: c.Badge.Word, Rail: c.Badge.Rail}):
			t.Errorf("%s %+v = %+v, want %+v", c.Status, c.MergeState, got, c.Badge)
		}
	}
}
