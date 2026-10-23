package orchestration

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fakeGitAuth records the sessions it gave a git directory and removed.
type fakeGitAuth struct {
	mu       sync.Mutex
	note     string
	err      error
	prepared []string // "session/repository"
	ended    []string
}

func (f *fakeGitAuth) SessionEnv(_ context.Context, sessionID, repositoryID string) (*ports.SessionGit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.prepared = append(f.prepared, sessionID+"/"+repositoryID)
	return &ports.SessionGit{
		Env:     []string{"GIT_CONFIG_GLOBAL=/data/git/sessions/" + sessionID + "/gitconfig", "GH_TOKEN=harness-no-credential"},
		EnvFile: "/data/git/sessions/" + sessionID + "/env.sh",
		Note:    f.note,
	}, nil
}

func (f *fakeGitAuth) EndSession(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended = append(f.ended, sessionID)
}

func (f *fakeGitAuth) Resolve(context.Context, string, string) (*ports.GitResolution, error) {
	return &ports.GitResolution{}, nil
}

func (f *fakeGitAuth) Operation(context.Context, string, string) (*ports.GitOperation, error) {
	return nil, errors.New("not used")
}

func (f *fakeGitAuth) GitHubToken(context.Context, string, string) (*domain.GitCredential, domain.Secret, error) {
	return nil, "", nil
}

func (f *fakeGitAuth) Used(string) {}

func (f *fakeGitAuth) endedSessions() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ended)
}

func TestASessionRunsWithItsGitDirectory(t *testing.T) {
	svc, _ := newInteractiveService(t)
	git := &fakeGitAuth{note: "Git: no stored credential for github.com (https_token), so git push and fetch to origin will fail."}
	svc.GitAuth = git
	sess, launch := startInteractive(t, svc, InteractiveRequest{})

	if !slices.Equal(git.prepared, []string{sess.ID + "/r1"}) {
		t.Fatalf("git directories prepared %v, want one for the session's repository", git.prepared)
	}
	if !slices.Contains(launch.Spec.Env, "GIT_CONFIG_GLOBAL=/data/git/sessions/"+sess.ID+"/gitconfig") {
		t.Fatalf("env %v lacks the session's git config", launch.Spec.Env)
	}
	if launch.Spec.EnvFile != "/data/git/sessions/"+sess.ID+"/env.sh" {
		t.Fatalf("env file %q", launch.Spec.EnvFile)
	}
	if system, _ := flag(launch.Args, "--append-system-prompt"); !strings.Contains(system, git.note) {
		t.Fatalf("the brief does not carry the git note:\n%s", system)
	}

	// The session ends: its directory goes. A resume makes it again.
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(git.endedSessions(), sess.ID) {
		t.Fatalf("ended %v, want the session's directory removed", git.endedSessions())
	}
	_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(git.prepared) != 2 || spec.EnvFile != "/data/git/sessions/"+sess.ID+"/env.sh" ||
		!slices.Contains(spec.Env, "GH_TOKEN=harness-no-credential") {
		t.Fatalf("resume: prepared %v, spec env %v file %q", git.prepared, spec.Env, spec.EnvFile)
	}
}

func TestASessionWhoseGitDirectoryFailsDoesNotStart(t *testing.T) {
	svc, prov := newInteractiveService(t)
	svc.GitAuth = &fakeGitAuth{err: errors.New("disk full")}
	_, _, err := svc.StartInteractive(context.Background(), InteractiveRequest{ProjectID: "p1", RepositoryID: "r1", TicketID: "tk1"})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("start = %v, want the git directory's failure", err)
	}
	if len(prov.discarded) != 1 {
		t.Fatalf("discarded %v: the worktree of a session that never started must go", prov.discarded)
	}
}

func TestWithoutGitAuthNothingChanges(t *testing.T) {
	svc, _ := newInteractiveService(t)
	_, launch := startInteractive(t, svc, InteractiveRequest{})
	if launch.Spec.EnvFile != "" {
		t.Fatalf("env file %q without GitAuth", launch.Spec.EnvFile)
	}
}

func TestASessionStartsWithTheAgentCredentialInItsEnv(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.AgentCredential = func() []string { return []string{"CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat-secret"} }
	sess, launch := startInteractive(t, svc, InteractiveRequest{})
	if !slices.Contains(launch.Spec.Env, "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat-secret") {
		t.Fatalf("env %v lacks the agent credential", launch.Spec.Env)
	}
	if got := svc.sessions.Get(sess.ID); strings.Contains(fmt.Sprintf("%+v", got), "sk-ant-oat-secret") {
		t.Fatalf("the session record carries the credential: %+v", got)
	}

	// A resume reads the credential as it is now.
	svc.AgentCredential = func() []string { return []string{"ANTHROPIC_API_KEY=sk-ant-api-new"} }
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	_, spec, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(spec.Env, "ANTHROPIC_API_KEY=sk-ant-api-new") || slices.Contains(spec.Env, "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat-secret") {
		t.Fatalf("resume env %v", spec.Env)
	}
}

func TestWithoutAnAgentCredentialTheEnvCarriesNone(t *testing.T) {
	svc, _ := newInteractiveService(t)
	svc.AgentCredential = func() []string { return nil }
	_, launch := startInteractive(t, svc, InteractiveRequest{})
	for _, e := range launch.Spec.Env {
		if strings.HasPrefix(e, "CLAUDE_CODE_OAUTH_TOKEN=") || strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			t.Fatalf("env carries %q without a credential", e)
		}
	}
}

// A restart under the same id while the first start's cleanup is pending: the
// session's git directory has one path per session, so the old cleanup must
// not remove what the restart is using; the restart's own cleanup does.
func TestAFirstStartsCleanupLeavesTheRestartsGitDirectoryAlone(t *testing.T) {
	svc, _ := newInteractiveService(t)
	git := &fakeGitAuth{}
	svc.GitAuth = git
	sess, _ := startInteractive(t, svc, InteractiveRequest{})
	if err := svc.sessions.UpdateStatus(sess.ID, domain.SessionDone); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ResumeInteractive(context.Background(), ports.ResumeRequest{SessionID: sess.ID}); err != nil {
		t.Fatal(err)
	}
	if got := git.endedSessions(); len(got) != 0 {
		t.Fatalf("the first start's cleanup removed the git directory %v the restart uses", got)
	}
	if _, err := svc.EndInteractive(context.Background(), sess.ID, 0, false); err != nil {
		t.Fatal(err)
	}
	if got := git.endedSessions(); !slices.Equal(got, []string{sess.ID}) {
		t.Fatalf("ended %v, want the restart's own cleanup to remove it once", got)
	}
}
