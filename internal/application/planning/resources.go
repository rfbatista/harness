package planning

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.TaskResources       = (*Service)(nil)
	_ ports.TaskResourceFiles   = (*Service)(nil)
	_ ports.TaskComments        = (*Service)(nil)
	_ ports.SessionTaskComments = (*Service)(nil)
)

// The resources and comments a person adds to a task. Every change is
// written, then announced on the project feed under resourceMu, so the feed
// carries them in the order they were applied; an addition also raises an
// event, which the orchestration turns into a turn for the task's sessions.

var (
	errResourceNotFound = &domain.StructuredError{Code: "RESOURCE_NOT_FOUND", Message: "resource not found"}
	errCommentNotFound  = &domain.StructuredError{Code: "COMMENT_NOT_FOUND", Message: "comment not found"}
	errCommentNotOnTask = &domain.StructuredError{Code: "COMMENT_NOT_ON_TASK", Message: "comment belongs to another task"}
	errCommentNotYours  = &domain.StructuredError{Code: "COMMENT_NOT_YOURS", Message: "only the session that wrote this comment can change it"}
	errNoResources      = &domain.StructuredError{Code: "UNAVAILABLE", Message: "task resources are not available on this server"}
	errNoComments       = &domain.StructuredError{Code: "UNAVAILABLE", Message: "task comments are not available on this server"}
)

func (s *Service) maxResourceBytes() int64 {
	if s.MaxResourceBytes > 0 {
		return s.MaxResourceBytes
	}
	return domain.MaxResourceBytes
}

func (s *Service) resourcesReady() error {
	if s.Resources == nil || s.Blobs == nil {
		return errNoResources
	}
	return nil
}

// ticket is the task id names, or TICKET_NOT_FOUND.
func (s *Service) ticket(id string) (*domain.Ticket, error) {
	if id == "" {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "ticket_id is required"}
	}
	tk := s.tickets.Get(id)
	if tk == nil {
		return nil, &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
	}
	return tk, nil
}

// stillThere is TICKET_NOT_FOUND when the task went while an addition was
// on its way. Callers hold resourceMu, which a task's deletion takes to drop
// what it had, so an addition lands either before that, and goes with it, or
// not at all.
func (s *Service) stillThere(ticketID string) error {
	if s.tickets.Get(ticketID) == nil {
		return &domain.StructuredError{Code: "TICKET_NOT_FOUND", Message: "ticket not found"}
	}
	return nil
}

// withContentURL fills the derived content URL of uploads.
func (s *Service) withContentURL(rs ...*domain.TicketResource) {
	if s.ContentURL == nil {
		return
	}
	for _, r := range rs {
		if r != nil && r.IsUpload() {
			r.ContentURL = s.ContentURL(r.ID)
		}
	}
}

func (s *Service) announceChange(c ports.ProjectChange) {
	if s.Feed != nil {
		s.Feed.AnnounceChange(c)
	}
}

func (s *Service) raise(ctx context.Context, ev domain.Event) {
	if s.Events == nil {
		return
	}
	if err := s.Events.Publish(ctx, ev); err != nil {
		slog.Warn("announce a planning change", "event", ev.EventName(), "err", err)
	}
}

// resourceAdded announces a new resource and raises its event. Callers hold
// resourceMu.
func (s *Service) resourceAdded(ctx context.Context, r *domain.TicketResource) {
	s.withContentURL(r)
	s.announceChange(ports.TicketResourceUpdated(r))
	s.raise(ctx, domain.TicketResourceAdded{Resource: *r})
}

func (s *Service) ListTicketResources(_ context.Context, ticketID string) ([]*domain.TicketResource, error) {
	if err := s.resourcesReady(); err != nil {
		return nil, err
	}
	if _, err := s.ticket(ticketID); err != nil {
		return nil, err
	}
	list := s.Resources.ListByTicket(ticketID)
	s.withContentURL(list...)
	return list, nil
}

func (s *Service) GetTicketResource(_ context.Context, id string) (*domain.TicketResource, error) {
	if err := s.resourcesReady(); err != nil {
		return nil, err
	}
	r := s.Resources.Get(id)
	if r == nil {
		return nil, errResourceNotFound
	}
	s.withContentURL(r)
	return r, nil
}

// UploadTicketResource checks the task before taking a byte, stages the
// content under the size cap, then stores the record and files the bytes
// under its id. A failure on the way leaves neither behind.
func (s *Service) UploadTicketResource(ctx context.Context, up ports.ResourceUpload) (*domain.TicketResource, error) {
	if err := s.resourcesReady(); err != nil {
		return nil, err
	}
	tk, err := s.ticket(up.TicketID)
	if err != nil {
		return nil, err
	}
	title := domain.DefaultResourceTitle(up.FileName)
	if strings.TrimSpace(up.Title) != "" {
		if title, err = domain.ResourceTitle(up.Title); err != nil {
			return nil, err
		}
	} else if title == "" {
		title = "file"
	}
	if up.Content == nil {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "file is required"}
	}
	staged, err := s.Blobs.Stage(up.Content, s.maxResourceBytes())
	if errors.Is(err, ports.ErrBlobTooLarge) {
		return nil, &domain.StructuredError{Code: "RESOURCE_TOO_LARGE", Message: "a file is limited to 25 MiB"}
	}
	if err != nil {
		return nil, err
	}
	if staged.Size() == 0 {
		staged.Discard()
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "file is empty"}
	}
	mimeType := resourceMime(up.Mime, up.FileName, staged.Head())

	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	if err := s.stillThere(tk.ID); err != nil {
		staged.Discard()
		return nil, err
	}
	r, err := s.Resources.Create(&domain.TicketResource{
		TicketID: tk.ID, ProjectID: tk.ProjectID, Kind: domain.ResourceKindFor(mimeType),
		Title: title, Mime: mimeType, SizeBytes: staged.Size(),
	})
	if err != nil {
		staged.Discard()
		return nil, err
	}
	if err := staged.Commit(r.ID); err != nil {
		if derr := s.Resources.Delete(r.ID); derr != nil {
			slog.Warn("drop a resource whose bytes were not kept", "resource", r.ID, "err", derr)
		}
		return nil, err
	}
	s.resourceAdded(ctx, r)
	return r, nil
}

// resourceMime is the type an upload is stored with: what the client
// declared, else what the file name's extension says, else what the first
// bytes look like. The media type is lower-cased; parameters are kept.
func resourceMime(declared, fileName string, head []byte) string {
	if t, params, err := mime.ParseMediaType(declared); err == nil && t != "application/octet-stream" {
		return mime.FormatMediaType(t, params)
	}
	if ext := filepath.Ext(fileName); ext != "" {
		if t := mime.TypeByExtension(strings.ToLower(ext)); t != "" {
			return t
		}
	}
	return http.DetectContentType(head)
}

// AddTicketLink stores a link. The server never fetches it.
func (s *Service) AddTicketLink(ctx context.Context, ticketID, rawURL, title string) (*domain.TicketResource, error) {
	if err := s.resourcesReady(); err != nil {
		return nil, err
	}
	tk, err := s.ticket(ticketID)
	if err != nil {
		return nil, err
	}
	u, err := domain.LinkURL(rawURL)
	if err != nil {
		return nil, err
	}
	t := domain.DefaultLinkTitle(u)
	if strings.TrimSpace(title) != "" {
		if t, err = domain.ResourceTitle(title); err != nil {
			return nil, err
		}
	}
	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	if err := s.stillThere(tk.ID); err != nil {
		return nil, err
	}
	r, err := s.Resources.Create(&domain.TicketResource{
		TicketID: tk.ID, ProjectID: tk.ProjectID, Kind: domain.ResourceLink, Title: t, URL: u,
	})
	if err != nil {
		return nil, err
	}
	s.resourceAdded(ctx, r)
	return r, nil
}

// RetitleTicketResource renames a resource; the same title again changes
// nothing.
func (s *Service) RetitleTicketResource(_ context.Context, id, title string) (*domain.TicketResource, error) {
	if err := s.resourcesReady(); err != nil {
		return nil, err
	}
	t, err := domain.ResourceTitle(title)
	if err != nil {
		return nil, err
	}
	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	existing := s.Resources.Get(id)
	if existing == nil {
		return nil, errResourceNotFound
	}
	if existing.Title == t {
		s.withContentURL(existing)
		return existing, nil
	}
	r, err := s.Resources.SetTitle(id, t)
	if err != nil {
		return nil, err
	}
	s.withContentURL(r)
	s.announceChange(ports.TicketResourceUpdated(r))
	return r, nil
}

// DeleteTicketResource removes the record, then the bytes; bytes that will
// not go are logged, the record is gone either way.
func (s *Service) DeleteTicketResource(_ context.Context, id string) error {
	if err := s.resourcesReady(); err != nil {
		return err
	}
	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	existing := s.Resources.Get(id)
	if existing == nil {
		return errResourceNotFound
	}
	if err := s.Resources.Delete(id); err != nil {
		return err
	}
	if existing.IsUpload() {
		s.dropBlob(id)
	}
	s.announceChange(ports.TicketResourceRemoved(existing))
	return nil
}

func (s *Service) dropBlob(id string) {
	if err := s.Blobs.Delete(id); err != nil {
		slog.Warn("remove a resource's bytes", "resource", id, "err", err)
	}
}

// upload is the resource id names when it has bytes.
func (s *Service) upload(id string) (*domain.TicketResource, error) {
	if err := s.resourcesReady(); err != nil {
		return nil, err
	}
	r := s.Resources.Get(id)
	if r == nil {
		return nil, errResourceNotFound
	}
	if !r.IsUpload() {
		return nil, &domain.StructuredError{Code: "RESOURCE_NOT_FOUND", Message: "a link has no content; open its url"}
	}
	s.withContentURL(r)
	return r, nil
}

func (s *Service) OpenTicketResource(_ context.Context, id string) (*ports.ResourceContent, error) {
	r, err := s.upload(id)
	if err != nil {
		return nil, err
	}
	b, err := s.Blobs.Open(id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, &domain.StructuredError{Code: "RESOURCE_NOT_FOUND", Message: "the resource's content is missing"}
	}
	if err != nil {
		return nil, err
	}
	return &ports.ResourceContent{Resource: r, Content: b.Content, ModTime: b.ModTime}, nil
}

func (s *Service) TicketResourcePath(_ context.Context, id string) (string, error) {
	if _, err := s.upload(id); err != nil {
		return "", err
	}
	return s.Blobs.Path(id), nil
}

// dropTaskResources removes a deleted task's resources, their bytes and its
// comments. The ticket's own deletion on the feed covers them, so nothing
// is announced. Failures are logged: the task is gone already.
func (s *Service) dropTaskResources(ticketID string) {
	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	if s.Resources != nil {
		ids, err := s.Resources.DeleteByTicket(ticketID)
		if err != nil {
			slog.Warn("remove a deleted task's resources", "ticket", ticketID, "err", err)
		}
		if s.Blobs != nil {
			for _, id := range ids {
				s.dropBlob(id)
			}
		}
	}
	if s.Comments != nil {
		if err := s.Comments.DeleteByTicket(ticketID); err != nil {
			slog.Warn("remove a deleted task's comments", "ticket", ticketID, "err", err)
		}
	}
}
