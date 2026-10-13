package vshell

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// contents is every file a session's shell should hold but the index, in
// the contract's layout. Files named after what they hold get their path
// when the builder places them.
func (s *Service) contents(ctx context.Context, sessionID string, tk *domain.Ticket) (*builder, error) {
	b := &builder{now: time.Now()}

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
	b.fixed(ports.ShellFile{Path: "README.md", Kind: ports.ShellReadme, Title: "README", UpdatedAt: b.now}, []byte(readme))
	b.fixed(ports.ShellFile{Path: "task/task.json", Kind: ports.ShellTask, ID: tk.ID, Scope: "task", Title: tk.Title, UpdatedAt: tk.UpdatedAt}, append(taskJSON, '\n'))

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
	return b, nil
}

// file is one file of a shell: its index row, and its bytes or how to get
// them.
type file struct {
	meta     ports.ShellFile
	content  []byte
	readOnly bool

	// load reads the bytes, only when the shell does not hold this version
	// already; nil when content is in hand. meta.Version may be empty until
	// then, and UpdatedAt then tells whether the shell's copy is current.
	load func() ([]byte, error)
	// generate makes the bytes once every other file is placed and loaded.
	generate func() []byte
	// A named file's path is chosen in folder from name, or kept where the
	// shell already has it while keep allows; a fixed one has meta.Path.
	folder, name string
	keep         func(path string) bool
	// dropped: its bytes could not be read, so it is left out.
	dropped bool
}

// builder collects a shell's files.
type builder struct {
	files []*file
	now   time.Time
}

// fixed keeps a read-only file at meta.Path; its version is its content's.
func (b *builder) fixed(meta ports.ShellFile, content []byte) {
	meta.Version = Version(content)
	b.files = append(b.files, &file{meta: meta, content: content, readOnly: true})
}

// named adds f, to be placed in folder under name.
func (b *builder) named(folder, name string, f *file) {
	f.folder, f.name = folder, name
	if f.load == nil && f.meta.Version == "" {
		f.meta.Version = Version(f.content)
	}
	b.files = append(b.files, f)
}

// generated adds a read-only file at meta.Path whose bytes are made last.
func (b *builder) generated(meta ports.ShellFile, gen func() []byte) {
	b.files = append(b.files, &file{meta: meta, readOnly: true, generate: gen})
}

// place gives each named file its path. One the shell already holds (prev:
// fileKey to path) keeps that path while it is in the same folder and its
// keep rule allows, so a file is not renamed under the agent; the rest are
// named in the order they were added (oldest first), with -<short id> on a
// clash, and never at a path held for an agent's change.
func (b *builder) place(prev map[string]string, held func(string) bool) {
	taken := map[string]bool{indexFile: true}
	for _, f := range b.files {
		if f.folder == "" {
			taken[f.meta.Path] = true
		}
	}
	placed := map[*file]bool{}
	for _, f := range b.files {
		p, ok := prev[fileKey(f.meta)]
		if f.folder == "" || !ok || taken[p] || !strings.HasPrefix(p, f.folder) || (f.keep != nil && !f.keep(p)) {
			continue
		}
		f.meta.Path, taken[p], placed[f] = p, true, true
	}
	for _, f := range b.files {
		if f.folder == "" || placed[f] {
			continue
		}
		f.meta.Path = Unique(f.folder+f.name, f.meta.ID, func(q string) bool { return taken[q] || held(q) })
		taken[f.meta.Path] = true
	}
}

// fileKey names what a file holds, whatever its path.
func fileKey(f ports.ShellFile) string { return f.Kind + "|" + f.ID }

// documents: those linked to the task with scope task in task/docs, every
// project document in project/docs; oldest first, so the older of two with
// one title keeps the plain name. A document keeps its file while the
// file's extension still goes with its format.
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
			format := d.Format
			b.named(set.folder, DocumentFileName(d.Title, d.Format), &file{
				meta: ports.ShellFile{Kind: ports.ShellDocument, ID: d.ID, Scope: string(set.scope),
					Version: domain.DocumentVersion(d.Content), Title: d.Title, UpdatedAt: d.UpdatedAt},
				content: []byte(d.Content),
				keep:    func(p string) bool { return DocumentFormatOf(p) == format },
			})
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
		b.named("task/comments/", CommentFileName(c), &file{
			meta:    ports.ShellFile{Kind: ports.ShellComment, ID: c.ID, Scope: "task", Title: CommentAuthor(c), UpdatedAt: c.UpdatedAt},
			content: []byte(c.Body), readOnly: !c.WrittenBy(sessionID),
		})
	}
	return nil
}

// resources: uploads under their names, links listed in links.md; all
// read-only. An upload's bytes are read only when the shell lacks them.
func (b *builder) resources(ctx context.Context, src ports.TaskResourceFiles, ticketID string) error {
	list, err := src.ListTicketResources(ctx, ticketID)
	if err != nil {
		return err
	}
	var links strings.Builder
	links.WriteString("# Links\n\nLinks the person added to this task.\n\n")
	for _, r := range list {
		if !r.IsUpload() {
			fmt.Fprintf(&links, "- [%s](%s)\n", r.Title, r.URL)
			continue
		}
		id := r.ID
		b.named("task/resources/", ResourceFileName(r), &file{
			meta:     ports.ShellFile{Kind: ports.ShellResource, ID: r.ID, Scope: "task", Title: r.Title, UpdatedAt: r.UpdatedAt},
			readOnly: true,
			load:     func() ([]byte, error) { return readResource(ctx, src, id) },
		})
	}
	b.fixed(ports.ShellFile{Path: "task/resources/links.md", Kind: ports.ShellLinks, Scope: "task", Title: "Links", UpdatedAt: b.now}, []byte(links.String()))
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
// the task listed in attached.md. An artifact's bytes are read only when the
// shell lacks its revision.
func (b *builder) designs(ctx context.Context, src ports.ArtifactReader, tk *domain.Ticket) error {
	onTask, err := src.ListArtifacts(ctx, ports.ArtifactFilter{TicketID: tk.ID})
	if err != nil {
		return err
	}
	inProject, err := src.ListArtifacts(ctx, ports.ArtifactFilter{ProjectID: tk.ProjectID, Scope: domain.ArtifactScopeProject})
	if err != nil {
		return err
	}

	where := map[string]*file{} // by artifact id
	put := func(folder string, a *domain.Artifact) {
		if a.Kind == domain.ArtifactURL {
			return
		}
		id := a.ID
		f := &file{
			meta: ports.ShellFile{Kind: ports.ShellArtifact, ID: a.ID, Scope: string(a.Scope),
				Version: strconv.Itoa(a.Revision), Title: a.Title, UpdatedAt: a.UpdatedAt},
			load: func() ([]byte, error) { return readArtifact(ctx, src, id) },
		}
		where[a.ID] = f
		b.named(folder, ArtifactFileName(a), f)
	}
	var taskURLs strings.Builder
	taskURLs.WriteString("# Design URLs\n\nDev-server URLs published on this task, not files.\n\n")
	for _, a := range oldestFirst(onTask) {
		if a.TicketID != tk.ID || a.Scope == domain.ArtifactScopeProject {
			continue
		}
		if a.Kind == domain.ArtifactURL {
			fmt.Fprintf(&taskURLs, "- [%s](%s)\n", a.Title, a.URL)
			continue
		}
		put("task/designs/", a)
	}
	b.fixed(ports.ShellFile{Path: "task/designs/urls.md", Kind: ports.ShellDesignURL, Scope: "task", Title: "Design URLs", UpdatedAt: b.now}, []byte(taskURLs.String()))
	var urls strings.Builder
	urls.WriteString("# Design URLs\n\nProject design assets that are URLs, not files.\n\n")
	urlsFile := &file{meta: ports.ShellFile{Path: "project/designs/urls.md"}}
	for _, a := range oldestFirst(inProject) {
		if a.Kind == domain.ArtifactURL {
			fmt.Fprintf(&urls, "- [%s](%s)\n", a.Title, a.URL)
			where[a.ID] = urlsFile
			continue
		}
		put("project/designs/", a)
	}

	var attachedHere []*domain.Artifact
	for _, a := range oldestFirst(onTask) {
		if a.Scope == domain.ArtifactScopeProject && a.AttachedTo(tk.ID) {
			attachedHere = append(attachedHere, a)
		}
	}
	b.generated(ports.ShellFile{Path: "task/designs/attached.md", Kind: ports.ShellAttached, Scope: "task", Title: "Attached project assets", UpdatedAt: b.now}, func() []byte {
		var attached strings.Builder
		attached.WriteString("# Attached project assets\n\nProject design assets attached to this task: title → where it is in this shell.\n\n")
		for _, a := range attachedHere {
			if f := where[a.ID]; f != nil && !f.dropped {
				fmt.Fprintf(&attached, "- %s → %s\n", a.Title, f.meta.Path)
			}
		}
		return []byte(attached.String())
	})
	b.fixed(ports.ShellFile{Path: "project/designs/urls.md", Kind: ports.ShellDesignURL, Scope: "project", Title: "Design URLs", UpdatedAt: b.now}, []byte(urls.String()))
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
