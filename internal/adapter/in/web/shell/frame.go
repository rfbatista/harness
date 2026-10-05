// Package shell renders what every web page shares: the document head, the
// top bar with the project picker, the task rail, and the stream bar. Pages
// fill the <main> it leaves open.
package shell

import (
	"context"
	"net/http"

	"github.com/a-h/templ"
)

// Frame is what the shell needs from a page.
type Frame struct {
	Title string
	Top   TopBar
	Rail  Rail
	// Asset URLs, content-hashed (web.Assets.URL).
	CSS, JS string
	// Built is false when `make web` has not produced the bundles yet.
	Built bool
	// Live: the page follows the session feed, so the stream bar shows the
	// connection. Pages that do not follow it show none, rather than a
	// "connecting" that never ends.
	Live bool
}

// TopBar holds the project picker.
type TopBar struct {
	Projects []Option
	Current  string // project ID; "" when none is selected
}

// Option is one project in the picker.
type Option struct{ ID, Name string }

// Rail is the current project's tasks, in kanban groups.
type Rail struct {
	// NewTaskHref is the new-task page of the selected project; "" with none.
	NewTaskHref string
	Groups      []RailGroup
	// Empty is shown when a project is selected but has no tasks.
	Empty string
	// Seed lets the browser keep the dots and counts live: the project's
	// sessions, followed over the project's feed. Nil leaves the rail static.
	Seed *RailSeed
}

// RailSeed is the project's sessions, as much as the rail needs of each (the
// API's JSON shape). The tasks module's rail gateway decodes it.
type RailSeed struct {
	ProjectID string        `json:"project_id"`
	Sessions  []RailSession `json:"sessions"`
}

// RailSession is a session as the rail counts it.
type RailSession struct {
	ID               string `json:"id"`
	TicketID         string `json:"ticket_id,omitempty"`
	Status           string `json:"status"`
	PendingApprovals int    `json:"pending_approvals"`
}

// RailGroup is one kanban column: "in progress", "todo", …
type RailGroup struct {
	Label string
	Links []Link
}

// Link is one task in the rail.
type Link struct {
	// TaskID ties the link to the live activity; "" keeps it static.
	TaskID  string
	Label   string
	Href    string
	Current bool
	// Live counts the task's sessions whose process is alive.
	Live int
	// Attention: one of its sessions is waiting on the developer.
	Attention bool
}

// DocumentTitle is the <title>: the page's own title, then the app.
func (f Frame) DocumentTitle() string {
	if f.Title == "" {
		return "harness"
	}
	return f.Title + " · harness"
}

// Layout builds a page's Frame: assets, the project picker and the task rail.
// projectID and taskID say what is selected ("" for none).
type Layout func(ctx context.Context, title, projectID, taskID string) (Frame, error)

// Handler is a page handler. Returning an error renders the error page for
// its StructuredError code (a *_NOT_FOUND is a 404).
type Handler func(w http.ResponseWriter, r *http.Request) error

// Renderer writes a page with a status code.
type Renderer func(w http.ResponseWriter, r *http.Request, status int, page templ.Component) error

// currentAttrs marks the current link with aria-current="page" and adds
// nothing to the others.
func currentAttrs(current bool) templ.Attributes {
	if current {
		return templ.Attributes{"aria-current": "page"}
	}
	return templ.Attributes{}
}

// selectedAttrs marks the picker's current option.
func selectedAttrs(selected bool) templ.Attributes {
	if selected {
		return templ.Attributes{"selected": true}
	}
	return templ.Attributes{}
}

// liveState is the rail's status dot: amber when a session waits on the
// developer, teal when one is running, none when the task is quiet.
func (l Link) liveState() string {
	switch {
	case l.Attention:
		return "waiting"
	case l.Live > 0:
		return "running"
	default:
		return ""
	}
}

// liveWord is the dot's accessible word.
func (l Link) liveWord() string {
	switch {
	case l.Attention:
		return "waiting on you"
	case l.Live > 0:
		return "running"
	default:
		return ""
	}
}

// railAttrs makes the rail live when it has a seed.
func railAttrs(r Rail) templ.Attributes {
	if r.Seed == nil {
		return nil
	}
	return templ.Attributes{"x-data": "tasksRail", "data-seed": "rail-seed"}
}

// hiddenUnless hides an element x-show controls until it has something to
// show, so the first paint matches what Alpine will render.
func hiddenUnless(show bool) templ.Attributes {
	if show {
		return nil
	}
	return templ.Attributes{"style": "display: none"}
}
