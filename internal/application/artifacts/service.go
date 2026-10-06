package artifacts

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.Artifacts = (*Service)(nil)

// DefaultMaxSizeBytes caps a published file. Videos are the case to think
// about: a few minutes of screen capture fits; a raw export does not.
const DefaultMaxSizeBytes int64 = 512 << 20

// sniffLen is how much of a file is read to infer its type when the
// extension says nothing.
const sniffLen = 512

// Service implements ports.Artifacts.
type Service struct {
	artifacts ports.ArtifactRepository
	sessions  ports.SessionRepository
	announce  ports.SessionAnnouncer // nil: publishes are recorded but not announced

	// MaxSizeBytes caps a published file; zero means DefaultMaxSizeBytes.
	MaxSizeBytes int64
	now          func() time.Time
}

// NewService returns the artifacts service. announce may be nil (tests).
func NewService(repo ports.ArtifactRepository, sessions ports.SessionRepository, announce ports.SessionAnnouncer) *Service {
	return &Service{artifacts: repo, sessions: sessions, announce: announce, now: time.Now}
}

// Subscribe removes a session's records when the session is deleted. The
// files stay: they belong to the worktree, whose own removal is the
// workspaces context's business.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(_ context.Context, ev domain.SessionDeleted) error {
		return s.artifacts.DeleteBySession(ev.SessionID)
	})
}

func (s *Service) maxSize() int64 {
	if s.MaxSizeBytes > 0 {
		return s.MaxSizeBytes
	}
	return DefaultMaxSizeBytes
}

func invalid(msg string) error { return &domain.StructuredError{Code: "INVALID_INPUT", Message: msg} }

// Publish records what a session made and tells the session's followers,
// synchronously: when it returns, the event is on the stream.
func (s *Service) Publish(_ context.Context, req ports.PublishArtifactRequest) (*domain.Artifact, error) {
	sess := s.sessions.Get(req.SessionID)
	if sess == nil {
		return nil, &domain.StructuredError{Code: "SESSION_NOT_FOUND", Message: "session not found"}
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, invalid("title is required")
	}
	declared, err := domain.ParseArtifactKind(strings.TrimSpace(req.Kind))
	if err != nil {
		return nil, err
	}
	p, u := strings.TrimSpace(req.Path), strings.TrimSpace(req.URL)
	if (p == "") == (u == "") {
		return nil, invalid("give exactly one of path or url")
	}
	a := &domain.Artifact{SessionID: sess.ID, TicketID: sess.TicketID, ProjectID: sess.ProjectID, Title: title, Note: strings.TrimSpace(req.Note)}

	if u != "" {
		if !domain.IsLoopbackURL(u) {
			return nil, &domain.StructuredError{Code: "ARTIFACT_URL_NOT_LOCAL",
				Message: "url must be http://localhost:<port>/… or http://127.0.0.1:<port>/… (a dev server on this machine); got " + u}
		}
		if declared != "" && declared != domain.ArtifactURL {
			return nil, &domain.StructuredError{Code: "ARTIFACT_KIND_MISMATCH", Message: "kind " + string(declared) + " does not go with url; a url artifact is kind url (or omit kind)"}
		}
		a.Kind, a.URL = domain.ArtifactURL, u
	} else {
		if declared == domain.ArtifactURL {
			return nil, &domain.StructuredError{Code: "ARTIFACT_KIND_MISMATCH", Message: "kind url needs url, not path"}
		}
		if sess.WorkingDir == "" {
			return nil, outside("this session has no worktree to publish from")
		}
		abs, rel, err := resolveInWorktree(sess.WorkingDir, p)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			return nil, notFound(p + " does not exist in the worktree or is a directory")
		}
		if info.Size() > s.maxSize() {
			return nil, &domain.StructuredError{Code: "ARTIFACT_TOO_LARGE",
				Message: fmt.Sprintf("%s is %s; the server publishes files up to %s", rel, humanBytes(info.Size()), humanBytes(s.maxSize()))}
		}
		a.Mime = domain.ArtifactMIME(rel, readHead(abs))
		inferred := domain.InferArtifactKind(a.Mime)
		if !domain.ArtifactKindMatches(declared, inferred) {
			return nil, &domain.StructuredError{Code: "ARTIFACT_KIND_MISMATCH",
				Message: fmt.Sprintf("%s is a %s (%s), not %s; omit kind to infer it", rel, inferred, a.Mime, declared)}
		}
		a.Kind = inferred
		if declared == domain.ArtifactFile {
			a.Kind = domain.ArtifactFile
		}
		a.Path, a.SizeBytes = rel, info.Size()
	}

	saved, err := s.save(a)
	if err != nil {
		return nil, err
	}
	if s.announce != nil {
		s.announce.Announce(sess.ID, ports.SessionEvent{Type: "artifact", Text: saved.Note, Artifact: saved, At: s.now()})
	}
	return saved, nil
}

// save applies the identity rule: the same session re-publishing the same
// path or url bumps the revision of the artifact it already has.
func (s *Service) save(a *domain.Artifact) (*domain.Artifact, error) {
	if prev := s.artifacts.FindByTarget(a.SessionID, a.Path, a.URL); prev != nil {
		prev.Title, prev.Note, prev.Kind, prev.Mime, prev.SizeBytes = a.Title, a.Note, a.Kind, a.Mime, a.SizeBytes
		prev.Revision++
		prev.UpdatedAt = s.now()
		if err := s.artifacts.Update(prev); err != nil {
			return nil, err
		}
		return s.artifacts.Get(prev.ID), nil
	}
	a.Revision = 1
	return s.artifacts.Create(a)
}

func (s *Service) Unpublish(_ context.Context, sessionID, artifactID string) error {
	a := s.artifacts.Get(artifactID)
	if a == nil {
		return notFound("artifact not found")
	}
	if a.SessionID != sessionID {
		return &domain.StructuredError{Code: "ARTIFACT_NOT_YOURS", Message: "artifact " + artifactID + " was published by session " + a.SessionID + "; only that session can unpublish it"}
	}
	return s.artifacts.Delete(artifactID)
}

func (s *Service) ListTaskArtifacts(_ context.Context, ticketID string) ([]*domain.Artifact, error) {
	if ticketID == "" {
		return nil, invalid("ticket id is required")
	}
	return s.artifacts.List(ports.ArtifactFilter{TicketID: ticketID}), nil
}

func (s *Service) GetArtifact(_ context.Context, id string) (*domain.Artifact, error) {
	a := s.artifacts.Get(id)
	if a == nil {
		return nil, notFound("artifact not found")
	}
	return a, nil
}

func (s *Service) ListArtifacts(_ context.Context, f ports.ArtifactFilter) ([]*domain.Artifact, error) {
	if f.SessionID == "" && f.TicketID == "" {
		return nil, invalid("session_id or ticket_id is required")
	}
	return s.artifacts.List(f), nil
}

func (s *Service) DeleteArtifact(_ context.Context, id string) error { return s.artifacts.Delete(id) }

// OpenArtifactFile opens the artifact's file, or a file relpath away from
// it, confined to the session worktree. Every refusal is ARTIFACT_NOT_FOUND:
// the browser asked for a subresource, and a 404 is all it needs.
func (s *Service) OpenArtifactFile(_ context.Context, id, relpath string) (*ports.ArtifactFile, error) {
	a := s.artifacts.Get(id)
	if a == nil {
		return nil, notFound("artifact not found")
	}
	if a.Kind == domain.ArtifactURL || a.Path == "" {
		return nil, notFound("a url artifact has no file to view")
	}
	sess := s.sessions.Get(a.SessionID)
	if sess == nil || sess.WorkingDir == "" {
		return nil, notFound("the artifact's session has no worktree")
	}
	target := a.Path
	if relpath != "" {
		// POSIX join against the artifact's directory; ".." may climb within
		// the worktree, and resolveInWorktree refuses anything that leaves it.
		target = path.Join(path.Dir(a.Path), relpath)
	}
	abs, rel, err := resolveInWorktree(sess.WorkingDir, filepath.FromSlash(target))
	if err != nil {
		return nil, notFound(target + " is not available under this artifact")
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, notFound(target + " cannot be opened")
	}
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		f.Close()
		return nil, notFound(target + " is not a file")
	}
	mimeType := a.Mime
	if relpath != "" {
		head := make([]byte, sniffLen)
		n, _ := io.ReadFull(f, head)
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			return nil, notFound(target + " cannot be read")
		}
		mimeType = domain.ArtifactMIME(rel, head[:n])
	}
	return &ports.ArtifactFile{Content: f, Mime: mimeType, Size: info.Size(), ModTime: info.ModTime(), Revision: a.Revision}, nil
}

// readHead returns up to sniffLen bytes of the file, or nil.
func readHead(abs string) []byte {
	f, err := os.Open(abs)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, sniffLen)
	n, _ := io.ReadFull(f, buf)
	return buf[:n]
}

// humanBytes renders a size the way an agent can quote back: "11 B", "3.2 MiB".
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
