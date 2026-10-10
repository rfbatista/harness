// Package code renders the code views' first paint from ports.CodeBrowser in
// process: the repository code page, the session's Code tab and the diffs on
// the history pages. The browser (web/src/modules/code) takes over from the
// seed each page embeds and reads /api/code/* after that.
package code

import (
	"net/http"
	"net/url"
	"strings"
)

// Views of a code page.
const (
	ViewChanges = "changes"
	ViewFiles   = "files"
	ViewMap     = "map"
)

// Route is a code URL (UX §2.4); routes.js reads and writes the same ones:
//
//	/projects/{p}/tasks/{t}/sessions/{s}/code[/changes|/files/{path}|/map][?scope=&workspace=&commit=]
//	/projects/{p}/repositories/{r}/code[/files/{path}|/map][?ref=]
type Route struct {
	Kind         string `json:"kind"` // "session" or "repository"
	ProjectID    string `json:"project_id"`
	TaskID       string `json:"task_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	RepositoryID string `json:"repository_id,omitempty"`
	// View is "" (the default), changes, files or map.
	View      string `json:"view"`
	Path      string `json:"path"`
	Scope     string `json:"scope,omitempty"`
	Ref       string `json:"ref,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Commit    string `json:"commit,omitempty"`
}

// SessionRoute reads a session code URL; view is the route's view (the
// mux pattern knows it).
func SessionRoute(r *http.Request, view string) Route {
	q := r.URL.Query()
	rt := Route{
		Kind: "session", ProjectID: r.PathValue("project"), TaskID: r.PathValue("task"), SessionID: r.PathValue("session"),
		View: view, Path: cleanPath(r.PathValue("path")), Workspace: q.Get("workspace"), Commit: q.Get("commit"),
	}
	if view == ViewChanges {
		rt.Scope = q.Get("scope")
	}
	return rt
}

// RepositoryRoute reads a repository code URL.
func RepositoryRoute(r *http.Request, view string) Route {
	return Route{
		Kind: "repository", ProjectID: r.PathValue("project"), RepositoryID: r.PathValue("repository"),
		View: view, Path: cleanPath(r.PathValue("path")), Ref: r.URL.Query().Get("ref"),
	}
}

func cleanPath(p string) string {
	return strings.Trim(p, "/")
}

// Base is the route's code URLs' prefix.
func (r Route) Base() string {
	if r.Kind == "session" {
		return "/projects/" + url.PathEscape(r.ProjectID) + "/tasks/" + url.PathEscape(r.TaskID) + "/sessions/" + url.PathEscape(r.SessionID) + "/code"
	}
	return RepositoryHref(r.ProjectID, r.RepositoryID)
}

// Href is the route's URL, with hash ("#L12") when given.
func (r Route) Href(hash string) string {
	p := r.Base()
	switch r.View {
	case ViewFiles:
		p += "/files"
		if r.Path != "" {
			p += "/" + escapePath(r.Path)
		}
	case ViewChanges:
		p += "/changes"
	case ViewMap:
		p += "/map"
	}
	q := url.Values{}
	if r.Kind == "session" {
		if r.View == ViewChanges && r.Scope != "" && r.Scope != "all" {
			q.Set("scope", r.Scope)
		}
		if r.Workspace != "" {
			q.Set("workspace", r.Workspace)
		}
		if r.Commit != "" {
			q.Set("commit", r.Commit)
		}
	} else if r.Ref != "" {
		q.Set("ref", r.Ref)
	}
	if len(q) > 0 {
		p += "?" + q.Encode()
	}
	if hash != "" && !strings.HasPrefix(hash, "#") {
		hash = "#" + hash
	}
	return p + hash
}

// With is the route showing another view (and path, for files).
func (r Route) With(view, path string) Route {
	r.View, r.Path = view, path
	if view != ViewChanges {
		r.Scope = ""
	}
	return r
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// IndexHref is the project's Code page (the rail's entry).
func IndexHref(projectID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/code"
}

// RepositoryHref is a repository's code page.
func RepositoryHref(projectID, repositoryID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/repositories/" + url.PathEscape(repositoryID) + "/code"
}

// SessionHref is a session's Code tab.
func SessionHref(projectID, taskID, sessionID string) string {
	return Route{Kind: "session", ProjectID: projectID, TaskID: taskID, SessionID: sessionID}.Href("")
}
