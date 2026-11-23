package orchestration

import (
	"context"
	"errors"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// skillTool is the tool claude calls to invoke a skill; the skill's name is
// the summary of the call in the transcript.
const skillTool = "Skill"

// SyncContextUsage reads what the session's transcript gained since the last
// pass and adds it to the session's context: how often each tool and skill was
// used, a skill_invoked change per skill call, and a tool_first_used change
// the first time a tool that was on demand is used (ADR 0099, contract C4). It
// reports whether anything changed. A session with no recorded context, or
// with no transcript yet, changes nothing.
//
// The first pass reads the whole transcript, every page back to its start;
// later passes read only what follows the cursor the last one left.
func (s *Service) SyncContextUsage(ctx context.Context, id string) (bool, error) {
	if s.Contexts == nil || s.TranscriptReader == nil {
		return false, nil
	}
	sess := s.sessions.Get(id)
	if sess == nil {
		return false, nil
	}
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	c, err := s.Contexts.Load(ctx, id)
	if err != nil || c == nil || len(c.Entries) == 0 {
		return false, err
	}
	entries, last, err := s.transcriptSince(sess, c.Cursor)
	if err != nil {
		var se *domain.StructuredError
		if errors.As(err, &se) && se.Code == "SESSION_TRANSCRIPT_MISSING" {
			return false, nil // nothing written yet
		}
		return false, err
	}
	if len(entries) == 0 {
		return false, nil
	}
	changed := addUse(c, entries)
	c.Cursor = last
	if err := s.Contexts.Save(ctx, c); err != nil {
		return false, err
	}
	return changed, nil
}

// transcriptSince is the transcript's entries after cursor, oldest first, and
// the cursor of the last. With no cursor it is the whole transcript.
func (s *Service) transcriptSince(sess *domain.Session, cursor string) ([]ports.TranscriptEntry, string, error) {
	read := func(q ports.TranscriptQuery) (ports.TranscriptPage, error) {
		q.Limit = ports.TranscriptMaxLimit
		return s.TranscriptReader.ReadTranscript(sess.WorkingDir, claudeID(sess), q)
	}
	if cursor != "" {
		var out []ports.TranscriptEntry
		for {
			page, err := read(ports.TranscriptQuery{After: cursor})
			if err != nil {
				return nil, "", err
			}
			out = append(out, page.Entries...)
			if len(page.Entries) > 0 {
				cursor = page.LastCursor
			}
			if !page.HasNewer || len(page.Entries) == 0 {
				return out, cursor, nil
			}
		}
	}
	page, err := read(ports.TranscriptQuery{})
	if err != nil {
		return nil, "", err
	}
	out, last := page.Entries, page.LastCursor
	for page.HasOlder && len(page.Entries) > 0 {
		if page, err = read(ports.TranscriptQuery{Before: page.FirstCursor}); err != nil {
			return nil, "", err
		}
		out = append(append([]ports.TranscriptEntry(nil), page.Entries...), out...)
	}
	return out, last, nil
}

// addUse counts the tool_use entries into c's usage and records the changes
// they make; it reports whether any entry was a tool call.
func addUse(c *domain.SessionContext, entries []ports.TranscriptEntry) bool {
	if c.Usage.Tools == nil {
		c.Usage.Tools = map[string]domain.ContextUse{}
	}
	if c.Usage.Skills == nil {
		c.Usage.Skills = map[string]domain.ContextUse{}
	}
	onDemand := c.OnDemandTools()
	changed := false
	for _, e := range entries {
		if e.Kind != ports.TranscriptToolUse || e.Tool == "" {
			continue
		}
		changed = true
		at, err := time.Parse(time.RFC3339, e.At)
		if err != nil {
			at = time.Now()
		}
		first := c.Usage.Tools[e.Tool].Count == 0
		c.Usage.Tools[e.Tool] = bump(c.Usage.Tools[e.Tool], at)
		if first && onDemand[e.Tool] {
			c.Changes = append(c.Changes, domain.ContextChange{At: at, Kind: domain.ContextChangeToolFirstUsed,
				Subject: e.Tool, Detail: "First use of a tool that was on demand"})
		}
		if e.Tool == skillTool && e.Text != "" {
			c.Usage.Skills[e.Text] = bump(c.Usage.Skills[e.Text], at)
			c.Changes = append(c.Changes, domain.ContextChange{At: at, Kind: domain.ContextChangeSkillInvoked,
				Subject: e.Text, Detail: "The agent invoked the skill " + e.Text})
		}
	}
	return changed
}

func bump(u domain.ContextUse, at time.Time) domain.ContextUse {
	u.Count++
	if at.After(u.LastAt) {
		u.LastAt = at
	}
	return u
}
