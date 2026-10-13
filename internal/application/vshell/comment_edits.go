package vshell

import (
	"context"
	"os"
	"time"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Comments written through the edit proxy: a new file in task/comments is
// a comment by this session; its own comments it edits and removes, and
// anyone else's is COMMENT_NOT_YOURS (never READ_ONLY, though their files
// are 0444).

func refuseNotYours(rel, author string) ports.ShellAnswer {
	return refuse("COMMENT_NOT_YOURS", rel+" was written by "+author)
}

// checkCommentAt: a new comment file may be written; an existing one only
// when this session wrote it and its copy is the stored one.
func (s *Service) checkCommentAt(sh *shell, rel string) ports.ShellAnswer {
	if row, ok := sh.files[rel]; ok && row.Kind == ports.ShellComment {
		if c := s.comment(sh, row.ID); c != nil && !c.WrittenBy(sh.sessionID) {
			return refuseNotYours(rel, row.Title)
		}
		if ans := s.stale(sh, row); !ans.Allow {
			return ans
		}
	}
	sh.hold(rel)
	return allow
}

// commitCommentWrite stores the file at rel as this session's comment: a
// new one, or its comment's new body. The file keeps the name the agent
// gave it.
func (s *Service) commitCommentWrite(sh *shell, rel string) ports.ShellAnswer {
	content, err := os.ReadFile(sh.abs(rel))
	if err != nil {
		return allow // nothing was written
	}
	ctx := context.Background()
	row, ok := sh.files[rel]
	var c *domain.TicketComment
	switch {
	case !ok:
		c, err = s.src.Comments.AddSessionComment(ctx, sh.ticketID, s.writer(ctx, sh.sessionID), string(content))
	case row.Kind != ports.ShellComment:
		return allow
	case Version(content) == row.Version:
		return allow
	default:
		c, err = s.src.Comments.UpdateSessionComment(ctx, sh.ticketID, sh.sessionID, row.ID, string(content))
	}
	if err != nil {
		return s.refuseComment(sh, rel, row, err)
	}
	sh.files[rel] = commentRow(rel, c)
	if c.Body != string(content) {
		if err := writeAtomic(sh.abs(rel), []byte(c.Body), false); err != nil {
			return refuseErr(err)
		}
	}
	return sh.saveIndex()
}

// commitCommentRemove deletes this session's comments whose files under
// rel are gone.
func (s *Service) commitCommentRemove(sh *shell, rel string) ports.ShellAnswer {
	ctx := context.Background()
	for _, row := range sh.under(rel, ports.ShellComment) {
		if _, err := os.Lstat(sh.abs(row.Path)); err == nil {
			continue // the removal did not happen
		}
		err := s.src.Comments.DeleteSessionComment(ctx, sh.ticketID, sh.sessionID, row.ID)
		if err != nil && !errs.HasCode(err, "COMMENT_NOT_FOUND") {
			return s.refuseComment(sh, row.Path, row, err)
		}
		delete(sh.files, row.Path)
	}
	return sh.saveIndex()
}

// refuseComment answers a comment write planning refused, and puts back
// what is stored.
func (s *Service) refuseComment(sh *shell, rel string, row ports.ShellFile, err error) ports.ShellAnswer {
	var ans ports.ShellAnswer
	switch {
	case errs.HasCode(err, "COMMENT_NOT_YOURS"):
		ans = refuseNotYours(rel, row.Title)
	case errs.HasCode(err, "COMMENT_NOT_FOUND"):
		ans = refuseStale(rel, "deleted")
	default:
		ans = refuseErr(err)
	}
	s.restore(sh, rel)
	return ans
}

// comment is the task's comment id as stored, or nil.
func (s *Service) comment(sh *shell, id string) *domain.TicketComment {
	list, err := s.src.Comments.ListTicketComments(context.Background(), sh.ticketID, time.Time{})
	if err != nil {
		return nil
	}
	for _, c := range list {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// writer is the session a comment is written as, with its agent's name.
func (s *Service) writer(ctx context.Context, sessionID string) domain.CommentWriter {
	w := domain.CommentWriter{SessionID: sessionID}
	if s.src.Sessions == nil {
		return w
	}
	sess := s.src.Sessions.Get(sessionID)
	if sess == nil || sess.AgentID == "" {
		return w
	}
	w.AgentName = sess.AgentID
	if s.src.Agents != nil {
		if a, err := s.src.Agents.GetAgent(ctx, sess.AgentID); err == nil && a != nil && a.Name != "" {
			w.AgentName = a.Name
		}
	}
	return w
}

func commentRow(rel string, c *domain.TicketComment) ports.ShellFile {
	return ports.ShellFile{Path: rel, Kind: ports.ShellComment, ID: c.ID, Scope: "task",
		Version: Version([]byte(c.Body)), Title: CommentAuthor(c), UpdatedAt: c.UpdatedAt}
}
