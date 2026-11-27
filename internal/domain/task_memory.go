package domain

import (
	"regexp"
	"strings"
)

// The task memory (ADR 0013): two files per task and repository, plan.md and
// progress.md, stored by the harness and shared by every session of the task
// in that repository.
const (
	TaskMemoryPlanFile     = "plan.md"
	TaskMemoryProgressFile = "progress.md"
)

// ProgressSectionNames are the fixed sections of progress.md, in order.
var ProgressSectionNames = []string{"Done", "Failed and why", "Next step", "Open questions"}

// TaskMemory is the content of the two files of one task and repository.
type TaskMemory struct {
	Plan     string
	Progress string
}

// TaskMemoryFiles are the file names of a task memory.
func TaskMemoryFiles() []string { return []string{TaskMemoryPlanFile, TaskMemoryProgressFile} }

// NewTaskMemory is the memory a task starts with: the fixed sections, empty.
func NewTaskMemory() TaskMemory {
	var b strings.Builder
	b.WriteString("# Progress\n")
	for _, name := range ProgressSectionNames {
		b.WriteString("\n## " + name + "\n")
	}
	return TaskMemory{
		Plan:     "# Plan\n\n## Steps\n\nOne step per line: `- [todo|doing|done|failed] what`.\n",
		Progress: b.String(),
	}
}

// Content is the file called name, "" for a name that is not a memory file.
func (m TaskMemory) Content(name string) string {
	switch name {
	case TaskMemoryPlanFile:
		return m.Plan
	case TaskMemoryProgressFile:
		return m.Progress
	}
	return ""
}

var sectionHeading = regexp.MustCompile(`^##\s+(.+?)\s*$`)

// ProgressSections splits progress.md by its "## " headings: section name to
// its body with surrounding whitespace trimmed. Text before the first heading
// belongs to no section.
func ProgressSections(md string) map[string]string {
	out := map[string]string{}
	var name string
	var body []string
	flush := func() {
		if name != "" {
			out[name] = strings.TrimSpace(strings.Join(body, "\n"))
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		if m := sectionHeading.FindStringSubmatch(line); m != nil {
			flush()
			name, body = m[1], nil
			continue
		}
		body = append(body, line)
	}
	flush()
	return out
}

// ProgressUnchanged reports whether every section of progress.md is identical
// in before and now: the "progress.md must change" check compares section by
// section, ignoring surrounding whitespace and line endings.
func ProgressUnchanged(before, now string) bool {
	a, b := ProgressSections(before), ProgressSections(now)
	if len(a) != len(b) {
		return false
	}
	for name, body := range a {
		if other, ok := b[name]; !ok || other != body {
			return false
		}
	}
	return true
}
