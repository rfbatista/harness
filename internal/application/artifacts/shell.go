package artifacts

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Designs written in a session's virtual shell. The shell is removed with
// the session, so each revision's bytes are kept in the harness's store
// from the start, the way a project asset's are.

var _ ports.ShellDesignWriter = (*Service)(nil)

// PublishDesign publishes content as a new task-scope artifact of the
// session's task, at path relative to its designs folder.
func (s *Service) PublishDesign(_ context.Context, sessionID, p, title string, content []byte) (*domain.Artifact, error) {
	sess := s.sessions.Get(sessionID)
	if sess == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	p = strings.TrimPrefix(path.Clean("/"+filepath.ToSlash(p)), "/")
	title = strings.TrimSpace(title)
	if p == "" || title == "" {
		return nil, invalid("a design needs a path and a title")
	}
	if err := s.designFits(p, content); err != nil {
		return nil, err
	}
	mime := domain.ArtifactMIME(p, content)
	a := &domain.Artifact{SessionID: sess.ID, TicketID: sess.TicketID, ProjectID: sess.ProjectID, Title: title,
		Kind: domain.InferArtifactKind(mime), Path: p, Mime: mime, SizeBytes: int64(len(content)),
		Revision: 1, Scope: domain.ArtifactScopeTask, Snapshot: true}

	unlockSession := s.lockSession(sess.ID)
	created, err := s.artifacts.Create(a)
	if err != nil {
		unlockSession()
		return nil, err
	}
	defer s.lockArtifact(created.ID)()
	unlockSession()
	if err := s.writeRevision(created, created.Revision, "", content); err != nil {
		_ = s.artifacts.Delete(created.ID)
		return nil, err
	}
	s.announceArtifact(created)
	s.changed(created)
	return created, nil
}

// ReviseDesign makes content the artifact's next revision. The files next
// to it come from the harness's copy when it has one, else from its
// session's worktree while there is one.
func (s *Service) ReviseDesign(_ context.Context, id string, content []byte) (*domain.Artifact, error) {
	defer s.lockArtifact(id)()
	a := s.artifacts.Get(id)
	if a == nil {
		return nil, notFound("artifact not found")
	}
	if a.Kind == domain.ArtifactURL || a.Path == "" {
		return nil, invalid("a url artifact has no file to revise")
	}
	if err := s.designFits(a.Path, content); err != nil {
		return nil, err
	}
	from := ""
	if a.Snapshot {
		from = s.snapshotRoot(a.ID, a.Revision)
	} else if sess := s.sessions.Get(a.SessionID); sess != nil && sess.WorkingDir != "" {
		from = sess.WorkingDir
	}
	next := a.Revision + 1
	if err := s.writeRevision(a, next, from, content); err != nil {
		return nil, err
	}
	a.Revision, a.Snapshot = next, true
	a.Mime, a.SizeBytes, a.Note = domain.ArtifactMIME(a.Path, content), int64(len(content)), ""
	a.UpdatedAt = s.now()
	if err := s.artifacts.Update(a); err != nil {
		os.RemoveAll(s.snapshotRoot(a.ID, next))
		return nil, err
	}
	s.pruneSnapshots(a.ID, next)
	saved := s.artifacts.Get(a.ID)
	if saved != nil && saved.Scope == domain.ArtifactScopeProject {
		s.announceFeed(ports.ArtifactUpdated(saved))
	}
	s.announceArtifact(saved)
	s.changed(saved)
	return saved, nil
}

// RemoveDesign unpublishes a task-scope artifact.
func (s *Service) RemoveDesign(_ context.Context, id string) error {
	defer s.lockArtifact(id)()
	a := s.artifacts.Get(id)
	if a == nil {
		return notFound("artifact not found")
	}
	if a.Scope == domain.ArtifactScopeProject {
		return &domain.StructuredError{Code: "ARTIFACT_IN_PROJECT", Message: "artifact " + id + " is kept by the project; move it back to task first, or a person deletes it in the UI"}
	}
	if err := s.delete(id); err != nil {
		return err
	}
	s.changed(a)
	return nil
}

func (s *Service) designFits(p string, content []byte) error {
	if s.StoreDir == "" {
		return &domain.StructuredError{Code: "UNAVAILABLE", Message: "this server keeps no artifact store, so designs cannot be published from the shell"}
	}
	if int64(len(content)) > s.maxSize() {
		return &domain.StructuredError{Code: "ARTIFACT_TOO_LARGE",
			Message: fmt.Sprintf("%s is %s; the server publishes files up to %s", p, humanBytes(int64(len(content))), humanBytes(s.maxSize()))}
	}
	return nil
}

// writeRevision makes the harness's copy of a at revision: the files of
// from (a copy root, or a worktree the artifact lives in; "" for none) and
// content as the artifact's own file. It appears whole or not at all. The
// caller holds the artifact's lock.
func (s *Service) writeRevision(a *domain.Artifact, revision int, from string, content []byte) error {
	dest := s.snapshotRoot(a.ID, revision)
	tmp := dest + ".tmp-" + randSuffix()
	err := func() error {
		switch {
		case from == "":
		case a.Snapshot:
			if err := copyTree(from, tmp); err != nil {
				return err
			}
		default:
			// Best effort: the worktree may no longer hold the files.
			_ = copyArtifactDir(from, a.Path, tmp, s.maxSize())
		}
		if err := os.MkdirAll(tmp, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(tmp, path.Base(a.Path)), content, 0o644)
	}()
	if err != nil {
		os.RemoveAll(tmp)
		return err
	}
	os.RemoveAll(dest)
	if err := os.Rename(tmp, dest); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	return nil
}

// copyTree copies the regular files under src into dst.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		return copyFile(p, filepath.Join(dst, rel))
	})
}

// announceArtifact tells the producing session's followers, while it
// exists. The caller holds the artifact's lock.
func (s *Service) announceArtifact(a *domain.Artifact) {
	if s.announce != nil && a != nil && s.sessions.Get(a.SessionID) != nil {
		s.announce.Announce(a.SessionID, ports.SessionEvent{Type: "artifact", Artifact: a, At: s.now()})
	}
}
