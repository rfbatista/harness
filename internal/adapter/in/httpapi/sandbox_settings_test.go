package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/sandboxsettings"
	"operators-mcp/internal/application/settings"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/portstest"
)

type sandboxBody struct {
	Enabled bool                `json:"enabled"`
	Driver  string              `json:"driver"`
	Paths   []ports.SandboxPath `json:"paths"`
	Code    string              `json:"code"`
	Message string              `json:"error"`
}

func sandboxHandler(t *testing.T, driver ports.SandboxDriver) (*Handler, *settings.Service) {
	t.Helper()
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc := settings.NewService(sqlite.NewSettingsRepository(db), nil)
	store := &sandboxsettings.Store{Settings: svc, Home: "/home/ana"}
	return NewHandler(Services{SandboxSettings: store, SandboxDriver: func() ports.SandboxDriver { return driver }}), svc
}

func decodeSandbox(t *testing.T, rec *httptest.ResponseRecorder, status int) sandboxBody {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	var b sandboxBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSandboxSettingsRoundTripThroughTheSettingsStore(t *testing.T) {
	h, svc := sandboxHandler(t, &portstest.RecordingSandboxDriver{})

	b := decodeSandbox(t, getJSON(t, h, "/get_sandbox_settings"), http.StatusOK)
	if !b.Enabled || b.Driver != "recording" || len(b.Paths) != 0 {
		t.Fatalf("initial = %+v, want on, driver recording, no paths", b)
	}

	b = decodeSandbox(t, postJSON(t, h, "/set_sandbox_enabled", map[string]any{"enabled": false}), http.StatusOK)
	if b.Enabled {
		t.Fatalf("still on after switching off: %+v", b)
	}
	if got := svc.Setting("agent_sandbox"); got != "off" {
		t.Fatalf("setting agent_sandbox = %q, want off", got)
	}

	decodeSandbox(t, postJSON(t, h, "/add_sandbox_path", map[string]any{"path": "~/go/pkg/mod"}), http.StatusOK)
	b = decodeSandbox(t, postJSON(t, h, "/add_sandbox_path", map[string]any{"path": "/opt/scratch", "write": true}), http.StatusOK)
	want := []ports.SandboxPath{{Path: "~/go/pkg/mod"}, {Path: "/opt/scratch", Write: true}}
	if len(b.Paths) != 2 || b.Paths[0] != want[0] || b.Paths[1] != want[1] {
		t.Fatalf("paths = %+v, want %+v", b.Paths, want)
	}

	b = decodeSandbox(t, postJSON(t, h, "/update_sandbox_path", map[string]any{"path": "~/go/pkg/mod", "write": true}), http.StatusOK)
	if !b.Paths[0].Write {
		t.Fatalf("access not switched: %+v", b.Paths)
	}
	b = decodeSandbox(t, postJSON(t, h, "/remove_sandbox_path", map[string]any{"path": "/opt/scratch"}), http.StatusOK)
	if len(b.Paths) != 1 || b.Paths[0].Path != "~/go/pkg/mod" {
		t.Fatalf("after remove: %+v", b.Paths)
	}

	// A fresh read shows the same: the state is in the settings, not the handler.
	b = decodeSandbox(t, getJSON(t, h, "/get_sandbox_settings"), http.StatusOK)
	if b.Enabled || len(b.Paths) != 1 {
		t.Fatalf("reread = %+v", b)
	}
}

func TestSandboxSettingsRefuseBadPathsWithACodedMessage(t *testing.T) {
	h, _ := sandboxHandler(t, nil)
	for path, message := range map[string]string{
		"":           "Enter a path.",
		"projects/x": "Use an absolute path or one starting with ~.",
	} {
		b := decodeSandbox(t, postJSON(t, h, "/add_sandbox_path", map[string]any{"path": path}), http.StatusBadRequest)
		if b.Code != "INVALID_INPUT" || b.Message != message {
			t.Errorf("%q: %+v, want INVALID_INPUT %q", path, b, message)
		}
	}
	b := decodeSandbox(t, postJSON(t, h, "/remove_sandbox_path", map[string]any{"path": "/nope"}), http.StatusNotFound)
	if b.Code != "SANDBOX_PATH_NOT_FOUND" {
		t.Errorf("code = %q", b.Code)
	}
}

func TestSandboxSettingsSayWhenThePlatformHasNoDriver(t *testing.T) {
	h, _ := sandboxHandler(t, nil)
	b := decodeSandbox(t, getJSON(t, h, "/get_sandbox_settings"), http.StatusOK)
	if b.Driver != "" {
		t.Fatalf("driver = %q, want none", b.Driver)
	}
}

func TestSandboxSettingsAreUnavailableWithoutAStore(t *testing.T) {
	rec := getJSON(t, NewHandler(Services{}), "/get_sandbox_settings")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSandboxPathsCarryTheirWarningsAndAreStillSaved(t *testing.T) {
	h, _ := sandboxHandler(t, nil)
	rec := postJSON(t, h, "/add_sandbox_path", map[string]any{"path": "~/.ssh-never-made/keys"})
	if rec.Code != http.StatusOK {
		t.Fatalf("add = %d %s", rec.Code, rec.Body)
	}
	var b struct {
		Paths []struct {
			Path     string   `json:"path"`
			Warnings []string `json:"warnings"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil || len(b.Paths) != 1 {
		t.Fatalf("body = %s, %v", rec.Body, err)
	}
	if got := b.Paths[0].Warnings; len(got) != 1 || got[0] != "missing" {
		t.Fatalf("warnings = %v", got)
	}
}
