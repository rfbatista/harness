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
