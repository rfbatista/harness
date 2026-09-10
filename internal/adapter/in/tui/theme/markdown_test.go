package theme

import "testing"

func TestMarkdownRendersHeadingsAndLists(t *testing.T) {
	out := Dark().Markdown("# Title\n\n- one\n- two\n", 60)
	if !contains(out, "Title") || !contains(out, "one") || !contains(out, "two") {
		t.Fatalf("markdown output missing content:\n%s", out)
	}
	if contains(out, "# Title") {
		t.Fatalf("heading marker should be rendered away:\n%s", out)
	}
}

func TestMarkdownFallsBackToRawOnEmptyWidth(t *testing.T) {
	if out := Light().Markdown("plain", 0); !contains(out, "plain") {
		t.Fatalf("fallback: %q", out)
	}
}
