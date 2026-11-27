package domain

import (
	"strings"
)

// Visual QA (Glossary "Visual QA"): a qa-mode session that walks a repository's
// declared UI in a browser and judges what it sees.

// VisualQABrief is what a qa-mode session's opening adds when its repository
// declares a UI: the app to start, where it is, what to walk and how to report.
func VisualQABrief(ui VisualQAUI) string {
	var b strings.Builder
	b.WriteString("This is a visual QA pass: the repository declares its UI, so you may start the app yourself.\n")
	b.WriteString("Start it through the services tools with the declared command; every create, start and stop is a service request the person approves, so give a `reason` each time:\n")
	b.WriteString("- start command: `" + ui.StartCommand + "`\n")
	b.WriteString("- URL: " + ui.URL + "\n")
	b.WriteString("- routes: " + listOrNone(ui.Routes) + "\n")
	b.WriteString("- viewports (width x height): " + listOrNone(ui.Viewports) + "\n")
	if len(ui.UIPaths) > 0 {
		b.WriteString("- UI paths: " + strings.Join(ui.UIPaths, ", ") + "\n")
	}
	b.WriteString("\nFor each route at each viewport, resize the browser to the viewport, open the route and publish one screenshot into task/designs/, " +
		"named `<route>-<width>x<height>.png`. Judge each screenshot with your own vision against the design assets attached to this task " +
		"(task/designs/attached.md and project/designs/); when there are none, against the screenshots of the previous visual QA pass " +
		"(its folder or files in task/designs/). Note what differs: layout, text, state, overflow, a broken or empty screen.\n")
	b.WriteString("\nWrite one document titled `Visual QA: <task title>` in task/docs/: one entry per route and viewport with its finding " +
		"(route, viewport, the reason) and `pass` or `fail`, and an overall verdict `pass` or `fail` on its own line. " +
		"When you are done, ask for the service to be stopped with a stop service request, and stop nothing else.")
	return b.String()
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none declared"
	}
	return strings.Join(items, ", ")
}
