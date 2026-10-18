package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/adapter/out/releases"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/capabilities"
	"operators-mcp/internal/domain"
)

// TestTokenSaversInstallFromThePinnedReleases installs every token saver from
// the real pinned releases (github.com) for this platform, through the same
// install path the Token savers page uses, and runs each once. It downloads
// tens of megabytes, so it only runs with HARNESS_TOKEN_SAVERS_SMOKE=1.
func TestTokenSaversInstallFromThePinnedReleases(t *testing.T) {
	if os.Getenv("HARNESS_TOKEN_SAVERS_SMOKE") != "1" {
		t.Skip("set HARNESS_TOKEN_SAVERS_SMOKE=1 to install the pinned token savers from github.com")
	}
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close(db) })
	caps := catalog.New(catalog.SQLiteDeps(db)).Capabilities
	caps.UseTokenSavers(capabilities.TokenSaverDeps{
		Installations: sqlite.NewTokenSaverInstallationRepository(db),
		Downloads:     releases.NewClient(),
		Dir:           filepath.Join(dir, "tools"),
	})

	ctx := context.Background()
	for _, rel := range capabilities.PinnedTokenSaverReleases() {
		if _, err := caps.InstallTokenSaver(ctx, rel.Saver); err != nil {
			t.Fatalf("install %s: %v", rel.Saver, err)
		}
	}
	deadline := time.Now().Add(10 * time.Minute)
	for {
		list, err := caps.ListTokenSavers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		pending := false
		for _, st := range list.Savers {
			switch st.State {
			case domain.TokenSaverFailed:
				t.Fatalf("%s %s: %s: %s", st.Saver, st.PinnedVersion, st.Error.Code, st.Error.Message)
			case domain.TokenSaverInstalling:
				pending = true
			}
		}
		if !pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the installs did not end within 10 minutes")
		}
		time.Sleep(time.Second)
	}

	installed, err := caps.InstalledTokenSavers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[domain.TokenSaver]string{}
	for _, in := range installed {
		t.Logf("%s %s at %s", in.Saver, in.Version, in.Path)
		paths[in.Saver] = in.Path
	}
	for _, saver := range []domain.TokenSaver{domain.TokenSaverRTK, domain.TokenSaverCodegraph} {
		path, ok := paths[saver]
		if !ok {
			t.Fatalf("%s is not installed", saver)
		}
		cmd := exec.Command(path, "--version")
		cmd.Env = append(os.Environ(), "DO_NOT_TRACK=1", "RTK_TELEMETRY_DISABLED=1", "CODEGRAPH_TELEMETRY=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s --version: %v\n%s", path, err, out)
		}
		if strings.TrimSpace(string(out)) == "" {
			t.Fatalf("%s --version printed nothing", path)
		}
		t.Logf("%s --version: %s", saver, strings.TrimSpace(string(out)))
	}
	ruleset, err := os.ReadFile(paths[domain.TokenSaverCaveman])
	if err != nil {
		t.Fatalf("caveman's ruleset: %v", err)
	}
	if strings.TrimSpace(string(ruleset)) == "" {
		t.Fatalf("caveman's ruleset %s is empty", paths[domain.TokenSaverCaveman])
	}
}
