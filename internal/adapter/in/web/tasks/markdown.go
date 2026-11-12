package tasks

import (
	"github.com/a-h/templ"

	"operators-mcp/internal/adapter/in/markdown"
)

// renderMarkdown is source as HTML, ready to place in the page (see the
// markdown package for what it keeps out), its images resolved by resolve.
func renderMarkdown(source string, resolve func(string) (string, bool)) templ.Component {
	return templ.Raw(markdown.HTMLWithImages(source, resolve))
}

// renderText is source as written: preformatted and escaped, never parsed.
func renderText(source string) templ.Component {
	return templ.Raw(markdown.Text(source))
}
