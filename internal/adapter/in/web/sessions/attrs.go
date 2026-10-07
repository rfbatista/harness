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
