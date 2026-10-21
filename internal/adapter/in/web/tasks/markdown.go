package tasks

import (
	"github.com/a-h/templ"

	"operators-mcp/internal/adapter/in/markdown"
)

// renderMarkdown is source as HTML, ready to place in the page (see the
// markdown package for what it keeps out).
func renderMarkdown(source string) templ.Component {
	return templ.Raw(markdown.HTML(source))
}

// renderText is source as written: preformatted and escaped, never parsed.
func renderText(source string) templ.Component {
	return templ.Raw(markdown.Text(source))
}
