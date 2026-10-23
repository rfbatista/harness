// Package markdown renders what agents and people write as GitHub-flavoured
// Markdown, the one way the harness's inbound adapters turn Markdown into
// HTML: the web UI's documents and the transcript API share it, so an agent's
// text looks the same wherever it shows.
package markdown

import (
	"bytes"
	"html"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// renderer is goldmark's safe mode with the GFM extensions: raw HTML in the
// source is left out and links with a dangerous scheme (javascript: and the
// like) lose their href, so what an agent writes cannot run script in a page.
var renderer = goldmark.New(goldmark.WithExtensions(extension.GFM))

// HTML is source rendered as HTML, ready to place in a page.
func HTML(source string) string {
	var buf bytes.Buffer
	if err := renderer.Convert([]byte(source), &buf); err != nil {
		// Convert only fails on a writer error; show the text as written.
		return Text(source)
	}
	return buf.String()
}

// Text is source as written: preformatted and escaped, never parsed.
func Text(source string) string {
	return "<pre>" + html.EscapeString(source) + "</pre>"
}
