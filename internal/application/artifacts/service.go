package artifacts

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
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

	// Tickets finds the task an artifact is attached to. Nil: nothing can be
	// attached.
	Tickets ports.TicketReader
	// Feed announces project-asset changes on the project feed. Nil: they
	// are stored but not announced.
	Feed ports.ProjectChangeSink

	// MaxSizeBytes caps a published file, and a project artifact's copy as a
	// whole; zero means DefaultMaxSizeBytes.
	MaxSizeBytes int64
	// StoreDir is where the harness keeps its copies of artifacts:
	// <StoreDir>/<id>/r<revision>/ holds the artifact's directory as of that
	// revision. Empty: no artifact can move to project scope.
	StoreDir string
	now      func() time.Time

	// snapMu serializes everything that writes an artifact's row together
	// with its copy, so a row never names a revision whose copy is not there.
	snapMu sync.Mutex
}

// NewService returns the artifacts service. announce may be nil (tests).
func NewService(repo ports.ArtifactRepository, sessions ports.SessionRepository, announce ports.SessionAnnouncer) *Service {
	return &Service{artifacts: repo, sessions: sessions, announce: announce, now: time.Now}
}

// Subscribe removes a session's task-scoped records, and any copies they
// have, when the session is deleted. Project artifacts stay. The worktree
// files stay too: their removal is the workspaces context's business.
func (s *Service) Subscribe(sub ports.EventSubscriber) {
	ports.On(sub, func(_ context.Context, ev domain.SessionDeleted) error {
		s.snapMu.Lock()
		defer s.snapMu.Unlock()
		ids, err := s.artifacts.DeleteTaskScopedBySession(ev.SessionID)
		for _, id := range ids {
			s.dropSnapshot(id)
		}
		return err
	})
	// A deleted task loses its attachments; the assets stay in the project.
	ports.On(sub, func(_ context.Context, ev domain.TicketDeleted) error {
		s.snapMu.Lock()
		defer s.snapMu.Unlock()
		ids, err := s.artifacts.DetachTicket(ev.TicketID)
		for _, id := range ids {
			if a := s.artifacts.Get(id); a != nil {
				s.announceFeed(ports.ArtifactUpdated(a))
			}
		}
		return err
	})
}

// announceFeed puts a project-asset change on the project feed. Callers hold
// snapMu and have stored the change, so the feed carries an artifact's
// changes in the order they were applied.
func (s *Service) announceFeed(c ports.ProjectChange) {
	if s.Feed != nil {
		s.Feed.AnnounceChange(c)
	}
}

func (s *Service) maxSize() int64 {
	if s.MaxSizeBytes > 0 {
		return s.MaxSizeBytes
	}
	return DefaultMaxSizeBytes
}

func invalid(msg string) error { return &domain.StructuredError{Code: "INVALID_INPUT", Message: msg} }

func notPromotable(msg string) error {
	return &domain.StructuredError{Code: "ARTIFACT_NOT_PROMOTABLE", Message: msg}
}

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

	saved, err := s.save(a, sess.WorkingDir)
	if err != nil {
		return nil, err
	}
	if s.announce != nil {
		s.announce.Announce(sess.ID, ports.SessionEvent{Type: "artifact", Text: saved.Note, Artifact: saved, At: s.now()})
	}
	return saved, nil
}

// save applies the identity rule: the same session re-publishing the same
// path or url bumps the revision of the artifact it already has. When the
// harness holds a copy of it, the copy is refreshed first, under the new
// revision, so a refused copy leaves the artifact as it was.
func (s *Service) save(a *domain.Artifact, worktree string) (*domain.Artifact, error) {
	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	if prev := s.artifacts.FindByTarget(a.SessionID, a.Path, a.URL); prev != nil {
		prev.Title, prev.Note, prev.Kind, prev.Mime, prev.SizeBytes = a.Title, a.Note, a.Kind, a.Mime, a.SizeBytes
		prev.Revision++
		prev.UpdatedAt = s.now()
		if prev.Snapshot {
			if err := copyArtifactDir(worktree, prev.Path, s.snapshotRoot(prev.ID, prev.Revision), s.maxSize()); err != nil {
				return nil, err
			}
		}
		if err := s.artifacts.Update(prev); err != nil {
			if prev.Snapshot {
				os.RemoveAll(s.snapshotRoot(prev.ID, prev.Revision))
			}
			return nil, err
		}
		if prev.Snapshot {
			s.pruneSnapshots(prev.ID, prev.Revision)
		}
		saved := s.artifacts.Get(prev.ID)
		if saved != nil && saved.Scope == domain.ArtifactScopeProject {
			s.announceFeed(ports.ArtifactUpdated(saved))
		}
		return saved, nil
	}
	a.Revision = 1
	return s.artifacts.Create(a)
}

func (s *Service) Unpublish(_ context.Context, sessionID, artifactID string) error {
	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	a := s.artifacts.Get(artifactID)
	if a == nil {
		return notFound("artifact not found")
	}
	if a.SessionID != sessionID {
		return &domain.StructuredError{Code: "ARTIFACT_NOT_YOURS", Message: "artifact " + artifactID + " was published by session " + a.SessionID + "; only that session can unpublish it"}
	}
	if a.Scope == domain.ArtifactScopeProject {
		return &domain.StructuredError{Code: "ARTIFACT_IN_PROJECT", Message: "artifact " + artifactID + " is kept by the project; move it back to task first, or a person deletes it in the UI"}
	}
	return s.delete(artifactID)
}

// delete removes the record, then the harness's copy if it has one. The
// caller holds snapMu.
func (s *Service) delete(id string) error {
	if err := s.artifacts.Delete(id); err != nil {
		return err
	}
	s.dropSnapshot(id)
	return nil
}

// SetArtifactScope moves an artifact between task and project scope. Moving
// to project copies the artifact's directory into the harness's store unless
// a copy is already there; moving back keeps the copy. The same scope again
// returns the artifact as stored and announces nothing.
func (s *Service) SetArtifactScope(_ context.Context, artifactID string, scope domain.ArtifactScope) (*domain.Artifact, error) {
	if !scope.Valid() {
		return nil, invalid("scope must be task or project")
	}
	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	a := s.artifacts.Get(artifactID)
	if a == nil {
		return nil, notFound("artifact not found")
	}
	if a.Scope == scope {
		return a, nil
	}
	copied := false
	if scope == domain.ArtifactScopeProject && !a.Snapshot {
		if err := s.takeSnapshot(a); err != nil {
			return nil, err
		}
		copied = true
	}
	saved, err := s.artifacts.SetScope(a.ID, scope, a.Snapshot || copied)
	if err != nil {
		if copied {
			s.dropSnapshot(a.ID)
		}
		return nil, err
	}
	if s.announce != nil && s.sessions.Get(saved.SessionID) != nil {
		s.announce.Announce(saved.SessionID, ports.SessionEvent{Type: "artifact", Artifact: saved, At: s.now()})
	}
	// Either way the project library and attached tasks' pages change.
	s.announceFeed(ports.ArtifactUpdated(saved))
	return saved, nil
}

// takeSnapshot makes the harness's first copy of an artifact, from its
// session's worktree. Every reason it cannot is ARTIFACT_NOT_PROMOTABLE.
func (s *Service) takeSnapshot(a *domain.Artifact) error {
	if a.Kind == domain.ArtifactURL || a.Path == "" {
		return notPromotable("a url artifact cannot move to the project: a dev server dies with its session")
	}
	if s.StoreDir == "" {
		return notPromotable("this server keeps no artifact store")
	}
	sess := s.sessions.Get(a.SessionID)
	if sess == nil || sess.WorkingDir == "" {
		return notPromotable("the artifact's session and worktree are gone, so there are no bytes to keep")
	}
	if err := copyArtifactDir(sess.WorkingDir, a.Path, s.snapshotRoot(a.ID, a.Revision), s.maxSize()); err != nil {
		return notPromotable("cannot keep a copy of " + a.Path + ": " + err.Error())
	}
	return nil
}

func (s *Service) ListProjectArtifacts(_ context.Context, projectID string) ([]*domain.Artifact, error) {
	if projectID == "" {
		return nil, invalid("project id is required")
	}
	return s.artifacts.List(ports.ArtifactFilter{ProjectID: projectID, Scope: domain.ArtifactScopeProject}), nil
}

func (s *Service) snapshotRoot(id string, revision int) string {
	return filepath.Join(s.StoreDir, id, fmt.Sprintf("r%d", revision))
}

// pruneSnapshots removes every copy of an artifact but the one at keep.
func (s *Service) pruneSnapshots(id string, keep int) {
	entries, _ := os.ReadDir(filepath.Join(s.StoreDir, id))
	want := filepath.Base(s.snapshotRoot(id, keep))
	for _, e := range entries {
		if e.Name() != want {
			os.RemoveAll(filepath.Join(s.StoreDir, id, e.Name()))
		}
	}
}

func (s *Service) dropSnapshot(id string) {
	if s.StoreDir == "" || id == "" || strings.ContainsAny(id, `/\.`) {
		return
	}
	os.RemoveAll(filepath.Join(s.StoreDir, id))
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
	if f.SessionID == "" && f.TicketID == "" && f.ProjectID == "" {
		return nil, invalid("session_id, ticket_id or project_id is required")
	}
	if f.Scope != "" && !f.Scope.Valid() {
		return nil, invalid("scope must be task or project")
	}
	return s.artifacts.List(f), nil
}

// DeleteArtifact is a person removing an artifact, in either scope. A
// project asset's deletion goes on the project feed with the ids it had.
func (s *Service) DeleteArtifact(_ context.Context, id string) error {
	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	last := s.artifacts.Get(id)
	if err := s.delete(id); err != nil {
		return err
	}
	if last != nil && last.Scope == domain.ArtifactScopeProject {
		s.announceFeed(ports.ArtifactRemoved(last))
	}
	return nil
}

func (s *Service) AttachArtifactToTicket(ctx context.Context, artifactID, ticketID string) (*domain.Artifact, error) {
	return s.link(ctx, artifactID, ticketID, (*domain.Artifact).CheckAttach, s.artifacts.Attach)
}

func (s *Service) DetachArtifactFromTicket(ctx context.Context, artifactID, ticketID string) (*domain.Artifact, error) {
	return s.link(ctx, artifactID, ticketID, (*domain.Artifact).CheckDetach, s.artifacts.Detach)
}

// link applies an attach or a detach: check is the domain rule, apply the
// repository write. A no-op returns the artifact as stored and says nothing.
func (s *Service) link(ctx context.Context, artifactID, ticketID string,
	check func(*domain.Artifact, string, string) (bool, error), apply func(string, string) (bool, error)) (*domain.Artifact, error) {
	artifactID, ticketID = strings.TrimSpace(artifactID), strings.TrimSpace(ticketID)
	if artifactID == "" || ticketID == "" {
		return nil, invalid("artifact_id and ticket_id are required")
	}
	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	a := s.artifacts.Get(artifactID)
	if a == nil {
		return nil, notFound("artifact not found")
	}
	if s.Tickets == nil {
		return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "this server cannot look tasks up"}
	}
	tk, err := s.Tickets.GetTicket(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	noop, err := check(a, tk.ID, tk.ProjectID)
	if err != nil {
		return nil, err
	}
	if noop {
		return a, nil
	}
	if _, err := apply(a.ID, tk.ID); err != nil {
		return nil, err
	}
	saved := s.artifacts.Get(a.ID)
	if saved == nil {
		return nil, notFound("artifact not found")
	}
	s.announceFeed(ports.ArtifactUpdated(saved))
	return saved, nil
}

// OpenArtifactFile opens the artifact's file, or a file relpath away from
// it: from the harness's copy when it holds one, confined to that copy;
// otherwise from the session worktree, confined to it. Every refusal is
// ARTIFACT_NOT_FOUND: the browser asked for a subresource, and a 404 is all
// it needs.
func (s *Service) OpenArtifactFile(_ context.Context, id, relpath string) (*ports.ArtifactFile, error) {
	a := s.artifacts.Get(id)
	if a == nil {
		return nil, notFound("artifact not found")
	}
	if a.Kind == domain.ArtifactURL || a.Path == "" {
		return nil, notFound("a url artifact has no file to view")
	}
	var root, target string
	if a.Snapshot {
		// The copy's root is the artifact's directory: ".." cannot leave it.
		root, target = s.snapshotRoot(a.ID, a.Revision), path.Base(a.Path)
		if relpath != "" {
			target = path.Clean(relpath)
		}
	} else {
		sess := s.sessions.Get(a.SessionID)
		if sess == nil || sess.WorkingDir == "" {
			return nil, notFound("the artifact's session has no worktree")
		}
		root, target = sess.WorkingDir, a.Path
		if relpath != "" {
			// POSIX join against the artifact's directory; ".." may climb within
			// the worktree, and resolveInWorktree refuses anything that leaves it.
			target = path.Join(path.Dir(a.Path), relpath)
		}
	}
	abs, rel, err := resolveInWorktree(root, filepath.FromSlash(target))
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
