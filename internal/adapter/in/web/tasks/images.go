package tasks

import (
	"context"
	"net/url"
	"path"
	"slices"
	"strings"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/application/vfs"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// imagePaths is where the files a Markdown document may show as images are
// served from, by their path in the virtual shell (task/designs/…,
// task/resources/…, project/designs/…), named as the shell names them: oldest
// first, with -<short id> on a clash. The page knows no session, so it cannot
// see where a session's shell kept a file; each file also answers to its
// -<short id> name, where the shell keeps it once the older file is gone.
type imagePaths map[string]string

// projectImagePaths are the project's file design assets. A list that
// cannot be read leaves its images as written.
func (h Handler) projectImagePaths(ctx context.Context, projectID string) imagePaths {
	paths := shellFixedFiles()
	if h.Artifacts != nil {
		if list, err := h.Artifacts.ListArtifacts(ctx, ports.ArtifactFilter{ProjectID: projectID, Scope: domain.ArtifactScopeProject}); err == nil {
			paths.addDesigns("project/designs/", list)
		}
	}
	return paths
}

// taskImagePaths are the project's file design assets, the task's own file
// designs and its uploads.
func (h Handler) taskImagePaths(ctx context.Context, projectID, taskID string) imagePaths {
	paths := h.projectImagePaths(ctx, projectID)
	if h.Artifacts != nil {
		if list, err := h.Artifacts.ListArtifacts(ctx, ports.ArtifactFilter{TicketID: taskID}); err == nil {
			own := slices.DeleteFunc(slices.Clone(list), func(a *domain.Artifact) bool {
				return a.TicketID != taskID || a.Scope == domain.ArtifactScopeProject
			})
			paths.addDesigns("task/designs/", own)
		}
	}
	if h.Resources != nil {
		if list, err := h.Resources.ListTicketResources(ctx, taskID); err == nil {
			for _, r := range list {
				if r.IsUpload() {
					paths.add("task/resources/"+vfs.ResourceFileName(r), r.ID, httpapi.TicketResourceContentPath(r.ID))
				}
			}
		}
	}
	return paths
}

// shellFixedFiles holds the names the shell gives its own files in the
// folders images come from, so no design or upload takes them: they serve
// nothing.
func shellFixedFiles() imagePaths {
	return imagePaths{"task/designs/urls.md": "", "task/designs/attached.md": "", "project/designs/urls.md": "", "task/resources/links.md": ""}
}

// addDesigns adds the file artifacts of list under folder, oldest first as
// the shell places them; a url artifact is no file.
func (p imagePaths) addDesigns(folder string, list []*domain.Artifact) {
	for _, a := range vfs.OldestFirst(list) {
		if a.Kind != domain.ArtifactURL {
			p.add(folder+vfs.ArtifactFileName(a), a.ID, httpapi.ArtifactViewPath(a.ID))
		}
	}
}

// add puts the file id at name, or at its -<short id> name when name is
// taken, and at that -<short id> name in any case while it is free.
func (p imagePaths) add(name, id, served string) {
	taken := func(q string) bool { _, ok := p[q]; return ok }
	p[vfs.Unique(name, id, taken)] = served
	ext := path.Ext(name)
	if alias := strings.TrimSuffix(name, ext) + "-" + vfs.ShortID(id) + ext; !taken(alias) {
		p[alias] = served
	}
}

// resolver reads an image destination as a shell path, from folder (the
// document's own) and then from the shell root. An address with a scheme
// or a leading slash is no shell path.
func (p imagePaths) resolver(folder string) func(string) (string, bool) {
	return func(dest string) (string, bool) {
		if u, err := url.Parse(dest); err != nil || u.Scheme != "" || strings.HasPrefix(dest, "/") {
			return "", false
		}
		name, err := url.PathUnescape(dest)
		if err != nil {
			name = dest
		}
		for _, q := range []string{path.Join(folder, name), path.Clean(name)} {
			if served, ok := p[q]; ok && served != "" {
				return served, true
			}
		}
		return "", false
	}
}

// documentFolder is where a document sits in the shell: task/docs for a
// task document, project/docs for a project one.
func documentFolder(d *domain.Document) string {
	if scopeOf(d) == domain.DocumentScopeProject {
		return "project/docs"
	}
	return "task/docs"
}
