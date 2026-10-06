package tasks

import "github.com/a-h/templ"

// selectedIf marks the option of the status a card has.
func selectedIf(selected bool) templ.Attributes {
	if selected {
		return templ.Attributes{"selected": true}
	}
	return templ.Attributes{}
}
