package ports

import "context"

// TopBar is the top bar's model for one page path (see "Contract: Harness
// server ↔ Web UI — the top bar island"): the island's props on a page, and
// the answer of GET /api/top_bar.
type TopBar struct {
	Path string `json:"path"`
	// Projects are sorted by name, case-insensitive; never nil.
	Projects []TopBarProject `json:"projects"`
	// CurrentProjectID is "" on a page that belongs to no project.
	CurrentProjectID  string `json:"current_project_id"`
	SwitchProjectHref string `json:"switch_project_href"`
	// Links holds only the pages the server serves, in the order the bar
	// shows them.
	Links []TopBarLink `json:"links"`
	// Notifications is nil when the inbox is not served: no bell.
	Notifications *TopBarBell `json:"notifications"`
}

// TopBarProject is one entry of the project picker.
type TopBarProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TopBarLink is one page of the top bar. Key names it; the client owns the
// label and icon.
type TopBarLink struct {
	Key  string `json:"key"`
	Href string `json:"href"`
	// Current marks the page that is open; Attention puts the mark on the link
	// (something there blocks agents from starting).
	Current   bool `json:"current"`
	Attention bool `json:"attention"`
}

// TopBarBell is the way to the inbox. Page is the latest notifications, nil
// when they could not be read: the client then reads them.
type TopBarBell struct {
	InboxHref string            `json:"inbox_href"`
	Page      *NotificationPage `json:"page"`
}

// TopBars builds the top bar's model for a page path, read fresh on each call.
type TopBars interface {
	TopBar(ctx context.Context, path string) (*TopBar, error)
}
