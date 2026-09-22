package configrepo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeManifest(t *testing.T, dir string, m Manifest) string {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	path := filepath.Join(dir, "agents.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func TestLoadManifest_ParsesAgentsAndBundles(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, Manifest{
		Bundles: map[string][]string{"developer": {"tdd", "git-commit"}},
		Agents: map[string]AgentConfig{
			"go-developer": {Skills: []string{"@developer"}, Description: "Go"},
		},
	})
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if !reflect.DeepEqual(m.Bundles["developer"], []string{"tdd", "git-commit"}) {
		t.Fatalf("bundles: %+v", m.Bundles)
	}
	if m.Agents["go-developer"].Description != "Go" {
		t.Fatalf("agent: %+v", m.Agents["go-developer"])
	}
}

func TestLoadManifest_MissingFileErrors(t *testing.T) {
	if _, err := LoadManifest(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected an error for a missing manifest")
	}
}

func TestResolveSkills_PlainListPassesThrough(t *testing.T) {
	out, err := ResolveSkills([]string{"a", "b"}, nil)
	if err != nil {
		t.Fatalf("ResolveSkills: %v", err)
	}
	if !reflect.DeepEqual(out, []string{"a", "b"}) {
		t.Fatalf("got %v", out)
	}
}

func TestResolveSkills_ExpandsBundleInPlace(t *testing.T) {
	bundles := map[string][]string{"developer": {"tdd", "git-commit"}}
	out, err := ResolveSkills([]string{"api-design", "@developer"}, bundles)
	if err != nil {
		t.Fatalf("ResolveSkills: %v", err)
	}
	if !reflect.DeepEqual(out, []string{"api-design", "tdd", "git-commit"}) {
		t.Fatalf("got %v", out)
	}
}

func TestResolveSkills_BundleMayReferenceABundle(t *testing.T) {
	bundles := map[string][]string{
		"base": {"tdd"},
		"full": {"@base", "git-commit"},
	}
	out, err := ResolveSkills([]string{"@full"}, bundles)
	if err != nil {
		t.Fatalf("ResolveSkills: %v", err)
	}
	if !reflect.DeepEqual(out, []string{"tdd", "git-commit"}) {
		t.Fatalf("got %v", out)
	}
}

func TestResolveSkills_DedupesKeepingFirstSeen(t *testing.T) {
	bundles := map[string][]string{"developer": {"tdd", "git-commit"}}
	out, err := ResolveSkills([]string{"tdd", "@developer"}, bundles)
	if err != nil {
		t.Fatalf("ResolveSkills: %v", err)
	}
	if !reflect.DeepEqual(out, []string{"tdd", "git-commit"}) {
		t.Fatalf("got %v", out)
	}
}

func TestResolveSkills_UnknownBundleErrors(t *testing.T) {
	_, err := ResolveSkills([]string{"@nope"}, map[string][]string{})
	if err == nil || !strings.Contains(err.Error(), "unknown bundle") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveSkills_SelfReferenceCycleErrors(t *testing.T) {
	bundles := map[string][]string{"a": {"@a"}}
	_, err := ResolveSkills([]string{"@a"}, bundles)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveSkills_MutualReferenceCycleErrors(t *testing.T) {
	bundles := map[string][]string{"a": {"@b"}, "b": {"@a"}}
	_, err := ResolveSkills([]string{"@a"}, bundles)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("got %v", err)
	}
}
