package workspaces_test

import (
	"errors"
	"testing"

	"github.com/rfbatista/harnesskit/errs"

	"operators-mcp/internal/domain"
)

type memEnvFiles map[string][]*domain.EnvFile

func (m memEnvFiles) List(repositoryID string) []*domain.EnvFile { return m[repositoryID] }
func (m memEnvFiles) Put(f *domain.EnvFile) (*domain.EnvFile, error) {
	m[f.RepositoryID] = append(m[f.RepositoryID], f)
	return f, nil
}
func (m memEnvFiles) Delete(string, string) error         { return nil }
func (m memEnvFiles) DeleteByRepository(id string) error { delete(m, id); return nil }

type recordingIO struct {
	written map[string][]*domain.EnvFile
	err     error
}

func (r *recordingIO) Read(string, string) ([]byte, error) { return nil, nil }
func (r *recordingIO) Write(root string, files []*domain.EnvFile) error {
	if r.err != nil {
		return r.err
	}
	r.written[root] = files
	return nil
}

func TestCreateWritesTheRepositorysEnvFilesIntoTheWorktree(t *testing.T) {
	svc, _, wt, repo := newService(t)
	io := &recordingIO{written: map[string][]*domain.EnvFile{}}
	store := memEnvFiles{repo.ID: {{RepositoryID: repo.ID, Path: ".env", Content: "A=1"}, {RepositoryID: repo.ID, Path: "apps/api/.env", Content: "B=2"}}}
	svc.UseEnvFiles(store, io)

	ws, err := svc.Create(repo.ID, "feature", "", "")
	if err != nil {
		t.Fatal(err)
	}
	got := io.written[ws.Path]
	if len(got) != 2 || got[0].Path != ".env" || got[1].Content != "B=2" {
		t.Fatalf("written into %s: %+v", ws.Path, got)
	}
	if len(wt.removed) != 0 {
		t.Fatalf("worktree removed: %v", wt.removed)
	}
}

func TestCreateRemovesTheWorktreeWhenItsEnvFilesCannotBeWritten(t *testing.T) {
	svc, repoStore, wt, repo := newService(t)
	io := &recordingIO{written: map[string][]*domain.EnvFile{}, err: errors.New("disk full")}
	svc.UseEnvFiles(memEnvFiles{repo.ID: {{RepositoryID: repo.ID, Path: ".env", Content: "A=1"}}}, io)

	_, err := svc.Create(repo.ID, "feature", "", "")
	if errs.Code(err) != "ENV_FILE_WRITE_FAILED" {
		t.Fatalf("got %v, want ENV_FILE_WRITE_FAILED", err)
	}
	if len(wt.added) != 1 || len(wt.removed) != 1 || wt.removed[0] != wt.added[0] {
		t.Fatalf("the half-made worktree must be removed: added %v removed %v", wt.added, wt.removed)
	}
	if len(repoStore.ListByRepository(repo.ID)) != 0 {
		t.Fatal("no workspace may be recorded")
	}
}

func TestCreateWithoutEnvFilesWritesNothing(t *testing.T) {
	svc, _, _, repo := newService(t)
	io := &recordingIO{written: map[string][]*domain.EnvFile{}}
	svc.UseEnvFiles(memEnvFiles{}, io)
	if _, err := svc.Create(repo.ID, "feature", "", ""); err != nil {
		t.Fatal(err)
	}
	if len(io.written) != 0 {
		t.Fatalf("wrote %v", io.written)
	}
}
