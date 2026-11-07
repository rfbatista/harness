package services_test

import (
	"testing"

	"operators-mcp/internal/adapter/out/persistence/sqlite"
	"operators-mcp/internal/application/services"
	"operators-mcp/internal/application/settings"
)

func TestTheRuntimeSettingIsStoredAndDefaultsToDocker(t *testing.T) {
	db, err := sqlite.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc := settings.NewService(sqlite.NewSettingsRepository(db), nil)
	rt := services.RuntimeSetting{Settings: svc}
	if rt.ContainerRuntime() != "docker" {
		t.Fatal("docker is the default")
	}
	if err := rt.SetContainerRuntime("podman"); err != nil {
		t.Fatal(err)
	}
	if got := (services.RuntimeSetting{Settings: svc}).ContainerRuntime(); got != "podman" {
		t.Fatalf("stored = %q", got)
	}
	if rt.SetContainerRuntime("rkt") == nil || rt.ContainerRuntime() != "podman" {
		t.Fatal("another runtime is refused and changes nothing")
	}
}
