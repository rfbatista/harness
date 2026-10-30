package gitsync

import "github.com/a-h/templ"

// oldAttrs marks counts over an hour behind (data-old, valueless).
func oldAttrs(old bool) templ.Attributes {
	if !old {
		return nil
	}
	return templ.Attributes{"data-old": true}
}

// class is a chip part's class: in CUBE brackets where its browser twin writes them.
func class(bracketed bool, name string) string {
	if bracketed {
		return "[ " + name + " ]"
	}
	return name
}
