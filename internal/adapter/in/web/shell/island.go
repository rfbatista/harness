package shell

import (
	"context"
	"encoding/json"

	"github.com/a-h/templ"
)

// TopBarIsland is the React top bar as a page carries it: the HTML the server
// rendered, and the island props (the top-bar model as JSON) the browser takes
// over from. Failed is a render that did not happen (it threw, timed out, or
// the bundles are not built): the mount is then empty and the browser draws it.
type TopBarIsland struct {
	HTML   string
	Props  string
	Failed bool
}

// RailIsland is the React rail as a page carries it: the HTML the server
// rendered and the island props (the seed and the navigation as JSON). Failed
// is a render that did not happen: the mount is then empty and the browser
// draws it.
type RailIsland struct {
	HTML   string
	Props  string
	Failed bool
}

// RailProps are the rail island's props: the rail seed (the project's tasks and
// sessions, which the headless feed owner reads too) with what else the rail
// draws. Fields keep their wire names (web/app/src/features/rail).
type RailProps struct {
	*RailSeed
	ProjectName string   `json:"project_name"`
	Current     string   `json:"current_task"`
	Page        string   `json:"page"`
	Collapsed   []string `json:"collapsed"`
	ReportsFeed bool     `json:"reports_feed"`
	Nav         RailNav  `json:"nav"`
}

// RailNav is the project's pages, as the rail links them; "" leaves a page out.
type RailNav struct {
	NewTaskHref           string `json:"new_task_href"`
	BoardHref             string `json:"board_href"`
	CodeHref              string `json:"code_href"`
	ServicesHref          string `json:"services_href"`
	ServicesProjectID     string `json:"services_project_id"`
	ServicesRunning       int    `json:"services_running"`
	ReviewsHref           string `json:"reviews_href"`
	DocumentsHref         string `json:"documents_href"`
	DesignHref            string `json:"design_href"`
	DesignSystemHref      string `json:"design_system_href"`
	DesignSystemProposals int    `json:"design_system_proposals"`
	UsageHref             string `json:"usage_href"`
	ProductOwnerHref      string `json:"product_owner_href"`
}

// RailProps builds the rail island's props from the frame.
func (f Frame) RailProps() RailProps {
	r := f.Rail
	collapsed := r.Collapsed
	if collapsed == nil {
		collapsed = []string{}
	}
	return RailProps{
		RailSeed:    r.Seed,
		ProjectName: f.ProjectName(),
		Current:     r.Current,
		Page:        r.Page,
		Collapsed:   collapsed,
		ReportsFeed: r.ReportsFeed,
		Nav: RailNav{
			NewTaskHref: r.NewTaskHref, BoardHref: r.BoardHref, CodeHref: r.CodeHref,
			ServicesHref: r.Services.Href, ServicesProjectID: r.Services.ProjectID, ServicesRunning: r.Services.Running,
			ReviewsHref: r.ReviewsHref, DocumentsHref: r.DocumentsHref, DesignHref: r.DesignHref,
			DesignSystemHref: r.DesignSystemHref, DesignSystemProposals: r.DesignSystemProposals,
			UsageHref: r.UsageHref, ProductOwnerHref: r.ProductOwnerHref,
		},
	}
}

// railPropsScript is the rail island's props, in the page. It is also the rail
// seed the headless feed owner reads (data-seed="rail-seed"): the props carry
// the seed's fields at the top, so the project's tasks are sent once.
func railPropsScript(props string) templ.Component {
	return templ.Raw(`<script type="application/json" id="rail-seed" data-island-props="rail">` + props + `</script>`)
}

// islandRenderedAttrs marks a mount whose server render failed.
func islandRenderedAttrs(failed bool) templ.Attributes {
	if failed {
		return templ.Attributes{"data-island-rendered": "false"}
	}
	return nil
}

// propsScript is the island's props, in the page: a JSON script that the
// browser reads (never run). Props is json.Marshal's output, which escapes
// <, > and & so it cannot close the script.
func propsScript(name, props string) templ.Component {
	return PropsScript(name, props)
}

// PropsScript is an island's props, in the page, for the pages that carry an island of their own.
func PropsScript(name, props string) templ.Component {
	return templ.Raw(`<script type="application/json" data-island-props="` + name + `">` + props + `</script>`)
}

type pathKey struct{}

// WithPath records the request's URL path on ctx: the page the top bar models.
func WithPath(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, pathKey{}, path)
}

// PathOf is the URL path of the request ctx belongs to.
func PathOf(ctx context.Context) string {
	p, _ := ctx.Value(pathKey{}).(string)
	return p
}

// OwnerPanelProps are the product owner panel island's props: the project it
// belongs to, its name, and whether it has no tasks yet (the thread's empty
// state says so), as JSON that is safe inside a script.
func (f Frame) OwnerPanelProps() string {
	name := f.Top.Current
	for _, p := range f.Top.Projects {
		if p.ID == f.Top.Current {
			name = p.Name
		}
	}
	props, _ := json.Marshal(struct {
		ProjectID   string `json:"project_id"`
		ProjectName string `json:"project_name"`
		NoTasks     bool   `json:"no_tasks"`
	}{f.Top.Current, name, f.Rail.Empty != ""})
	return string(props)
}
