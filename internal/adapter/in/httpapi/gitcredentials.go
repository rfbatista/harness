package httpapi

import (
	"context"
	"net/http"

	"github.com/labstack/echo/v4"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// The git credentials every remote operation of the harness uses (see
// "Contract: Harness server ↔ Web UI — git credentials"). Bodies are the
// port's types unchanged, so a page seed and an API body are the same bytes.
// Secrets come in and never go out.

var errGitCredentialsOff = echo.NewHTTPError(http.StatusServiceUnavailable, "git credentials not configured")

func (h *Handler) handleListGitCredentials(c echo.Context) error {
	if h.gitCredentials == nil {
		return errGitCredentialsOff
	}
	list, err := h.gitCredentials.ListGitCredentials(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"credentials": list})
}

func (h *Handler) handleCreateGitCredential(c echo.Context) error {
	if h.gitCredentials == nil {
		return errGitCredentialsOff
	}
	var in ports.CreateGitCredential
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	cred, err := h.gitCredentials.CreateGitCredential(c.Request().Context(), in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"credential": cred})
}

func credentialIDRequired() error {
	return domain.GitCredentialError("INVALID_INPUT", "credential_id is required")
}

func (h *Handler) handleUpdateGitCredential(c echo.Context) error {
	if h.gitCredentials == nil {
		return errGitCredentialsOff
	}
	var in ports.UpdateGitCredential
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.CredentialID == "" {
		return credentialIDRequired()
	}
	cred, err := h.gitCredentials.UpdateGitCredential(c.Request().Context(), in)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"credential": cred})
}

func (h *Handler) handleDeleteGitCredential(c echo.Context) error {
	if h.gitCredentials == nil {
		return errGitCredentialsOff
	}
	var in struct {
		CredentialID string `json:"credential_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.CredentialID == "" {
		return credentialIDRequired()
	}
	if err := h.gitCredentials.DeleteGitCredential(c.Request().Context(), in.CredentialID); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) handleTestGitCredential(c echo.Context) error {
	if h.gitCredentials == nil {
		return errGitCredentialsOff
	}
	var in struct {
		CredentialID string `json:"credential_id"`
		RepositoryID string `json:"repository_id"`
	}
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.CredentialID == "" {
		return credentialIDRequired()
	}
	res, err := h.gitCredentials.TestGitCredential(c.Request().Context(), in.CredentialID, in.RepositoryID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, res)
}

func (h *Handler) handleGetGitIdentity(c echo.Context) error {
	if h.gitCredentials == nil {
		return errGitCredentialsOff
	}
	id, err := h.gitCredentials.GetGitIdentity(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"identity": id})
}

func (h *Handler) handleUpdateGitIdentity(c echo.Context) error {
	if h.gitCredentials == nil {
		return errGitCredentialsOff
	}
	var in domain.GitIdentity
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	id, err := h.gitCredentials.UpdateGitIdentity(c.Request().Context(), in.Name, in.Email)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"identity": id})
}

func (h *Handler) handleGetRepositoryCredential(c echo.Context) error {
	if h.gitCredentials == nil {
		return errGitCredentialsOff
	}
	id := c.QueryParam("repository_id")
	if id == "" {
		return domain.GitCredentialError("INVALID_INPUT", "repository_id is required")
	}
	rc, err := h.gitCredentials.GetRepositoryCredential(c.Request().Context(), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, rc)
}

func (h *Handler) handleListRepositoryCredentials(c echo.Context) error {
	if h.gitCredentials == nil {
		return errGitCredentialsOff
	}
	id := c.QueryParam("project_id")
	if id == "" {
		return domain.GitCredentialError("INVALID_INPUT", "project_id is required")
	}
	list, err := h.gitCredentials.ListRepositoryCredentials(c.Request().Context(), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"repositories": list})
}

func (h *Handler) handleSetRepositoryCredential(c echo.Context) error {
	if h.gitCredentials == nil {
		return errGitCredentialsOff
	}
	var in domain.RepositoryCredentialBinding
	if err := bindJSON(c, &in); err != nil {
		return err
	}
	if in.RepositoryID == "" {
		return domain.GitCredentialError("INVALID_INPUT", "repository_id is required")
	}
	rc, err := h.gitCredentials.SetRepositoryCredential(c.Request().Context(), in.RepositoryID, in.CredentialID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, rc)
}

// gitCredentialEvent is a credential change as /api/project_events carries
// it: the credential, only its id when deleted, or the identity.
func gitCredentialEvent(ch ports.GitCredentialChange) any {
	switch {
	case ch.Identity != nil:
		return map[string]any{"git_identity": ch.Identity}
	case ch.Deleted:
		return map[string]any{"git_credential": map[string]string{"id": ch.Credential.ID}, "deleted": true}
	}
	return map[string]any{"git_credential": ch.Credential}
}

// catalogEvents merges the project catalog feed with the credential feed
// into one stream of wire events. It closes when ctx ends or either feed
// closes (its follower fell behind), so the client reloads both.
func catalogEvents(ctx context.Context, projects <-chan ports.ProjectCatalogChange, creds <-chan ports.GitCredentialChange) <-chan any {
	out := make(chan any)
	go func() {
		defer close(out)
		for {
			var ev any
			select {
			case <-ctx.Done():
				return
			case ch, ok := <-projects:
				if !ok {
					return
				}
				ev = projectEventOf(ch)
			case ch, ok := <-creds:
				if !ok {
					return
				}
				ev = gitCredentialEvent(ch)
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
