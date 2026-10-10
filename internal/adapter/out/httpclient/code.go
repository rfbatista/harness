package httpclient

import (
	"context"
	"net/url"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.CodeBrowser = (*Code)(nil)

// Code reads code over /api/code/*.
type Code struct{ c *Client }

// NewCode returns the code views adapter over c.
func NewCode(c *Client) *Code { return &Code{c: c} }

func sourceValues(q ports.CodeSourceQuery) url.Values {
	v := url.Values{}
	set(v, "repository_id", q.RepositoryID)
	set(v, "ref", q.Ref)
	set(v, "workspace_id", q.WorkspaceID)
	return v
}

// set adds a parameter that has a value.
func set(v url.Values, key, value string) {
	if value != "" {
		v.Set(key, value)
	}
}

func (c *Code) CodeTree(ctx context.Context, q ports.CodeSourceQuery, dir string) (*domain.CodeTree, error) {
	v := sourceValues(q)
	set(v, "path", dir)
	var out domain.CodeTree
	if err := c.c.get(ctx, "/api/code/tree", v, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Code) CodePaths(ctx context.Context, q ports.CodeSourceQuery) (*domain.CodePaths, error) {
	var out domain.CodePaths
	if err := c.c.get(ctx, "/api/code/paths", sourceValues(q), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Code) CodeFile(ctx context.Context, q ports.CodeSourceQuery, p string) (*domain.CodeFile, error) {
	v := sourceValues(q)
	set(v, "path", p)
	var out domain.CodeFile
	if err := c.c.get(ctx, "/api/code/file", v, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Code) CodeDiff(ctx context.Context, q ports.CodeDiffQuery) (*domain.CodeDiff, error) {
	v := url.Values{}
	set(v, "workspace_id", q.WorkspaceID)
	set(v, "scope", q.Scope)
	set(v, "repository_id", q.RepositoryID)
	set(v, "commit", q.Commit)
	set(v, "base", q.Base)
	set(v, "head", q.Head)
	set(v, "path", q.Path)
	var out domain.CodeDiff
	if err := c.c.get(ctx, "/api/code/diff", v, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Code) CodeMap(ctx context.Context, q ports.CodeSourceQuery) (*domain.CodeMap, error) {
	var out domain.CodeMap
	if err := c.c.get(ctx, "/api/code/map", sourceValues(q), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
