package tasks

import (
	"slices"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
)

// The feature card (ADR 0058): a feature task and its children are one card,
// in the column of the feature's own status. The browser's featureCard
// (web/src/modules/tasks/domain/feature.js) derives the same from the store's
// task list; web/testdata/views/feature-cards.json pins the words and the
// order.

// barOrder is the progress bar's fill order, left to right.
var barOrder = []string{"done", "review", "in_progress", "todo", "backlog"}

// FeatureCard is what a feature task's card adds to a plain one.
type FeatureCard struct {
	// Segments is the progress bar: the statuses with children, in barOrder.
	Segments []Segment
	// Bar is the bar's text equivalent: "2 done, 1 review, ...".
	Bar string
	// Summary is "2 of 6 done"; Tasks the disclosure's label, "6 tasks".
	Summary, Tasks string
	// Hint is the status mismatch hint; "" when the feature's status fits its
	// children. HintNote says what to do about it.
	Hint, HintNote string
	Children       []ChildLine
}

// Segment is one status of the progress bar.
type Segment struct {
	Status, Label string
	Count         int
}

// ChildLine is one child inside a feature card.
type ChildLine struct {
	TaskID, Label, Href string
	Status, StatusLabel string
	// Archived: a done child archived on its own; the card draws it muted.
	Archived            bool
	LiveState, LiveWord string
	Reviews             int
}

// childTotals counts the children by status.
func childTotals(children []*domain.Ticket) map[string]int {
	n := map[string]int{}
	for _, c := range children {
		n[string(c.Status)]++
	}
	return n
}

var segmentWords = map[string]string{"done": "done", "review": "review", "in_progress": "in progress", "todo": "todo", "backlog": "backlog"}

// taskWord is "1 task", "3 tasks".
func taskWord(n int) string {
	if n == 1 {
		return "1 task"
	}
	return strconv.Itoa(n) + " tasks"
}

// featureCard builds the card part of a feature task from its children.
func featureCard(projectID string, f *domain.Ticket, children []*domain.Ticket, byTask map[string]activity) *FeatureCard {
	total := childTotals(children)
	fc := &FeatureCard{Tasks: taskWord(len(children))}
	var words []string
	for _, st := range barOrder {
		if total[st] == 0 {
			continue
		}
		fc.Segments = append(fc.Segments, Segment{Status: st, Label: statusLabel(domain.TicketStatus(st)), Count: total[st]})
		words = append(words, strconv.Itoa(total[st])+" "+segmentWords[st])
	}
	fc.Bar = strings.Join(words, ", ")
	fc.Summary = strconv.Itoa(total["done"]) + " of " + strconv.Itoa(len(children)) + " done"
	open := len(children) - total["done"]
	switch f.Status {
	case domain.TicketStatusBacklog, domain.TicketStatusTodo, domain.TicketStatusInProgress:
		if open == 0 {
			fc.Hint = "All " + taskWord(len(children)) + " done."
			fc.HintNote = "Move the feature to Review when it is ready."
		}
	case domain.TicketStatusReview, domain.TicketStatusDone:
		if open > 0 {
			fc.Hint = taskWord(open) + " open."
			fc.HintNote = "The feature is in " + statusLabel(f.Status) + " while a child is not done."
		}
	}
	sorted := slices.Clone(children)
	slices.SortStableFunc(sorted, func(a, b *domain.Ticket) int {
		if d := childRank(string(a.Status)) - childRank(string(b.Status)); d != 0 {
			return d
		}
		return b.StatusSince.Compare(a.StatusSince)
	})
	for _, c := range sorted {
		l := ChildLine{TaskID: c.ID, Label: c.Title, Href: Href(projectID, c.ID), Status: string(c.Status), StatusLabel: statusLabel(c.Status), Archived: c.ArchivedAt != nil, Reviews: c.PendingReviews}
		l.LiveState, l.LiveWord = liveDot(byTask[c.ID])
		fc.Children = append(fc.Children, l)
	}
	return fc
}

// liveDot is the dot a task's activity draws: "waiting", "running" or none.
func liveDot(a activity) (state, word string) {
	switch {
	case a.attention:
		return "waiting", "waiting"
	case a.live > 0:
		return "running", strconv.Itoa(a.live) + " live"
	}
	return "", ""
}

// rollUp adds a child's activity to the feature's own.
func rollUp(a, b activity) activity {
	a.live += b.live
	a.attention = a.attention || b.attention
	return a
}

// rolledMerge is the merge badge of a feature and its children: the one
// state they share, else "partial" counting those merged.
func rolledMerge(badges []*MergeBadge) *MergeBadge {
	var merged int
	states := map[string]bool{}
	for _, b := range badges {
		if b == nil {
			continue
		}
		states[b.State] = true
		if b.State == "merged" {
			merged++
		}
	}
	var n int
	for _, b := range badges {
		if b != nil {
			n++
		}
	}
	switch {
	case n == 0:
		return nil
	case len(states) == 1:
		for _, b := range badges {
			if b != nil {
				return &MergeBadge{State: b.State, Word: b.Word, Rail: b.Rail}
			}
		}
		return nil
	}
	of := strconv.Itoa(n)
	m := strconv.Itoa(merged)
	return &MergeBadge{State: "partial", Word: m + " of " + of + " merged", Rail: m + "/" + of + " merged"}
}
