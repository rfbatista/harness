package sessions

import "github.com/a-h/templ"

// attentionAttrs sets data-attention on a row waiting on the developer and
// leaves it off otherwise (the .row block's exception keys on presence).
func attentionAttrs(attention bool) templ.Attributes {
	if attention {
		return templ.Attributes{"data-attention": true}
	}
	return templ.Attributes{}
}

// selectedAttrs marks the current option of a <select>.
func selectedAttrs(selected bool) templ.Attributes {
	if selected {
		return templ.Attributes{"selected": true}
	}
	return templ.Attributes{}
}
