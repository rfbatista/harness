package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseArtifactKind(t *testing.T) {
	for _, ok := range []string{"", "page", "image", "video", "url", "file"} {
		if _, err := ParseArtifactKind(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	if _, err := ParseArtifactKind("slide"); err == nil || !strings.Contains(err.Error(), "INVALID_INPUT") {
		t.Fatalf("slide accepted or wrong code: %v", err)
	}
}

func TestArtifactMIMEAndKind(t *testing.T) {
	for _, tc := range []struct {
		name, head, mime string
		kind             ArtifactKind
	}{
		{"design/card.html", "", "text/html", ArtifactPage},
		{"card.HTM", "", "text/html", ArtifactPage},
		{"hero.png", "", "image/png", ArtifactImage},
		{"logo.svg", "", "image/svg+xml", ArtifactImage},
		{"clip.mp4", "", "video/mp4", ArtifactVideo},
		{"clip.mov", "", "video/quicktime", ArtifactVideo},
		{"clip.webm", "", "video/webm", ArtifactVideo},
		{"style.css", "", "text/css", ArtifactFile},
		{"notes.txt", "<html>", "text/plain", ArtifactFile},                          // extension wins over content
		{"noext", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR", "image/png", ArtifactImage}, // sniffed
		{"noext", "", "application/octet-stream", ArtifactFile},
	} {
		if got := ArtifactMIME(tc.name, []byte(tc.head)); got != tc.mime {
			t.Errorf("ArtifactMIME(%q) = %q, want %q", tc.name, got, tc.mime)
		}
		if got := InferArtifactKind(tc.mime); got != tc.kind {
			t.Errorf("InferArtifactKind(%q) = %q, want %q", tc.mime, got, tc.kind)
		}
	}
}

func TestArtifactKindMatches(t *testing.T) {
	if !ArtifactKindMatches("", ArtifactPage) || !ArtifactKindMatches(ArtifactFile, ArtifactPage) || !ArtifactKindMatches(ArtifactPage, ArtifactPage) {
		t.Fatal("omitted, file and equal kinds must match")
	}
	if ArtifactKindMatches(ArtifactImage, ArtifactPage) {
		t.Fatal("image must not match a page")
	}
}

func TestIsLoopbackURL(t *testing.T) {
	for _, ok := range []string{"http://localhost:3000/", "http://localhost", "http://127.0.0.1:5173/app", "https://127.0.0.1/", "http://[::1]:3000/"} {
		if !IsLoopbackURL(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"http://example.com", "http://10.0.0.1:3000", "http://localhost.evil.com", "file:///etc/passwd", "ftp://127.0.0.1/", "127.0.0.1:3000", ""} {
		if IsLoopbackURL(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestArtifactJSON_NullPathAndURL(t *testing.T) {
	b, _ := json.Marshal(Artifact{ID: "a1", Kind: ArtifactURL, URL: "http://localhost:3000"})
	s := string(b)
	if !strings.Contains(s, `"path":null`) || !strings.Contains(s, `"url":"http://localhost:3000"`) || !strings.Contains(s, `"size_bytes":0`) {
		t.Fatalf("url artifact json = %s", s)
	}
	b, _ = json.Marshal(Artifact{ID: "a2", Kind: ArtifactPage, Path: "design/card.html"})
	if s := string(b); !strings.Contains(s, `"url":null`) || !strings.Contains(s, `"path":"design/card.html"`) {
		t.Fatalf("page artifact json = %s", s)
	}
	var back Artifact
	if err := json.Unmarshal(b, &back); err != nil || back.Path != "design/card.html" || back.URL != "" || back.Kind != ArtifactPage {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
}

func TestArtifactScope_Valid(t *testing.T) {
	for _, s := range []ArtifactScope{ArtifactScopeTask, ArtifactScopeProject} {
		if !s.Valid() {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []ArtifactScope{"", "global", "Task"} {
		if s.Valid() {
			t.Errorf("%q should not be valid", s)
		}
	}
}

// The scope is always on the wire, "task" for an artifact that never had one;
// whether the harness holds a copy is not.
func TestArtifact_ScopeOnTheWire(t *testing.T) {
	b, err := json.Marshal(Artifact{ID: "a1", Kind: ArtifactPage, Path: "x.html", Snapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"scope":"task"`) {
		t.Fatalf("empty scope should read as task: %s", b)
	}
	if strings.Contains(strings.ToLower(string(b)), "snapshot") {
		t.Fatalf("snapshot leaked onto the wire: %s", b)
	}
	b, _ = json.Marshal(Artifact{ID: "a1", Scope: ArtifactScopeProject})
	var back Artifact
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Scope != ArtifactScopeProject {
		t.Fatalf("scope lost in the round trip: %q", back.Scope)
	}
	if err := json.Unmarshal([]byte(`{"id":"a2"}`), &back); err != nil || back.Scope != ArtifactScopeTask {
		t.Fatalf("missing scope should read as task: %q %v", back.Scope, err)
	}
}
