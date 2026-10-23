package web

import (
	"context"
	"strings"
	"testing"

	"operators-mcp/internal/application/gitcredentials/gitcredentialstest"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const pageSecret = "ghp_pageSecretNeverRendered9876"

// credentialsWorld is the board with a git credentials store holding a
// default GitHub token and a generated SSH key.
func credentialsWorld(t *testing.T) (world, *gitcredentialstest.Fake) {
	t.Helper()
	ctx := context.Background()
	fake := gitcredentialstest.New()
	if _, err := fake.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "work-token", Kind: domain.GitHTTPSToken, Host: "github.com", Token: pageSecret, IsDefault: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.CreateGitCredential(ctx, ports.CreateGitCredential{Name: "laptop-key", Kind: domain.GitSSHKey, Host: "github.com", Generate: true}); err != nil {
		t.Fatal(err)
	}
	w := board()
	w.creds = fake
	return w, fake
}

func TestGitCredentialsPageListsByHostAndNeverRendersASecret(t *testing.T) {
	w, _ := credentialsWorld(t)
	rec := get(t, newTestHandler(t, w), "/git-credentials")
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("got %d:\n%s", rec.Code, body)
	}
	for _, want := range []string{
		`x-data="gitCredentialsPage"`,
		`<script id="credentials-seed" type="application/json">`,
		`"locked":false`,
		`"identity":null`,
		`<span class="[ font-mono ]">github.com</span>`,
		`>laptop-key<`,
		`>work-token<`,
		`>SSH key<`,
		`>HTTPS token<`,
		`data-tone="signal">Default<`,
		`x-ref="token"`,
		`id="credential-delete"`,
		`<h1>Git credentials</h1>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page is missing %s", want)
		}
	}
	if strings.Contains(body, pageSecret) {
		t.Error("a secret is on the page")
	}
	if strings.Contains(body, `x-model="token"`) || strings.Contains(body, `x-model="privateKey"`) || strings.Contains(body, `x-model="passphrase"`) {
		t.Error("a secret input is bound to component state")
	}
	seed := seedOf(t, body, "credentials-seed")
	list, _ := seed["credentials"].([]any)
	if len(list) != 2 {
		t.Fatalf("seed credentials = %v", seed["credentials"])
	}
	key, _ := list[0].(map[string]any)
	if key["name"] != "laptop-key" || key["public_key"] == nil || key["kind"] != "ssh_key" {
		t.Errorf("the seed's first credential is the key with its public half: %v", key)
	}
}

func TestGitCredentialsPageSaysWhatIsMissing(t *testing.T) {
	w := board()
	w.creds = gitcredentialstest.New()
	body := get(t, newTestHandler(t, w), "/git-credentials").Body.String()
	if !strings.Contains(body, "No git credentials yet") {
		t.Error("an empty store says so")
	}
	if strings.Contains(tagWith(body, "No author is set"), "display: none") {
		t.Error("an unset identity is pointed out on first paint")
	}
	if !strings.Contains(tagWith(body, lockedBanner), "display: none") {
		t.Error("an open store hides the locked banner")
	}
}

func TestGitCredentialsPageShowsALockedStoreOnFirstPaint(t *testing.T) {
	w, fake := credentialsWorld(t)
	fake.Lock()
	body := get(t, newTestHandler(t, w), "/git-credentials").Body.String()
	if !strings.Contains(body, `"locked":true`) {
		t.Error("the seed says the store is locked")
	}
	if banner := tagWith(body, lockedBanner); strings.Contains(banner, "display: none") || !strings.Contains(body, "HARNESS_SECRET_KEY") {
		t.Errorf("the locked banner shows on first paint: %s", banner)
	}
	if !strings.Contains(body, ">work-token<") {
		t.Error("a locked store still lists what it keeps")
	}
}

// lockedBanner finds the locked banner's tag.
const lockedBanner = `x-show="locked"`

func TestGitCredentialsPageOpensTheAddFormForAHost(t *testing.T) {
	w, _ := credentialsWorld(t)
	h := newTestHandler(t, w)
	body := get(t, h, "/git-credentials?host=gitlab.example.com&kind=ssh_key").Body.String()
	if !strings.Contains(body, `data-host="gitlab.example.com"`) || !strings.Contains(body, `data-kind="ssh_key"`) {
		t.Error("the link's host and kind reach the page")
	}
	if body := get(t, h, "/git-credentials?host=x&kind=password").Body.String(); !strings.Contains(body, `data-kind=""`) {
		t.Error("an unknown kind is dropped")
	}
	if body := get(t, h, "/git-credentials?credential=gc-9").Body.String(); !strings.Contains(body, `data-credential="gc-9"`) {
		t.Error("a credential to show reaches the page")
	}
}

func TestTopBarLeadsToGitCredentialsOnceTheyAreServed(t *testing.T) {
	w, _ := credentialsWorld(t)
	h := newTestHandler(t, w)
	link := tagWith(get(t, h, "/projects/p1").Body.String(), `aria-label="Git credentials"`)
	if !strings.Contains(link, `href="/git-credentials"`) || strings.Contains(link, "aria-current") {
		t.Errorf("the top bar links to Git credentials: %s", link)
	}
	if link := tagWith(get(t, h, "/git-credentials").Body.String(), `aria-label="Git credentials"`); !strings.Contains(link, `aria-current="page"`) {
		t.Errorf("the link is current on its page: %s", link)
	}
	without := newTestHandler(t, board())
	if strings.Contains(get(t, without, "/projects/p1").Body.String(), `aria-label="Git credentials"`) {
		t.Error("no link when credentials are not served")
	}
	if body := get(t, without, "/git-credentials").Body.String(); body != "legacy designer" {
		t.Error("the page is not served without credentials")
	}
}

func TestRepositoriesPageSaysWhichCredentialEachRepositoryUses(t *testing.T) {
	w, fake := credentialsWorld(t)
	projectID, harness := fake.AddRepository("", "harness", "https://github.com/acme/harness.git")
	_, mirror := fake.AddRepository(projectID, "mirror", "https://gitlab.example.com/acme/mirror.git")
	_, scratch := fake.AddRepository(projectID, "scratch", "")
	w.projects = append(w.projects, &domain.Project{ID: projectID, Name: "creds"})
	w.repos = []*domain.Repository{
		{ID: harness, ProjectID: projectID, Name: "harness"},
		{ID: mirror, ProjectID: projectID, Name: "mirror"},
		{ID: scratch, ProjectID: projectID, Name: "scratch"},
	}
	body := get(t, newTestHandler(t, w), "/projects/"+projectID+"/repositories").Body.String()
	for _, want := range []string{
		`<script id="repository-credentials-seed" type="application/json">`,
		`"project_id":"` + projectID + `"`,
		`Credential: <b>work-token</b> · automatic (default for github.com)`,
		`No token or SSH key for gitlab.example.com: pushes from this repository will fail.`,
		`Pull requests: no token for gitlab.example.com, so they will fail.`,
		`<a href="/git-credentials?host=gitlab.example.com&amp;kind=https_token">Add one</a>`,
		`Local only: no remote, no credential needed.`,
		`x-data="repositoryCredential"`,
		`x-data="dropdown"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the repositories page is missing %s", want)
		}
	}
	if strings.Contains(body, pageSecret) {
		t.Error("a secret is on the page")
	}

	plain := board()
	if body := get(t, newTestHandler(t, plain), "/projects/p1/repositories").Body.String(); strings.Contains(body, "repositoryCredential") || strings.Contains(body, "repository-credentials-seed") {
		t.Error("without credentials served, the rows go without")
	}
}
