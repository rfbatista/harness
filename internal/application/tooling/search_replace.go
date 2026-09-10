package tooling

import (
	"fmt"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// ReplaceResult holds the outcome of a successful search-and-replace operation.
type ReplaceResult struct {
	Content  string
	Strategy string
	Diff     string
}

// SuggestResult holds the closest matching chunk when no strategy succeeds.
type SuggestResult struct {
	StartLine  int
	EndLine    int
	Similarity float64
	Text       string
}

// SearchReplace tries multiple matching strategies in order of strictness
// and returns the first successful result. Inspired by Aider's search_replace.py.
func SearchReplace(content, oldStr, newStr, path string, count int) *ReplaceResult {
	if count <= 0 {
		count = 1
	}

	if result, ok := ExactReplace(content, oldStr, newStr, count); ok {
		diff := generateDiff(content, result, path)
		return &ReplaceResult{Content: result, Strategy: "exact", Diff: diff}
	}

	if result, ok := WhitespaceFlexReplace(content, oldStr, newStr); ok {
		diff := generateDiff(content, result, path)
		return &ReplaceResult{Content: result, Strategy: "whitespace_flex", Diff: diff}
	}

	if result, ok := FuzzyReplace(content, oldStr, newStr, 0.8); ok {
		diff := generateDiff(content, result, path)
		return &ReplaceResult{Content: result, Strategy: "fuzzy", Diff: diff}
	}

	return nil
}

// ExactReplace performs a literal string replacement, replacing up to count occurrences.
func ExactReplace(content, oldStr, newStr string, count int) (string, bool) {
	if !strings.Contains(content, oldStr) {
		return "", false
	}
	result := strings.Replace(content, oldStr, newStr, count)
	return result, true
}

// WhitespaceFlexReplace matches oldStr against content with flexible leading
// whitespace, then re-indents the replacement to match the content's indentation.
func WhitespaceFlexReplace(content, oldStr, newStr string) (string, bool) {
	contentLines := strings.Split(content, "\n")
	oldLines := trimTrailingEmpty(strings.Split(oldStr, "\n"))
	newLines := strings.Split(newStr, "\n")

	if len(oldLines) == 0 {
		return "", false
	}

	oldStripped, oldIndent := stripCommonIndent(oldLines)

	for i := 0; i <= len(contentLines)-len(oldLines); i++ {
		window := contentLines[i : i+len(oldLines)]
		windowStripped, windowIndent := stripCommonIndent(window)

		if linesEqual(oldStripped, windowStripped) {
			reindented := reindentLines(newLines, oldIndent, windowIndent)

			result := make([]string, 0, len(contentLines)-len(oldLines)+len(reindented))
			result = append(result, contentLines[:i]...)
			result = append(result, reindented...)
			result = append(result, contentLines[i+len(oldLines):]...)

			return strings.Join(result, "\n"), true
		}
	}

	return "", false
}

// FuzzyReplace uses a line-level SequenceMatcher to find the most similar chunk
// above the given threshold and replaces it.
func FuzzyReplace(content, oldStr, newStr string, threshold float64) (string, bool) {
	contentLines := strings.Split(content, "\n")
	oldLines := trimTrailingEmpty(strings.Split(oldStr, "\n"))
	newLines := strings.Split(newStr, "\n")

	if len(oldLines) == 0 || len(contentLines) == 0 {
		return "", false
	}

	oldNorm := normalizeLines(oldLines)

	bestRatio := 0.0
	bestIdx := -1
	bestLen := 0

	minWS := max(1, len(oldLines)-2)
	maxWS := min(len(contentLines), len(oldLines)+2)

	for ws := minWS; ws <= maxWS; ws++ {
		for i := 0; i <= len(contentLines)-ws; i++ {
			window := contentLines[i : i+ws]
			windowNorm := normalizeLines(window)

			m := difflib.NewMatcher(oldNorm, windowNorm)
			ratio := m.Ratio()
			if ratio > bestRatio {
				bestRatio = ratio
				bestIdx = i
				bestLen = ws
			}
		}
	}

	if bestRatio < threshold || bestIdx < 0 {
		return "", false
	}

	matchedWindow := contentLines[bestIdx : bestIdx+bestLen]
	contentIndent := minIndent(matchedWindow)
	oldIndent := minIndent(oldLines)
	reindented := reindentLines(newLines, oldIndent, contentIndent)

	result := make([]string, 0, len(contentLines)-bestLen+len(reindented))
	result = append(result, contentLines[:bestIdx]...)
	result = append(result, reindented...)
	result = append(result, contentLines[bestIdx+bestLen:]...)

	return strings.Join(result, "\n"), true
}

// FindSimilar scans content for the chunk most similar to oldStr.
// Returns nil if nothing exceeds a minimum similarity of 0.6.
func FindSimilar(content, oldStr string) *SuggestResult {
	contentLines := strings.Split(content, "\n")
	oldLines := trimTrailingEmpty(strings.Split(oldStr, "\n"))

	if len(oldLines) == 0 || len(contentLines) == 0 {
		return nil
	}

	oldNorm := normalizeLines(oldLines)

	bestRatio := 0.0
	bestIdx := -1
	bestLen := 0

	minWS := max(1, len(oldLines)-2)
	maxWS := min(len(contentLines), len(oldLines)+2)

	for ws := minWS; ws <= maxWS; ws++ {
		for i := 0; i <= len(contentLines)-ws; i++ {
			window := contentLines[i : i+ws]
			windowNorm := normalizeLines(window)

			m := difflib.NewMatcher(oldNorm, windowNorm)
			ratio := m.Ratio()
			if ratio > bestRatio {
				bestRatio = ratio
				bestIdx = i
				bestLen = ws
			}
		}
	}

	const minSimilarity = 0.6
	if bestRatio < minSimilarity || bestIdx < 0 {
		return nil
	}

	matchedLines := contentLines[bestIdx : bestIdx+bestLen]
	return &SuggestResult{
		StartLine:  bestIdx + 1,
		EndLine:    bestIdx + bestLen,
		Similarity: bestRatio,
		Text:       strings.Join(matchedLines, "\n"),
	}
}

func generateDiff(old, new, path string) string {
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(old),
		B:        difflib.SplitLines(new),
		FromFile: "a/" + path,
		ToFile:   "b/" + path,
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 60 {
		lines = append(lines[:60], fmt.Sprintf("... (%d more lines)", len(lines)-60))
	}
	return strings.Join(lines, "\n")
}

func trimTrailingEmpty(lines []string) []string {
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func normalizeLines(lines []string) []string {
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = strings.TrimSpace(line)
	}
	return result
}

func minIndent(lines []string) int {
	mi := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if mi < 0 || indent < mi {
			mi = indent
		}
	}
	if mi < 0 {
		return 0
	}
	return mi
}

// stripCommonIndent removes the minimum leading whitespace from all lines
// and returns the normalized lines plus the number of characters stripped.
func stripCommonIndent(lines []string) ([]string, int) {
	mi := minIndent(lines)
	result := make([]string, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			result[i] = ""
		} else if len(line) > mi {
			result[i] = line[mi:]
		} else {
			result[i] = ""
		}
	}
	return result, mi
}

func linesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// reindentLines adjusts every line by the delta between toIndent and fromIndent.
func reindentLines(lines []string, fromIndent, toIndent int) []string {
	delta := toIndent - fromIndent
	if delta == 0 {
		return lines
	}
	result := make([]string, len(lines))
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			result[i] = line
			continue
		}
		if delta > 0 {
			result[i] = strings.Repeat(" ", delta) + line
		} else {
			trimmed := line
			for j := 0; j < -delta && len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\t'); j++ {
				trimmed = trimmed[1:]
			}
			result[i] = trimmed
		}
	}
	return result
}
