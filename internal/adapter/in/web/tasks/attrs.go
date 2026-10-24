package tasks

import "github.com/a-h/templ"

// selectedIf marks the option of the status a card has.
func selectedIf(selected bool) templ.Attributes {
	if selected {
		return templ.Attributes{"selected": true}
	}
	return templ.Attributes{}
}

// waitingAttrs marks Review while it holds tasks waiting for the person's check.
func waitingAttrs(waiting bool) templ.Attributes {
	if waiting {
		return templ.Attributes{"data-waiting": true}
	}
	return templ.Attributes{}
}

// staleAttrs marks a card's age past its column's stale threshold.
func staleAttrs(stale bool) templ.Attributes {
	if stale {
		return templ.Attributes{"data-stale": true}
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
