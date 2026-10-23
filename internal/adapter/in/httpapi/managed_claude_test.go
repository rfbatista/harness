package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/application/managedclaude"
	"operators-mcp/internal/ports"
)

// memoryReleases serves a release layout from memory.
type memoryReleases map[string]string

func (m memoryReleases) Download(_ context.Context, url string, w io.Writer, _ func(done, total int64)) error {
	body, ok := m[strings.TrimPrefix(url, "http://releases/")]
	if !ok {
		return ports.ErrReleaseFileNotFound
	}
	_, err := io.WriteString(w, body)
	return err
}

func managedClaudeHandler(t *testing.T, override string) *Handler {
	t.Helper()
	bin := "#!/bin/sh\necho 2.1.52\n"
	sum := sha256.Sum256([]byte(bin))
	svc := managedclaude.New(managedclaude.Deps{
		Downloads: memoryReleases{
			"latest":                  "2.1.52",
			"2.1.52/manifest.json":    fmt.Sprintf(`{"platforms":{"linux-x64":{"checksum":%q,"size":%d}}}`, hex.EncodeToString(sum[:]), len(bin)),
			"2.1.52/linux-x64/claude": bin,
		},
		Dir: filepath.Join(t.TempDir(), "claude"), BaseURL: "http://releases", Platform: "linux-x64",
	})
	t.Cleanup(svc.Close)
	return NewHandler(Services{ManagedClaude: svc, ManagedClaudeBin: managedclaude.Resolver{Override: override, Managed: svc}})
}

type managedClaudeBody struct {
	Current    string `json:"current"`
	Overridden bool   `json:"overridden"`
	Bin        string `json:"bin"`
	Installed  []struct {
		Version string `json:"version"`
		Active  bool   `json:"active"`
	} `json:"installed"`
	Installing *struct{ Version string } `json:"installing"`
	Latest     string                    `json:"latest"`
	Code       string                    `json:"code"`
}

func TestManagedClaudeInstallsOverHTTPAndAnswersItsState(t *testing.T) {
	h := managedClaudeHandler(t, "")
	var b managedClaudeBody
	if err := json.Unmarshal(getJSON(t, h, "/get_managed_claude").Body.Bytes(), &b); err != nil || b.Current != "" || len(b.Installed) != 0 {
		t.Fatalf("initial = %+v, %v", b, err)
	}
	json.Unmarshal(getJSON(t, h, "/list_managed_claude_releases").Body.Bytes(), &b)
	if b.Latest != "2.1.52" {
		t.Fatalf("releases = %+v", b)
	}

	if rec := postJSON(t, h, "/install_managed_claude", map[string]any{"version": "latest"}); rec.Code != http.StatusOK {
		t.Fatalf("install = %d %s", rec.Code, rec.Body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b = managedClaudeBody{}
		json.Unmarshal(getJSON(t, h, "/get_managed_claude").Body.Bytes(), &b)
		if b.Installing == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if b.Current != "2.1.52" || len(b.Installed) != 1 || !b.Installed[0].Active || !strings.HasSuffix(b.Bin, "current") {
		t.Fatalf("after install = %+v", b)
	}

	rec := postJSON(t, h, "/install_managed_claude", map[string]any{"version": "2.1.52"})
	json.Unmarshal(rec.Body.Bytes(), &b)
	if rec.Code != http.StatusConflict || b.Code != "MANAGED_CLAUDE_VERSION_INSTALLED" {
		t.Fatalf("reinstall = %d %s", rec.Code, rec.Body)
	}
	if rec := postJSON(t, h, "/switch_managed_claude", map[string]any{"version": "9.9.9"}); rec.Code != http.StatusNotFound {
		t.Fatalf("switch to a missing version = %d", rec.Code)
	}
	if rec := postJSON(t, h, "/switch_managed_claude", map[string]any{"version": "2.1.52"}); rec.Code != http.StatusOK {
		t.Fatalf("switch = %d", rec.Code)
	}
}

func TestManagedClaudeStateSaysWhenTheOverrideDecides(t *testing.T) {
	h := managedClaudeHandler(t, "/opt/dev/claude")
	var b managedClaudeBody
	json.Unmarshal(getJSON(t, h, "/get_managed_claude").Body.Bytes(), &b)
	if !b.Overridden || b.Bin != "/opt/dev/claude" {
		t.Fatalf("state = %+v", b)
	}
}

func TestManagedClaudeEndpointsAnswer503WhenNotConfigured(t *testing.T) {
	h := NewHandler(Services{})
	if rec := getJSON(t, h, "/get_managed_claude"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestManagedClaudeInstallCanBeCancelledAndANonCurrentVersionRemovedOverHTTP(t *testing.T) {
	h := managedClaudeHandler(t, "")
	postJSON(t, h, "/install_managed_claude", map[string]any{"version": "latest"})
	var b managedClaudeBody
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		b = managedClaudeBody{}
		json.Unmarshal(getJSON(t, h, "/get_managed_claude").Body.Bytes(), &b)
		if b.Installing == nil {
			break
		}
	}
	if rec := postJSON(t, h, "/cancel_managed_claude_install", map[string]any{}); rec.Code != http.StatusOK {
		t.Fatalf("cancel with nothing running = %d %s", rec.Code, rec.Body)
	}
	rec := postJSON(t, h, "/remove_managed_claude_version", map[string]any{"version": "2.1.52"})
	json.Unmarshal(rec.Body.Bytes(), &b)
	if rec.Code != http.StatusConflict || b.Code != "MANAGED_CLAUDE_VERSION_CURRENT" {
		t.Fatalf("removing the current version = %d %s", rec.Code, rec.Body)
	}
	if rec := postJSON(t, h, "/remove_managed_claude_version", map[string]any{"version": "9.9.9"}); rec.Code != http.StatusNotFound {
		t.Fatalf("removing a missing version = %d", rec.Code)
	}
}
