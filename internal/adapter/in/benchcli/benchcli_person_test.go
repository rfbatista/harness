package benchcli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/adapter/out/personcred"
)

type acceptsOne string

func (a acceptsOne) Verify(_ context.Context, c string) bool { return c != "" && c == string(a) }

func withStore(t *testing.T, credential string) {
	t.Helper()
	old := personcred.Default
	t.Cleanup(func() { personcred.Default = old })
	file := filepath.Join(t.TempDir(), "person.cred")
	store := personcred.FileStore{PathFn: func() (string, error) { return file, nil }}
	if credential != "" {
		if err := store.Save(credential); err != nil {
			t.Fatal(err)
		}
	}
	personcred.Default = store
}

func TestTheCommandSendsTheStoredPersonCredentialSoARunWorksAgainstAServerWithPersonProofOn(t *testing.T) {
	srv, _ := testServerWith(t, &benchSessions{}, true, httpapi.WithPersonProof(acceptsOne("stored-cred"), nil))

	withStore(t, "stored-cred")
	code, out, errOut := run(t, "run", "-url", srv.URL, "-token", "secret", "-project", "p1", "-poll", "5ms")
	if code != 0 || !strings.Contains(out, "reads the readme") {
		t.Fatalf("with the stored credential: exit %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}
}

func TestTheCommandWithNoStoredCredentialOrAWrongOneIsRefusedByTheServerAndSaysSo(t *testing.T) {
	srv, _ := testServerWith(t, &benchSessions{}, true, httpapi.WithPersonProof(acceptsOne("stored-cred"), nil))
	for name, stored := range map[string]string{"nothing stored": "", "a wrong credential": "other"} {
		withStore(t, stored)
		code, _, errOut := run(t, "run", "-url", srv.URL, "-token", "secret", "-project", "p1", "-poll", "5ms")
		if code != 1 || !strings.Contains(errOut, "PERSON_ONLY") {
			t.Errorf("%s: exit %d stderr %q, want 1 with PERSON_ONLY", name, code, errOut)
		}
	}
}
