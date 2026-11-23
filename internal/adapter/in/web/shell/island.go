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
// belongs to, as JSON that is safe inside a script.
func (f Frame) OwnerPanelProps() string {
	props, _ := json.Marshal(struct {
		ProjectID string `json:"project_id"`
	}{f.Top.Current})
	return string(props)
}
