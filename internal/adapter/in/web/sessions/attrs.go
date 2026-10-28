package sessions

import (
	"strconv"
	"strings"

	"github.com/a-h/templ"
)

// attentionAttrs sets data-attention on a row waiting on the developer and
// leaves it off otherwise (the .row block's exception keys on presence).
func attentionAttrs(attention bool) templ.Attributes {
	if attention {
		return templ.Attributes{"data-attention": true}
	}
	return templ.Attributes{}
}

// roleAttrs places a row in the architect's group: data-role on the
// architect and its delegates, data-depth on a delegate (the .row block
// indents by it). A peer gets neither, as before.
// unsandboxedAttrs marks the row of a session that ran without the sandbox.
func unsandboxedAttrs(unsandboxed bool) templ.Attributes {
	if unsandboxed {
		return templ.Attributes{"data-unsandboxed": true}
	}
	return templ.Attributes{}
}

// leadAttrs marks a row that leads with its title, not its agent: a feature
// brainstorm, which has no task.
func leadAttrs(leadsWithTitle bool) templ.Attributes {
	if leadsWithTitle {
		return templ.Attributes{"data-lead": "title"}
	}
	return templ.Attributes{}
}

// chipToneAttrs paints a status chip's tone; neutral ("") has none.
func chipToneAttrs(tone string) templ.Attributes {
	if tone != "" {
		return templ.Attributes{"data-tone": tone}
	}
	return templ.Attributes{}
}

func roleAttrs(role string, depth int) templ.Attributes {
	a := templ.Attributes{}
	if role != "" {
		a["data-role"] = role
	}
	if depth > 0 {
		a["data-depth"] = strconv.Itoa(depth)
	}
	return a
}

// selectedAttrs marks the current option of a <select>.
func selectedAttrs(selected bool) templ.Attributes {
	if selected {
		return templ.Attributes{"selected": true}
	}
	return templ.Attributes{}
}

// descriptionAttrs gives an option its second line in a Listbox select
// (data-description), and leaves it off when there is none.
func descriptionAttrs(description string) templ.Attributes {
	if description != "" {
		return templ.Attributes{"data-description": description}
	}
	return templ.Attributes{}
}

// reviewsButtonAttrs paints the title row's Reviews button before the browser
// reads the requests: named with the count when some wait on the person,
// hidden when none do.
func reviewsButtonAttrs(pending int) templ.Attributes {
	if pending > 0 {
		return templ.Attributes{"aria-label": "Reviews, " + strconv.Itoa(pending) + " waiting"}
	}
	return templ.Attributes{"style": "display: none"}
}

// waitingLine is how many requests wait: "1 review waits on you". The
// browser's waitingLine (reviews/presentation/view.js) says the same.
func waitingLine(n int) string {
	if n == 1 {
		return "1 review waits on you"
	}
	return strconv.Itoa(n) + " reviews wait on you"
}

// hiddenUnless paints an element hidden until the live page shows it.
func hiddenUnless(shown bool) templ.Attributes {
	if shown {
		return templ.Attributes{}
	}
	return templ.Attributes{"hidden": true}
}

// shownIf hides an element the server knows is empty until Alpine shows it
// (x-show sets display itself, so the hidden attribute would fight it).
func shownIf(shown bool) templ.Attributes {
	if shown {
		return templ.Attributes{}
	}
	return templ.Attributes{"style": "display: none"}
}

// featureCount is the feature section's badge: how many children the server
// painted ("0" with none; the browser keeps it current).
func featureCount(f *FeatureView) string {
	if f == nil {
		return "0"
	}
	return strconv.Itoa(len(f.Children))
}

// hiddenIf paints an element hidden until the browser decides to show it
// (x-bind:hidden), like shownIf but for the hidden attribute.
func hiddenIf(hidden bool) templ.Attributes {
	if hidden {
		return templ.Attributes{"hidden": true}
	}
	return templ.Attributes{}
}

// doneAttr marks a child row whose task is done: its title is muted.
func doneAttr(done bool) templ.Attributes {
	if done {
		return templ.Attributes{"data-done": true}
	}
	return templ.Attributes{}
}

// featurePhrase is what the Feature button's accessible name adds to the count:
// "3 in review · 2 in progress", "all done", or nothing. The browser's
// featureSummary (tasks/presentation/featureView.js) says the same.
func featurePhrase(f *FeatureView) string {
	if f == nil || len(f.Children) == 0 {
		return ""
	}
	count := map[string]int{}
	for _, c := range f.Children {
		count[c.Status]++
	}
	var parts []string
	if n := count["review"]; n > 0 {
		parts = append(parts, strconv.Itoa(n)+" in review")
	}
	if n := count["in_progress"]; n > 0 {
		parts = append(parts, strconv.Itoa(n)+" in progress")
	}
	if len(parts) == 0 && count["done"] == len(f.Children) {
		parts = append(parts, "all done")
	}
	return strings.Join(parts, " · ")
}

// featureName is the Feature button's accessible name, which a narrow strip
// keeps whole while it hides the phrase: "Feature, 16 children, 3 in review,
// 2 in progress". The browser's featureSummary says the same.
func featureName(f *FeatureView) string {
	n := len(childrenOf(f))
	name := "Feature, " + strconv.Itoa(n) + " children"
	if n == 1 {
		name = "Feature, 1 child"
	}
	if phrase := featurePhrase(f); phrase != "" {
		name += ", " + strings.ReplaceAll(phrase, " · ", ", ")
	}
	return name
}

// featureFoot is the Feature popover's foot before the browser takes over:
// how many children are in each status, in the popover's order. The
// browser's statusLine (tasks/presentation/featureView.js) says the same.
func featureFoot(f *FeatureView) string {
	count := map[string]int{}
	for _, c := range childrenOf(f) {
		count[c.Status]++
	}
	var parts []string
	for _, st := range []struct{ status, word string }{{"review", "in review"}, {"in_progress", "in progress"}, {"todo", "todo"}, {"backlog", "backlog"}, {"done", "done"}} {
		if n := count[st.status]; n > 0 {
			parts = append(parts, strconv.Itoa(n)+" "+st.word)
		}
	}
	return strings.Join(parts, " · ")
}

// featureButtonAttrs hides the Feature button until the task has children.
func featureButtonAttrs(f *FeatureView) templ.Attributes {
	if len(childrenOf(f)) == 0 {
		return templ.Attributes{"style": "display: none"}
	}
	return templ.Attributes{}
}

// featureScrollLabel names the Feature panel's scroll region: "Child tasks, 16".
func featureScrollLabel(f *FeatureView) string {
	return "Child tasks, " + strconv.Itoa(len(childrenOf(f)))
}

func childrenOf(f *FeatureView) []ChildView {
	if f == nil {
		return nil
	}
	return f.Children
}

// docCount is a child row's document count in words; nothing for none.
func docCount(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return "1 doc"
	}
	return strconv.Itoa(n) + " docs"
}
