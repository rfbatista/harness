package sessions

import (
	"strconv"

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

// bandAttrs paints the review band before the browser reads the requests:
// amber when some wait on the person, hidden when none do.
func bandAttrs(pending int) templ.Attributes {
	if pending > 0 {
		return templ.Attributes{"data-attention": true}
	}
	return templ.Attributes{"hidden": true}
}

// waitingLine is how many requests wait: "1 review waits on you". The
// browser's waitingLine (reviews/presentation/view.js) says the same.
func waitingLine(n int) string {
	if n == 1 {
		return "1 review waits on you"
	}
	return strconv.Itoa(n) + " reviews wait on you"
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
