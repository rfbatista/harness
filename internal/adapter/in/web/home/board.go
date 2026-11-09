package home

import (
	"github.com/a-h/templ"
	"net/url"
	"strconv"
	"time"

	"operators-mcp/internal/adapter/in/web/shell"
	"operators-mcp/internal/domain"
)

// The rules below are the Go twin of web/src/modules/agentboard
// (domain/board.js and presentation/view.js).

// collapseAt is how many session lines a card shows before "+N more".
const collapseAt = 3

// BoardView is what the Agent board page renders. Seed is nil on a page
// swapped in place: the browser reads it and draws the cards itself.
type BoardView struct {
	Frame    shell.Frame
	Seed     *domain.BoardSeed
	Summary  string
	Sections []SectionView
	// Quiet is the footer line ("5 quiet projects have no live session."),
	// "" when every project has a card.
	Quiet string
}

// SectionView is one project's cards.
type SectionView struct {
	ID, Name, Href string
	// Needs is the "2 need you" badge, "" when no card needs the person.
	Needs string
	Count string
	Cards []CardView
}

// CardView is one task with live sessions.
type CardView struct {
	TaskID, Href, Title, Feature string
	Status                       string
	NeedsYou                     bool
	Active, LiveWord             string
	Lines, Hidden                []LineView
}

// LineView is one live session on a card.
type LineView struct {
	SessionID, Href, Agent, Dot, Word, Tone, Time, WaitingFor, Hover string
	Chip                                                             bool
}

type statusLook struct {
	dot, word, tone string
	chip            bool
	rank            int
}

var looks = map[domain.SessionStatus]statusLook{
	domain.SessionStarting:        {"queued", "starting", "", true, 3},
	domain.SessionRunning:         {"running", "running", "", false, 3},
	domain.SessionThinking:        {"running", "thinking", "signal", true, 3},
	domain.SessionIdle:            {"waiting", "your turn", "attention", true, 2},
	domain.SessionWaitingApproval: {"waiting", "approval", "attention", true, 0},
	domain.SessionPaused:          {"idle", "paused", "", true, 3},
}

// lookOf is the look of a session line: its status's, or questions while an
// interview waits and no approval does.
func lookOf(s domain.BoardSessionLine) statusLook {
	look, ok := looks[s.Status]
	if !ok {
		look = statusLook{dot: "idle", word: string(s.Status), chip: true, rank: 3}
	}
	if s.OpenInterviewCount > 0 && s.Status != domain.SessionWaitingApproval {
		// The chip and its place are the interview's; the dot stays the session's own.
		look.word, look.tone, look.chip, look.rank = "questions", "attention", true, 1
	}
	return look
}

var taskStatusWords = map[string]string{
	"backlog": "Backlog", "todo": "Todo", "in_progress": "In progress", "review": "Review", "done": "Done",
}

// NewBoardView lays the seed out as the page shows it.
func NewBoardView(frame shell.Frame, seed *domain.BoardSeed, now time.Time) BoardView {
	v := BoardView{Frame: frame, Seed: seed}
	if seed == nil {
		return v
	}
	cards, lives, needs := 0, 0, 0
	for _, p := range seed.Projects {
		sec := SectionView{ID: p.ProjectID, Name: p.ProjectName, Href: "/projects/" + url.PathEscape(p.ProjectID)}
		sectionNeeds, sectionLive := 0, 0
		for _, c := range p.Cards {
			if c.NeedsYou {
				sectionNeeds++
			}
			sectionLive += len(c.Sessions)
			sec.Cards = append(sec.Cards, newCard(c, now))
		}
		if sectionNeeds > 0 {
			sec.Needs = strconv.Itoa(sectionNeeds) + " " + plural(sectionNeeds, "needs", "need") + " you"
		}
		sec.Count = strconv.Itoa(len(p.Cards)) + " " + plural(len(p.Cards), "task", "tasks") + " · " + strconv.Itoa(sectionLive) + " live"
		cards += len(p.Cards)
		lives += sectionLive
		needs += sectionNeeds
		v.Sections = append(v.Sections, sec)
	}
	if cards == 0 {
		v.Summary = "No live sessions right now."
	} else {
		v.Summary = strconv.Itoa(cards) + " " + plural(cards, "task", "tasks") + " with " + strconv.Itoa(lives) + " live " +
			plural(lives, "session", "sessions") + " in " + strconv.Itoa(len(seed.Projects)) + " " + plural(len(seed.Projects), "project", "projects")
	}
	if needs > 0 {
		v.Summary += " · " + strconv.Itoa(needs) + " need you"
	}
	if seed.QuietProjects > 0 {
		v.Quiet = strconv.Itoa(seed.QuietProjects) + " quiet " + plural(seed.QuietProjects, "project has", "projects have") + " no live session."
	}
	return v
}

// NeedsCount is the part of the summary the page paints in the attention tone.
func (v BoardView) NeedsCount() int {
	n := 0
	if v.Seed == nil {
		return 0
	}
	for _, p := range v.Seed.Projects {
		for _, c := range p.Cards {
			if c.NeedsYou {
				n++
			}
		}
	}
	return n
}

// SummaryLead is the summary without the needs-you part.
func (v BoardView) SummaryLead() string {
	n := v.NeedsCount()
	if n == 0 {
		return v.Summary
	}
	return v.Summary[:len(v.Summary)-len(" · "+strconv.Itoa(n)+" need you")]
}

func newCard(c domain.BoardCard, now time.Time) CardView {
	taskHref := "/projects/" + url.PathEscape(c.ProjectID) + "/tasks/" + url.PathEscape(c.TaskID)
	cv := CardView{
		TaskID: c.TaskID, Href: taskHref, Title: c.TaskTitle,
		Status: taskStatusWords[c.TaskStatus], NeedsYou: c.NeedsYou,
		Active:   "active " + relative(c.LastStatusChangeAt, now),
		LiveWord: strconv.Itoa(len(c.Sessions)) + " live " + plural(len(c.Sessions), "session", "sessions"),
	}
	if c.FeatureTitle != nil {
		cv.Feature = *c.FeatureTitle
	}
	lines := orderLines(c.Sessions)
	for i, s := range lines {
		l := newLine(s, taskHref, now)
		if i < collapseAt {
			cv.Lines = append(cv.Lines, l)
		} else {
			cv.Hidden = append(cv.Hidden, l)
		}
	}
	return cv
}

// orderLines puts approvals first, then questions, then your turn, then the
// rest in the order the server sent (a stable sort).
func orderLines(in []domain.BoardSessionLine) []domain.BoardSessionLine {
	out := make([]domain.BoardSessionLine, 0, len(in))
	for rank := 0; rank <= 3; rank++ {
		for _, s := range in {
			if lookOf(s).rank == rank {
				out = append(out, s)
			}
		}
	}
	return out
}

func newLine(s domain.BoardSessionLine, taskHref string, now time.Time) LineView {
	look := lookOf(s)
	l := LineView{
		SessionID: s.SessionID, Href: taskHref + "?session=" + url.QueryEscape(s.SessionID), Agent: s.AgentName,
		Dot: look.dot, Word: look.word, Tone: look.tone, Chip: look.chip, Time: relative(s.StatusSince, now),
	}
	if s.WaitingFor != nil {
		l.WaitingFor = *s.WaitingFor
	}
	l.Hover = hoverTitle(s, look.word, l.Time, l.WaitingFor)
	return l
}

// hoverTitle is the line's title: the agent, its role, mode and model, the
// status and how long ago it began, and what an approval asks for.
func hoverTitle(s domain.BoardSessionLine, word, since, waiting string) string {
	parts := []string{s.AgentName}
	for _, p := range []string{s.Role, s.Mode, s.Model} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	parts = append(parts, word)
	if waiting != "" {
		parts = append(parts, waiting)
	}
	parts = append(parts, since)
	out := parts[0]
	for _, p := range parts[1:] {
		out += " · " + p
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// relative is "now", "4m", "3h", "2d" (web/src/shared/presentation/format.js).
func relative(then, now time.Time) string {
	d := now.Sub(then)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + "d"
}

func itoa(n int) string { return strconv.Itoa(n) }

// needsAttrs marks a card that waits on the person.
func needsAttrs(needs bool) templ.Attributes {
	if needs {
		return templ.Attributes{"data-needs-you": true}
	}
	return templ.Attributes{}
}

// toneAttrs gives a chip its tone; a neutral chip has none.
func toneAttrs(tone string) templ.Attributes {
	if tone == "" {
		return templ.Attributes{}
	}
	return templ.Attributes{"data-tone": tone}
}
