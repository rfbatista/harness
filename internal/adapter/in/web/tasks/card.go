package tasks

import (
	"strconv"
	"time"
)

// How a task card on the board reads its signals: the time in its column, the
// last move and the merge badge. The browser draws the live board by the same
// rules (web/src/modules/tasks/domain/card.js); web/testdata/views/task-card.json
// pins them together.

// staleAfter is how long a task may sit in a column before its age says
// "stale"; columns not listed never go stale.
var staleAfter = map[string]time.Duration{
	"in_progress": 3 * 24 * time.Hour,
	"review":      2 * 24 * time.Hour,
}

// cardAge is the time a task has been in its column: "now", "4m", "2h",
// "yesterday", "5d", then the date from 30 days on; " · stale" past its
// column's threshold. A zero since is unknown: no text.
func cardAge(status string, since, now time.Time) (text string, stale bool) {
	if since.IsZero() {
		return "", false
	}
	d := max(now.Sub(since), 0)
	limit, ok := staleAfter[status]
	stale = ok && d > limit
	const day = 24 * time.Hour
	switch {
	case d < time.Minute:
		text = "now"
	case d < time.Hour:
		text = strconv.Itoa(int(d/time.Minute)) + "m"
	case d < day:
		text = strconv.Itoa(int(d/time.Hour)) + "h"
	case d < 2*day:
		text = "yesterday"
	case d < 30*day:
		text = strconv.Itoa(int(d/day)) + "d"
	default:
		local := since.Local()
		if local.Year() == now.Local().Year() {
			text = local.Format("Jan 2")
		} else {
			text = local.Format("Jan 2, 2006")
		}
	}
	if stale {
		text += " · stale"
	}
	return text, stale
}

// exactTime is a moment to the minute, in local time: "Oct 19, 09:12".
func exactTime(t time.Time) string {
	return t.Local().Format("Jan 2, 15:04")
}

// lastMove is a task's latest status move, as the contract's Ticket JSON
// carries it.
type lastMove struct {
	Status       string
	By           string // "person", "architect" or "session"
	SessionLabel string
	Reason       string
	At           time.Time
}

// lastMoveLine says who put the task in its column: "Moved here by" you, the
// architect or the session's label; "Created by you" when it never moved.
func lastMoveLine(m *lastMove) (lead, who, reason string) {
	if m == nil {
		return "Created by", "you", ""
	}
	switch {
	case m.By == "person":
		who = "you"
	case m.By == "architect":
		who = "architect"
	case m.SessionLabel != "":
		who = m.SessionLabel
	default:
		who = "a session"
	}
	return "Moved here by", who, m.Reason
}

// mergeState is whether a task's branches landed, as the contract's Ticket
// JSON carries it.
type mergeState struct {
	State         string
	Merged, Total int
}

// MergeBadge is a Review or Done card's merge badge: its state (the
// data-merge-state hook), the card's word and the rail's lower-case word.
type MergeBadge struct {
	State, Word, Rail string
}

// mergeBadge is nil when there is nothing to show: a status without the
// badge, no merge state, "none", or a state this build does not know.
func mergeBadge(status string, m *mergeState) *MergeBadge {
	if m == nil || (status != "review" && status != "done") {
		return nil
	}
	switch m.State {
	case "merged":
		return &MergeBadge{State: m.State, Word: "Merged", Rail: "merged"}
	case "partial":
		n, of := strconv.Itoa(m.Merged), strconv.Itoa(m.Total)
		return &MergeBadge{State: m.State, Word: n + " of " + of + " merged", Rail: n + "/" + of + " merged"}
	case "not_merged":
		return &MergeBadge{State: m.State, Word: "Not merged", Rail: "not merged"}
	case "unknown":
		return &MergeBadge{State: m.State, Word: "Unknown", Rail: "unknown"}
	}
	return nil
}
