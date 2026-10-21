package markdown

import (
	"strings"
	"testing"
)

func TestHTMLRendersGitHubFlavouredMarkdown(t *testing.T) {
	got := HTML("# Done\n\n- [x] fixed\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n```go\nx := 1\n```\n\n~~old~~ `code`")
	for _, want := range []string{"<h1>Done</h1>", `<input checked="" disabled="" type="checkbox"`, "<table>", `<code class="language-go">x := 1`, "<del>old</del>", "<code>code</code>"} {
		if !strings.Contains(got, want) {
			t.Errorf("HTML misses %q in:\n%s", want, got)
		}
	}
}

func TestHTMLNeverPassesScriptThrough(t *testing.T) {
	got := HTML("<img src=x onerror=alert(1)>\n\n<script>alert(1)</script>\n\n[click](javascript:alert(1))")
	for _, bad := range []string{"<img", "<script", "onerror", "javascript:"} {
		if strings.Contains(got, bad) {
			t.Errorf("HTML passed %q through:\n%s", bad, got)
		}
	}
}

func TestTextEscapes(t *testing.T) {
	if got := Text("<b>x</b>"); got != "<pre>&lt;b&gt;x&lt;/b&gt;</pre>" {
		t.Errorf("Text = %q", got)
	}
}
