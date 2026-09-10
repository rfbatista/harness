package unit

import (
	"strings"
	"testing"

	"operators-mcp/internal/application/tooling"
)

func TestExactReplace_SingleOccurrence(t *testing.T) {
	content := "func main() {\n\tfmt.Println(\"hello\")\n}\n"
	result, ok := tooling.ExactReplace(content, `fmt.Println("hello")`, `fmt.Println("world")`, 1)
	if !ok {
		t.Fatal("expected match")
	}
	if !strings.Contains(result, `fmt.Println("world")`) {
		t.Errorf("expected replacement, got:\n%s", result)
	}
	if strings.Contains(result, `fmt.Println("hello")`) {
		t.Error("old string should not remain")
	}
}

func TestExactReplace_MultipleOccurrences_CountOne(t *testing.T) {
	content := "a = 1\nb = 1\nc = 1\n"
	result, ok := tooling.ExactReplace(content, "1", "2", 1)
	if !ok {
		t.Fatal("expected match")
	}
	if strings.Count(result, "2") != 1 {
		t.Errorf("expected exactly 1 replacement, got:\n%s", result)
	}
	if strings.Count(result, "1") != 2 {
		t.Errorf("expected 2 remaining '1's, got:\n%s", result)
	}
}

func TestExactReplace_NotFound(t *testing.T) {
	content := "hello world"
	_, ok := tooling.ExactReplace(content, "foobar", "baz", 1)
	if ok {
		t.Fatal("expected no match")
	}
}

func TestWhitespaceFlexReplace_DifferentIndentation(t *testing.T) {
	content := "func main() {\n    if true {\n        fmt.Println(\"hello\")\n    }\n}\n"
	oldStr := "if true {\n    fmt.Println(\"hello\")\n}"
	newStr := "if true {\n    fmt.Println(\"world\")\n    fmt.Println(\"extra\")\n}"

	result, ok := tooling.WhitespaceFlexReplace(content, oldStr, newStr)
	if !ok {
		t.Fatal("expected whitespace-flex match")
	}
	if !strings.Contains(result, `fmt.Println("world")`) {
		t.Errorf("expected replacement, got:\n%s", result)
	}
	if !strings.Contains(result, `fmt.Println("extra")`) {
		t.Errorf("expected extra line, got:\n%s", result)
	}
}

func TestWhitespaceFlexReplace_NoMatch(t *testing.T) {
	content := "func main() {\n    doStuff()\n}\n"
	oldStr := "doOtherStuff()"

	_, ok := tooling.WhitespaceFlexReplace(content, oldStr, "replacement()")
	if ok {
		t.Fatal("expected no match")
	}
}

func TestFuzzyReplace_SlightDifference(t *testing.T) {
	content := "func handler() {\n\ta := 1\n\tb := 2\n\tc := 3\n\td := 4\n\te := 5\n\tf := 6\n}\n"
	oldStr := "func handler() {\n\ta := 1\n\tb := 2\n\tc := 99\n\td := 4\n\te := 5\n\tf := 6\n}"
	newStr := "func handler() {\n\ta := 10\n\tb := 20\n\tc := 30\n\td := 40\n\te := 50\n\tf := 60\n}"

	result, ok := tooling.FuzzyReplace(content, oldStr, newStr, 0.8)
	if !ok {
		t.Fatal("expected fuzzy match")
	}
	if !strings.Contains(result, "a := 10") {
		t.Errorf("expected replacement, got:\n%s", result)
	}
}

func TestFuzzyReplace_TooLowSimilarity(t *testing.T) {
	content := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"
	oldStr := "completely different content that bears no resemblance"

	_, ok := tooling.FuzzyReplace(content, oldStr, "replacement", 0.8)
	if ok {
		t.Fatal("expected no match for dissimilar content")
	}
}

func TestSearchReplace_TriesStrategiesInOrder(t *testing.T) {
	content := "    if x > 0 {\n        doStuff()\n    }\n"

	t.Run("exact match wins first", func(t *testing.T) {
		oldStr := "    if x > 0 {\n        doStuff()\n    }"
		result := tooling.SearchReplace(content, oldStr, "    if x > 0 {\n        doOther()\n    }", "test.go", 1)
		if result == nil {
			t.Fatal("expected match")
		}
		if result.Strategy != "exact" {
			t.Errorf("expected exact strategy, got %s", result.Strategy)
		}
	})

	t.Run("whitespace_flex when indent differs", func(t *testing.T) {
		oldStr := "if x > 0 {\n    doStuff()\n}"
		result := tooling.SearchReplace(content, oldStr, "if x > 0 {\n    doOther()\n}", "test.go", 1)
		if result == nil {
			t.Fatal("expected match")
		}
		if result.Strategy != "whitespace_flex" {
			t.Errorf("expected whitespace_flex strategy, got %s", result.Strategy)
		}
	})
}

func TestSearchReplace_ReturnsNilWhenNoMatch(t *testing.T) {
	content := "hello world"
	result := tooling.SearchReplace(content, "foobar", "baz", "test.txt", 1)
	if result != nil {
		t.Fatal("expected nil for no match")
	}
}

func TestSearchReplace_DiffIncluded(t *testing.T) {
	content := "line1\nline2\nline3\n"
	result := tooling.SearchReplace(content, "line2", "LINE_TWO", "test.txt", 1)
	if result == nil {
		t.Fatal("expected match")
	}
	if result.Diff == "" {
		t.Error("expected non-empty diff")
	}
	if !strings.Contains(result.Diff, "-line2") {
		t.Errorf("diff should show removed line, got:\n%s", result.Diff)
	}
	if !strings.Contains(result.Diff, "+LINE_TWO") {
		t.Errorf("diff should show added line, got:\n%s", result.Diff)
	}
}

func TestFindSimilar_ReturnsSuggestion(t *testing.T) {
	content := "func foo() {\n\ta := 1\n\tb := 2\n\tc := 3\n}\n"
	oldStr := "func foo() {\n\ta := 1\n\tb := 99\n\tc := 3\n}"

	suggest := tooling.FindSimilar(content, oldStr)
	if suggest == nil {
		t.Fatal("expected suggestion")
	}
	if suggest.Similarity < 0.6 {
		t.Errorf("expected similarity >= 0.6, got %f", suggest.Similarity)
	}
	if suggest.StartLine < 1 {
		t.Errorf("expected valid start line, got %d", suggest.StartLine)
	}
}

func TestFindSimilar_NothingSimilar(t *testing.T) {
	content := "package main\n"
	oldStr := "this is completely unrelated content that shares no common lines whatsoever with the file above"

	suggest := tooling.FindSimilar(content, oldStr)
	if suggest != nil {
		t.Errorf("expected nil for very dissimilar content, got similarity=%f", suggest.Similarity)
	}
}
