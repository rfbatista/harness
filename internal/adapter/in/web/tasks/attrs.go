package tasks

import "github.com/a-h/templ"

// selectedIf marks the option of the status a card has.
func selectedIf(selected bool) templ.Attributes {
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
