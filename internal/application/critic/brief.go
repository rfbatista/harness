package critic

import (
	"context"
	"strconv"
	"strings"

	"operators-mcp/internal/domain"
)

// Caps keep the starting context a size one model turn can take.
const (
	maxDiffBytes     = 200 << 10
	maxDocumentBytes = 60 << 10
	maxStandardBytes = 30 << 10
)

// Marker opens the critic's findings message to the author: the task channel
// recognises a critic's report by the session, and the author by this subject.
const FindingsSubject = "Critic findings"

// brief is the critic's whole starting context: the critic prompt, the diff
// against base, the task's Spec and Contract documents and the standards the
// effective configuration lists. Nothing from the author's transcript.
func (s *Service) brief(ctx context.Context, author *domain.Session, eff domain.EffectiveAgentSystem, base, head, diff string) string {
	var b strings.Builder
	b.WriteString("You are the critic: a fresh pair of eyes the harness launched after the author session's gates went green. " +
		"You review the diff below against the task's Spec and the repository's standards. You are advisory and read-only: " +
		"change no file, make no commit, push nothing; the harness refuses it.\n\n")
	b.WriteString("Report once, with message_architect kind status_report, status done, subject \"" + FindingsSubject + "\". " +
		"The harness delivers it to the author session and to the architect. The body is a verdict line, `Verdict: approve` or " +
		"`Verdict: changes_requested`, then one finding per line in the form `file:line · severity · reason` " +
		"(severity is blocker, major, minor or nit); no finding at all when the diff is sound. " +
		"Write the same findings as one document titled `Review: <task title>` in task/docs/.\n\n")
	b.WriteString("Diff of the author's branch against its base (" + short(base) + ".." + short(head) + "):\n\n```diff\n")
	b.WriteString(capped(diff, maxDiffBytes))
	b.WriteString("\n```\n")

	if s.d.Documents != nil {
		for _, d := range s.d.Documents.ListTicketDocuments(ctx, author.TicketID) {
			if !strings.HasPrefix(d.Title, "Spec:") && !strings.HasPrefix(d.Title, "Contract:") {
				continue
			}
			b.WriteString("\nTask document \"" + d.Title + "\":\n\n" + capped(d.Content, maxDocumentBytes) + "\n")
		}
	}

	for _, src := range eff.Review.Critic.Standards.Value {
		if text := s.d.Diff.File(ctx, author, src); text != "" {
			b.WriteString("\nStandard " + src + ":\n\n" + capped(text, maxStandardBytes) + "\n")
		} else if strings.EqualFold(src, "context map") {
			b.WriteString("\nThe repository's context map is in your starting context when it has one.\n")
		}
	}
	return b.String()
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func capped(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n… (" + strconv.Itoa(len(s)-max) + " more bytes cut)"
}
