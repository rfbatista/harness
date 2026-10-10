package httpclient

import (
	"context"
	"net/url"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var _ ports.GitCredentials = (*GitCredentials)(nil)

// GitCredentials is the git credentials context over its /api endpoints.
type GitCredentials struct{ c *Client }

// NewGitCredentials returns the adapter over c.
func NewGitCredentials(c *Client) *GitCredentials { return &GitCredentials{c: c} }

func (g *GitCredentials) ListGitCredentials(ctx context.Context) (*ports.GitCredentialList, error) {
	var out ports.GitCredentialList
	if err := g.c.get(ctx, "/api/list_git_credentials", nil, &out); err != nil {
		return nil, err
	}
	if out.Credentials == nil {
		out.Credentials = []*domain.GitCredential{}
	}
	return &out, nil
}

// secrets are the request's secret fields in clear: domain.Secret writes
// itself as [redacted], so they travel as plain strings.
func secrets(body map[string]any, token, privateKey, passphrase domain.Secret) map[string]any {
	for k, v := range map[string]domain.Secret{"token": token, "private_key": privateKey, "passphrase": passphrase} {
		if v != "" {
			body[k] = v.Reveal()
		}
	}
	return body
}

func (g *GitCredentials) CreateGitCredential(ctx context.Context, in ports.CreateGitCredential) (*domain.GitCredential, error) {
	body := secrets(map[string]any{
		"name": in.Name, "kind": in.Kind, "host": in.Host, "username": in.Username,
		"generate": in.Generate, "is_default": in.IsDefault,
	}, in.Token, in.PrivateKey, in.Passphrase)
	var out struct {
		Credential *domain.GitCredential `json:"credential"`
	}
	if err := g.c.post(ctx, "/api/create_git_credential", body, &out); err != nil {
		return nil, err
	}
	return out.Credential, nil
}

func (g *GitCredentials) UpdateGitCredential(ctx context.Context, in ports.UpdateGitCredential) (*domain.GitCredential, error) {
	body := map[string]any{"credential_id": in.CredentialID}
	if in.Name != nil {
		body["name"] = *in.Name
	}
	if in.Host != nil {
		body["host"] = *in.Host
	}
	if in.Username != nil {
		body["username"] = *in.Username
	}
	if in.IsDefault != nil {
		body["is_default"] = *in.IsDefault
	}
	var out struct {
		Credential *domain.GitCredential `json:"credential"`
	}
	if err := g.c.post(ctx, "/api/update_git_credential", secrets(body, in.Token, in.PrivateKey, in.Passphrase), &out); err != nil {
		return nil, err
	}
	return out.Credential, nil
}

func (g *GitCredentials) DeleteGitCredential(ctx context.Context, credentialID string) error {
	return g.c.post(ctx, "/api/delete_git_credential", map[string]string{"credential_id": credentialID}, nil)
}

func (g *GitCredentials) TestGitCredential(ctx context.Context, credentialID, repositoryID string) (*ports.GitCredentialTest, error) {
	var out ports.GitCredentialTest
	body := map[string]string{"credential_id": credentialID}
	if repositoryID != "" {
		body["repository_id"] = repositoryID
	}
	if err := g.c.post(ctx, "/api/test_git_credential", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (g *GitCredentials) GetGitIdentity(ctx context.Context) (*domain.GitIdentity, error) {
	var out struct {
		Identity *domain.GitIdentity `json:"identity"`
	}
	if err := g.c.get(ctx, "/api/get_git_identity", nil, &out); err != nil {
		return nil, err
	}
	return out.Identity, nil
}

func (g *GitCredentials) UpdateGitIdentity(ctx context.Context, name, email string) (*domain.GitIdentity, error) {
	var out struct {
		Identity *domain.GitIdentity `json:"identity"`
	}
	if err := g.c.post(ctx, "/api/update_git_identity", domain.GitIdentity{Name: name, Email: email}, &out); err != nil {
		return nil, err
	}
	return out.Identity, nil
}

func (g *GitCredentials) GetRepositoryCredential(ctx context.Context, repositoryID string) (*domain.RepositoryCredential, error) {
	var out domain.RepositoryCredential
	if err := g.c.get(ctx, "/api/get_repository_credential", url.Values{"repository_id": {repositoryID}}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (g *GitCredentials) ListRepositoryCredentials(ctx context.Context, projectID string) ([]*domain.RepositoryCredential, error) {
	var out struct {
		Repositories []*domain.RepositoryCredential `json:"repositories"`
	}
	if err := g.c.get(ctx, "/api/list_repository_credentials", url.Values{"project_id": {projectID}}, &out); err != nil {
		return nil, err
	}
	if out.Repositories == nil {
		out.Repositories = []*domain.RepositoryCredential{}
	}
	return out.Repositories, nil
}

func (g *GitCredentials) SetRepositoryCredential(ctx context.Context, repositoryID string, credentialID *string) (*domain.RepositoryCredential, error) {
	var out domain.RepositoryCredential
	in := domain.RepositoryCredentialBinding{RepositoryID: repositoryID, CredentialID: credentialID}
	if err := g.c.post(ctx, "/api/set_repository_credential", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// gitCredentialEvent is an /api/project_events event as far as credentials
// care; project events carry neither key.
type gitCredentialEvent struct {
	Credential *domain.GitCredential `json:"git_credential"`
	Identity   *domain.GitIdentity   `json:"git_identity"`
	Deleted    bool                  `json:"deleted"`
}

// FollowGitCredentials follows /api/project_events, keeping the credential
// and identity changes, until ctx ends or the stream does.
func (g *GitCredentials) FollowGitCredentials(ctx context.Context) (<-chan ports.GitCredentialChange, error) {
	events, err := follow(ctx, g.c, "/api/project_events", func(e gitCredentialEvent) bool {
		return e.Credential != nil || e.Identity != nil
	})
	if err != nil {
		return nil, err
	}
	out := make(chan ports.GitCredentialChange, 64)
	go func() {
		defer close(out)
		for e := range events {
			select {
			case out <- ports.GitCredentialChange{Credential: e.Credential, Identity: e.Identity, Deleted: e.Deleted}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
