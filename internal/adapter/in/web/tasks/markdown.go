package tasks

import (
	"bytes"
	"context"
	"io"

	"github.com/a-h/templ"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// markdown renders documents as GitHub-flavoured Markdown. It is goldmark's
// safe mode: raw HTML in a document is left out and links with a dangerous
// scheme (javascript: and the like) lose their href, so what an agent writes
// cannot run script in the page.
var markdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// renderMarkdown is source as HTML, ready to place in the page.
func renderMarkdown(source string) templ.Component {
	var buf bytes.Buffer
	if err := markdown.Convert([]byte(source), &buf); err != nil {
		// Convert only fails on a writer error; show the text as written.
		return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, "<pre>"+templ.EscapeString(source)+"</pre>")
			return err
		})
	}
	return templ.Raw(buf.String())
}
