package sandboxsettings

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/application/settings"
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

const home = "/home/ana"

func newStore(t *testing.T) (*Store, *settings.Service) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc := settings.NewService(sqlite.NewSettingsRepository(db), nil)
	return &Store{Settings: svc, Home: home}, svc
}

func TestTheSandboxIsOnUntilSwitchedOffAndOnAgain(t *testing.T) {
	s, svc := newStore(t)
	if !s.Enabled() {
		t.Fatal("a fresh store should have the sandbox on")
	}
	if err := s.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if s.Enabled() {
		t.Fatal("still on after SetEnabled(false)")
	}
	// The orchestration reads the same setting the store writes.
	if on := orchestration.SandboxSwitch(svc)(); on {
		t.Fatal("the orchestration switch did not follow the store")
	}
	if err := s.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !s.Enabled() || !orchestration.SandboxSwitch(svc)() {
		t.Fatal("not back on")
	}
}

func TestPathsAreAddedSwitchedBetweenReadOnlyAndReadWriteAndRemoved(t *testing.T) {
	s, _ := newStore(t)
	if got := s.Paths(); len(got) != 0 {
		t.Fatalf("fresh store has paths: %v", got)
	}
	if _, err := s.AddPath(ports.SandboxPath{Path: "/opt/cache"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPath(ports.SandboxPath{Path: "~/go/pkg/mod", Write: true}); err != nil {
		t.Fatal(err)
	}
	want := []ports.SandboxPath{{Path: "/opt/cache"}, {Path: "~/go/pkg/mod", Write: true}}
	if got := s.Paths(); !slices.Equal(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	if _, err := s.SetPathAccess("/opt/cache", true); err != nil {
		t.Fatal(err)
	}
	if got := s.Paths()[0]; !got.Write {
		t.Fatalf("access not switched: %v", got)
	}
	got, err := s.RemovePath("/opt/cache")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want[1:]) {
		t.Fatalf("after remove = %v", got)
	}
}

func TestExtraGivesThePolicyBuilderAbsolutePaths(t *testing.T) {
	s, _ := newStore(t)
	s.AddPath(ports.SandboxPath{Path: "~/go/pkg/mod"})
	s.AddPath(ports.SandboxPath{Path: "/opt/scratch/", Write: true})
	want := []ports.SandboxPath{{Path: home + "/go/pkg/mod"}, {Path: "/opt/scratch", Write: true}}
	var extra ports.SandboxExtraPaths = s.Extra
	if got := extra(); !slices.Equal(got, want) {
		t.Fatalf("extra = %v, want %v", got, want)
	}
}

func TestPathsTheStoreRefuses(t *testing.T) {
	s, _ := newStore(t)
	s.AddPath(ports.SandboxPath{Path: "/opt/cache"})
	const wide = "That would give agents your whole home directory. Add the specific folder instead."
	for name, tc := range map[string]struct{ path, message string }{
		"empty":     {"  ", "Enter a path."},
		"relative":  {"projects/x", "Use an absolute path or one starting with ~."},
		"root":      {"/", wide},
		"home":      {"~", wide},
		"home abs":  {home + "/", wide},
		"duplicate": {"/opt/cache/", "Already in the list."},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.AddPath(ports.SandboxPath{Path: tc.path})
			var se *domain.StructuredError
			if !errors.As(err, &se) || se.Code != "INVALID_INPUT" || se.Message != tc.message {
				t.Fatalf("err = %v, want INVALID_INPUT %q", err, tc.message)
			}
			if got := s.Paths(); len(got) != 1 {
				t.Fatalf("a refused path was stored: %v", got)
			}
		})
	}
}

func TestChangingOrRemovingAPathNotInTheListIsRefused(t *testing.T) {
	s, _ := newStore(t)
	_, errAccess := s.SetPathAccess("/nope", true)
	_, errRemove := s.RemovePath("/nope")
	for _, err := range []error{errAccess, errRemove} {
		var se *domain.StructuredError
		if !errors.As(err, &se) || se.Code != "SANDBOX_PATH_NOT_FOUND" {
			t.Fatalf("err = %v, want SANDBOX_PATH_NOT_FOUND", err)
		}
	}
}

func TestAPathThatDoesNotExistYetIsWarnedAboutAndStillSaved(t *testing.T) {
	s, _ := newStore(t)
	s.Home = t.TempDir()
	if _, err := s.AddPath(ports.SandboxPath{Path: "~/never-made"}); err != nil {
		t.Fatalf("a missing path is refused: %v", err)
	}
	if got := s.PathWarnings("~/never-made"); !slices.Equal(got, []string{WarnMissing}) {
		t.Fatalf("warnings = %v", got)
	}
	if got := s.PathWarnings(s.Home); len(got) != 0 {
		t.Fatalf("an existing folder is warned about: %v", got)
	}
}

func TestACredentialFolderIsWarnedAboutWhetherOrNotItExists(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &Store{Home: home}
	for path, want := range map[string][]string{
		"~/.ssh":                   {WarnCredentials},
		home + "/.ssh/keys":        {WarnMissing, WarnCredentials},
		"~/.aws":                   {WarnMissing, WarnCredentials},
		"~/.config/gcloud/configs": {WarnMissing, WarnCredentials},
		"~/.kube":                  {WarnMissing, WarnCredentials},
		"~/.sshfoo":                {WarnMissing},
		"~/.config":                {WarnMissing},
	} {
		if got := s.PathWarnings(path); !slices.Equal(got, want) {
			t.Errorf("%s: warnings = %v, want %v", path, got, want)
		}
	}
}
