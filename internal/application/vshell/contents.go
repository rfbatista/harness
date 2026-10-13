package vshell

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// contents is every file a session's shell starts with but the index, at
// its path, in the contract's layout and names.
func (s *Service) contents(ctx context.Context, sessionID string, tk *domain.Ticket) ([]file, error) {
	b := &builder{taken: map[string]bool{indexFile: true}, now: time.Now()}

	taskJSON, err := json.MarshalIndent(struct {
		ID                 string  `json:"id"`
		Title              string  `json:"title"`
		Description        string  `json:"description"`
		Status             string  `json:"status"`
		ArchitectSessionID *string `json:"architect_session_id"`
	}{tk.ID, tk.Title, tk.Description, string(tk.Status), tk.ArchitectSessionID}, "", "  ")
	if err != nil {
		return nil, err
	}
	b.add(ports.ShellFile{Path: "README.md", Kind: ports.ShellReadme, Title: "README", UpdatedAt: b.now}, []byte(readme), true)
	b.add(ports.ShellFile{Path: "task/task.json", Kind: ports.ShellTask, ID: tk.ID, Scope: "task", Title: tk.Title, UpdatedAt: tk.UpdatedAt}, append(taskJSON, '\n'), true)

	b.documents(s.src.Documents, tk)
	if err := b.comments(ctx, s.src.Comments, tk.ID, sessionID); err != nil {
		return nil, err
	}
	if err := b.resources(ctx, s.src.Resources, tk.ID); err != nil {
		return nil, err
	}
	if s.src.Artifacts != nil {
		if err := b.designs(ctx, s.src.Artifacts, tk); err != nil {
			return nil, err
		}
	}
	return b.files, nil
}

// builder collects a shell's files, keeping their paths unique.
type builder struct {
	files []file
	taken map[string]bool
	now   time.Time
}

// add keeps a file at meta.Path; its version is its content's.
func (b *builder) add(meta ports.ShellFile, content []byte, readOnly bool) {
	if meta.Version == "" {
		meta.Version = Version(content)
	}
	b.taken[meta.Path] = true
	b.files = append(b.files, file{meta: meta, content: content, readOnly: readOnly})
}

// unique is p, or p with -<short id> before its extension when p is taken.
func (b *builder) unique(p, id string) string {
	return Unique(p, id, func(q string) bool { return b.taken[q] })
}

// documents: those linked to the task with scope task in task/docs, every
// project document in project/docs; oldest first, so the older of two with
// one title keeps the plain name.
func (b *builder) documents(src ports.DocumentReader, tk *domain.Ticket) {
	var taskDocs []*domain.Document
	for _, d := range src.ListTicketDocuments(tk.ID) {
		if d.Scope != domain.DocumentScopeProject {
			taskDocs = append(taskDocs, d)
		}
	}
	for _, set := range []struct {
		folder string
		scope  domain.DocumentScope
		docs   []*domain.Document
	}{
		{"task/docs/", domain.DocumentScopeTask, taskDocs},
		{"project/docs/", domain.DocumentScopeProject, src.ListDocuments(tk.ProjectID, domain.DocumentScopeProject)},
	} {
		docs := slices.Clone(set.docs)
		slices.SortFunc(docs, func(a, c *domain.Document) int {
			if n := a.CreatedAt.Compare(c.CreatedAt); n != 0 {
				return n
			}
			return strings.Compare(a.ID, c.ID)
		})
		for _, d := range docs {
			b.add(ports.ShellFile{
				Path: b.unique(set.folder+DocumentFileName(d.Title, d.Format), d.ID), Kind: ports.ShellDocument,
				ID: d.ID, Scope: string(set.scope), Version: domain.DocumentVersion(d.Content), Title: d.Title, UpdatedAt: d.UpdatedAt,
			}, []byte(d.Content), false)
		}
	}
}

// comments: one file each, body only; read-only unless this session wrote it.
func (b *builder) comments(ctx context.Context, src ports.TaskCommentReader, ticketID, sessionID string) error {
	list, err := src.ListTicketComments(ctx, ticketID, time.Time{})
	if err != nil {
		return err
	}
	for _, c := range list {
		b.add(ports.ShellFile{
			Path: b.unique("task/comments/"+CommentFileName(c), c.ID), Kind: ports.ShellComment,
			ID: c.ID, Scope: "task", Title: CommentAuthor(c), UpdatedAt: c.UpdatedAt,
		}, []byte(c.Body), !c.WrittenBy(sessionID))
	}
	return nil
}

// resources: uploads under their names, links listed in links.md; all
// read-only. An upload whose bytes cannot be read is left out.
func (b *builder) resources(ctx context.Context, src ports.TaskResourceFiles, ticketID string) error {
	list, err := src.ListTicketResources(ctx, ticketID)
	if err != nil {
		return err
	}
	b.taken["task/resources/links.md"] = true
	var links strings.Builder
	links.WriteString("# Links\n\nLinks the person added to this task.\n\n")
	for _, r := range list {
		if !r.IsUpload() {
			fmt.Fprintf(&links, "- [%s](%s)\n", r.Title, r.URL)
			continue
		}
		content, err := readResource(ctx, src, r.ID)
		if err != nil {
			slog.Warn("leave a resource out of a virtual shell", "resource", r.ID, "err", err)
			continue
		}
		b.add(ports.ShellFile{
			Path: b.unique("task/resources/"+ResourceFileName(r), r.ID), Kind: ports.ShellResource,
			ID: r.ID, Scope: "task", Title: r.Title, UpdatedAt: r.UpdatedAt,
		}, content, true)
	}
	b.add(ports.ShellFile{Path: "task/resources/links.md", Kind: ports.ShellLinks, Scope: "task", Title: "Links", UpdatedAt: b.now}, []byte(links.String()), true)
	return nil
}

func readResource(ctx context.Context, src ports.TaskResourceFiles, id string) ([]byte, error) {
	rc, err := src.OpenTicketResource(ctx, id)
	if err != nil {
		return nil, err
	}
	defer rc.Content.Close()
	return io.ReadAll(io.LimitReader(rc.Content, domain.MaxResourceBytes+1))
}

// designs: the task's own artifacts in task/designs, every project asset in
// project/designs (url ones listed in urls.md), and the assets attached to
// the task listed in attached.md. An artifact whose bytes cannot be read is
// left out.
func (b *builder) designs(ctx context.Context, src ports.ArtifactReader, tk *domain.Ticket) error {
	onTask, err := src.ListArtifacts(ctx, ports.ArtifactFilter{TicketID: tk.ID})
	if err != nil {
		return err
	}
	inProject, err := src.ListArtifacts(ctx, ports.ArtifactFilter{ProjectID: tk.ProjectID, Scope: domain.ArtifactScopeProject})
	if err != nil {
		return err
	}
	b.taken["task/designs/attached.md"] = true
	b.taken["project/designs/urls.md"] = true

	where := map[string]string{} // artifact id -> its path in the shell
	put := func(folder string, a *domain.Artifact) {
		if a.Kind == domain.ArtifactURL {
			return
		}
		content, err := readArtifact(ctx, src, a.ID)
		if err != nil {
			slog.Warn("leave an artifact out of a virtual shell", "artifact", a.ID, "err", err)
			return
		}
		p := b.unique(folder+ArtifactFileName(a), a.ID)
		where[a.ID] = p
		b.add(ports.ShellFile{
			Path: p, Kind: ports.ShellArtifact, ID: a.ID, Scope: string(a.Scope),
			Version: strconv.Itoa(a.Revision), Title: a.Title, UpdatedAt: a.UpdatedAt,
		}, content, false)
	}
	for _, a := range oldestFirst(onTask) {
		if a.TicketID == tk.ID && a.Scope != domain.ArtifactScopeProject {
			put("task/designs/", a)
		}
	}
	var urls strings.Builder
	urls.WriteString("# Design URLs\n\nProject design assets that are URLs, not files.\n\n")
	for _, a := range oldestFirst(inProject) {
		if a.Kind == domain.ArtifactURL {
			fmt.Fprintf(&urls, "- [%s](%s)\n", a.Title, a.URL)
			where[a.ID] = "project/designs/urls.md"
			continue
		}
		put("project/designs/", a)
	}

	var attached strings.Builder
	attached.WriteString("# Attached project assets\n\nProject design assets attached to this task: title → where it is in this shell.\n\n")
	for _, a := range oldestFirst(onTask) {
		if a.Scope == domain.ArtifactScopeProject && a.AttachedTo(tk.ID) && where[a.ID] != "" {
			fmt.Fprintf(&attached, "- %s → %s\n", a.Title, where[a.ID])
		}
	}
	b.add(ports.ShellFile{Path: "task/designs/attached.md", Kind: ports.ShellAttached, Scope: "task", Title: "Attached project assets", UpdatedAt: b.now}, []byte(attached.String()), true)
	b.add(ports.ShellFile{Path: "project/designs/urls.md", Kind: ports.ShellDesignURL, Scope: "project", Title: "Design URLs", UpdatedAt: b.now}, []byte(urls.String()), true)
	return nil
}

func readArtifact(ctx context.Context, src ports.ArtifactReader, id string) ([]byte, error) {
	f, err := src.OpenArtifactFile(ctx, id, "")
	if err != nil {
		return nil, err
	}
	defer f.Content.Close()
	return io.ReadAll(f.Content)
}

func oldestFirst(list []*domain.Artifact) []*domain.Artifact {
	out := slices.Clone(list)
	slices.SortFunc(out, func(a, c *domain.Artifact) int {
		if n := a.CreatedAt.Compare(c.CreatedAt); n != 0 {
			return n
		}
		return strings.Compare(a.ID, c.ID)
	})
	return out
}
