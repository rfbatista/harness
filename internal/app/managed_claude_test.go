//go:build !windows

package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"operators-mcp/internal/application/managedclaude"
	"operators-mcp/internal/ports"
)

// The real wiring resolves the claude agents run to the managed current path
// under the data dir, installs from Config.ManagedClaudeReleaseBase, and lets
// -claude.bin win.
func TestTheManagedClaudeIsWiredUnderTheDataDirAndTheOverrideWins_EndToEnd(t *testing.T) {
	platform := managedclaude.HostPlatform()
	build := "#!/bin/sh\necho 2.1.52\n"
	sum := sha256.Sum256([]byte(build))
	rel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.Trim(r.URL.Path, "/") {
		case "latest":
			fmt.Fprint(w, "2.1.52")
		case "2.1.52/manifest.json":
			fmt.Fprintf(w, `{"platforms":{%q:{"checksum":%q,"size":%d}}}`, platform, hex.EncodeToString(sum[:]), len(build))
		case "2.1.52/" + platform + "/claude":
			fmt.Fprint(w, build)
		default:
			http.NotFound(w, r)
		}
	}))
	defer rel.Close()

	boot := func(claudeBin string) (*managedclaude.Service, ports.ManagedClaudeBin, string) {
		dir := t.TempDir()
		var (
			svc *managedclaude.Service
			bin ports.ManagedClaudeBin
		)
		a := fx.New(
			fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "harness.db"), Root: dir, ClaudeBin: claudeBin, SessionShell: "direct", ManagedClaudeReleaseBase: rel.URL}),
			PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
			AgentRuntimeModule, TaskChannelModule, TextProcessingModule,
			fx.Populate(&svc, &bin),
			fx.NopLogger,
		)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := a.Start(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = a.Stop(context.Background()) })
		return svc, bin, dir
	}

	svc, bin, dir := boot("claude")
	want := filepath.Join(dir, "claude", "current")
	if bin.Overridden() || bin.Bin() != want || bin.Installed() {
		t.Fatalf("before an install: bin %q overridden %v installed %v, want %q", bin.Bin(), bin.Overridden(), bin.Installed(), want)
	}
	if _, err := svc.Install(context.Background(), "latest"); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); !bin.Installed() && time.Now().Before(deadline); {
		time.Sleep(10 * time.Millisecond)
	}
	if !bin.Installed() || bin.Bin() != want {
		t.Fatalf("after the install: bin %q installed %v", bin.Bin(), bin.Installed())
	}

	_, bin, _ = boot("/opt/dev/claude")
	if !bin.Overridden() || bin.Bin() != "/opt/dev/claude" {
		t.Fatalf("with -claude.bin: bin %q overridden %v", bin.Bin(), bin.Overridden())
	}
}
