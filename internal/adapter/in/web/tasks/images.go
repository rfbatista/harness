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
// task/resources/…, project/designs/…), named as the shell names them: oldest first, with
// -<short id> on a clash.
type imagePaths map[string]string

// projectImagePaths are the project's file design assets. A list that
// cannot be read leaves its images as written.
func (h Handler) projectImagePaths(ctx context.Context, projectID string) imagePaths {
	paths := imagePaths{}
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

// addDesigns adds the file artifacts of list under folder, oldest first as
// the shell places them; a url artifact is no file.
func (p imagePaths) addDesigns(folder string, list []*domain.Artifact) {
	list = slices.Clone(list)
	slices.SortFunc(list, func(a, c *domain.Artifact) int {
		if n := a.CreatedAt.Compare(c.CreatedAt); n != 0 {
			return n
		}
		return strings.Compare(a.ID, c.ID)
	})
	for _, a := range list {
		if a.Kind != domain.ArtifactURL {
			p.add(folder+vfs.ArtifactFileName(a), a.ID, httpapi.ArtifactViewPath(a.ID))
		}
	}
}

func (p imagePaths) add(name, id, served string) {
	p[vfs.Unique(name, id, func(q string) bool { _, ok := p[q]; return ok })] = served
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
			if served, ok := p[q]; ok {
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
