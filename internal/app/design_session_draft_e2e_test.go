//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"operators-mcp/internal/ports"
)

// A design session's shell holds the draft of its design system, project/
// design-system/, writable through the edit proxy (contract "Harness server ↔
// Agent sessions — design-system view, draft and proposals", "Read-only,
// except in a design session's draft").

// designSession starts a design session bound to the design system and finds
// its shell.
func (e *shellE2E) designSession(designSystemID string) runningSession {
	e.t.Helper()
	return e.startWith(ports.InteractiveRequest{ProjectID: e.project, RepositoryID: e.repo, Mode: "designsystem", DesignSystemID: designSystemID})
}

// draftWrite is what the edit proxy does for a write: check, the file is
// written, commit. It answers the commit's answer, or the check's when that
// refused.
func (e *shellE2E) draftWrite(s runningSession, rel, content string) shellAnswer {
	e.t.Helper()
	full := designSystemDir + "/" + rel
	if a, _ := e.shellCall(s, "check", "write", full); !a.Allow {
		return a
	}
	writeShellFile(e.t, s, full, content)
	a, _ := e.shellCall(s, "commit", "write", full)
	return a
}

func (e *shellE2E) draftRemove(s runningSession, rel string) shellAnswer {
	e.t.Helper()
	full := designSystemDir + "/" + rel
	if a, _ := e.shellCall(s, "check", "rm", full); !a.Allow {
		return a
	}
	removeShellFile(e.t, s, full)
	a, _ := e.shellCall(s, "commit", "rm", full)
	return a
}

// savedDesignSystem is a design system at version 1 with the shell's tokens.
func (e *shellE2E) savedDesignSystem() string {
	e.t.Helper()
	id := e.newDesignSystem("Brand", "brand", "The brand voice, in colour.")
	e.saveTo(id, 0, dsRecord(buttonComponent()))
	return id
}

func wantRefused(t *testing.T, a shellAnswer, code string, fragments ...string) {
	t.Helper()
	if a.Allow || a.Code != code {
		t.Fatalf("answer = %+v, want refused %s", a, code)
	}
	for _, f := range fragments {
		if !strings.Contains(a.Reason, f) {
			t.Errorf("reason %q lacks %q", a.Reason, f)
		}
	}
}

func TestDesignDraft_IsTheBoundDesignSystemWithEveryGeneratedFileWritableButReadme(t *testing.T) {
	e := newShellE2E(t)
	id := e.savedDesignSystem()
	other := e.newDesignSystem("Other", "other", "")
	e.saveTo(other, 0, dsRecord())
	s := e.designSession(id)

	got := strings.Join(e.listing(s, designSystemDir), ",")
	if got != "DESIGN.md,README.md,components,tokens.json" {
		t.Fatalf("the draft holds %s", got)
	}
	if got := strings.Join(e.listing(s, designSystemDir+"/components"), ","); got != "button.html" {
		t.Fatalf("components = %s", got)
	}
	idx, err := shellIndex(s.shell)
	if err != nil {
		t.Fatal(err)
	}
	for p, row := range idx {
		if !strings.HasPrefix(p, designSystemDir+"/") {
			continue
		}
		if row.Kind != "design-system" || row.Scope != "project" || row.Writable != (p != designSystemDir+"/README.md") {
			t.Errorf("%s indexed as %+v", p, row)
		}
	}
	for _, rel := range []string{"README.md", "tokens.json"} {
		info, err := os.Stat(filepath.Join(s.shell, designSystemDir, rel))
		if err != nil {
			t.Fatal(err)
		}
		if writable := info.Mode().Perm()&0o200 != 0; writable != (rel != "README.md") {
			t.Errorf("%s mode %v", rel, info.Mode())
		}
	}
	// Only the bound design system: the other one is nowhere in the shell.
	if b, _ := os.ReadFile(filepath.Join(s.shell, designSystemDir, "README.md")); !strings.Contains(string(b), "Brand") || strings.Contains(string(b), "Other") {
		t.Errorf("README.md of the draft:\n%s", b)
	}
}

func TestDesignDraft_AtVersionZeroIsTheReadmeAloneAndTheFirstWriteMakesTheFiles(t *testing.T) {
	e := newShellE2E(t)
	id := e.newDesignSystem("Fresh", "fresh", "")
	s := e.designSession(id)
	if got := strings.Join(e.listing(s, designSystemDir), ","); got != "README.md" {
		t.Fatalf("the draft of a design system with no version holds %s", got)
	}
	tokens := `{"colors":{"signal":{"$type":"color","$value":"#336699"}}}`
	if a := e.draftWrite(s, "tokens.json", tokens); !a.Allow {
		t.Fatalf("write tokens.json: %+v", a)
	}
	if got := e.designFile(s, "tokens.json"); !strings.Contains(got, `"signal"`) {
		t.Fatalf("tokens.json reads back %s", got)
	}
	if got := strings.Join(e.listing(s, designSystemDir), ","); !strings.Contains(got, "DESIGN.md") {
		t.Fatalf("the draft holds %s", got)
	}
	// The stored design system is untouched: a draft is not a save.
	var got struct {
		Version int  `json:"version"`
		Exists  bool `json:"exists"`
	}
	e.post("get_design_system", map[string]any{"design_system_id": id}, &got)
	if got.Exists || got.Version != 0 {
		t.Fatalf("the design system changed: %+v", got)
	}
}

func TestDesignDraft_AcceptedWritesReadBackRegeneratedFiles(t *testing.T) {
	e := newShellE2E(t)
	s := e.designSession(e.savedDesignSystem())

	tokens := e.designFile(s, "tokens.json")
	withInk := strings.Replace(tokens, `"signal"`, `"ink": {"$type": "color", "$value": "#111111"}, "signal"`, 1)
	if withInk == tokens {
		t.Fatalf("tokens.json has no signal token:\n%s", tokens)
	}
	if a := e.draftWrite(s, "tokens.json", withInk); !a.Allow {
		t.Fatalf("write tokens.json: %+v", a)
	}
	if got := e.designFile(s, "tokens.json"); !strings.Contains(got, `"ink"`) {
		t.Errorf("tokens.json does not read back the new token:\n%s", got)
	}
	if got := e.designFile(s, "DESIGN.md"); !strings.Contains(got, "ink") {
		t.Errorf("DESIGN.md's frontmatter does not follow the tokens:\n%s", got)
	}

	// The body of DESIGN.md is the principles.
	design := e.designFile(s, "DESIGN.md")
	if a := e.draftWrite(s, "DESIGN.md", strings.Replace(design, "Quiet surfaces.", "Quiet surfaces, always.", 1)); !a.Allow {
		t.Fatalf("write DESIGN.md: %+v", a)
	}
	if got := e.designFile(s, "DESIGN.md"); !strings.Contains(got, "Quiet surfaces, always.") {
		t.Errorf("DESIGN.md does not read back the principles:\n%s", got)
	}

	// A component is a file: create it from the existing page, then remove one.
	page := e.designFile(s, "components/button.html")
	chip := strings.NewReplacer("button", "chip", "Button", "Chip").Replace(page)
	if a := e.draftWrite(s, "components/chip.html", chip); !a.Allow {
		t.Fatalf("create components/chip.html: %+v", a)
	}
	if got := strings.Join(e.listing(s, designSystemDir+"/components"), ","); got != "button.html,chip.html" {
		t.Fatalf("components = %s", got)
	}
	if a := e.draftRemove(s, "components/chip.html"); !a.Allow {
		t.Fatalf("rm components/chip.html: %+v", a)
	}
	if got := strings.Join(e.listing(s, designSystemDir+"/components"), ","); got != "button.html" {
		t.Fatalf("components = %s", got)
	}
}

func TestDesignDraft_RefusedWritesSayWhyAndLeaveTheFileAsItWas(t *testing.T) {
	e := newShellE2E(t)
	s := e.designSession(e.savedDesignSystem())
	tokens := e.designFile(s, "tokens.json")
	design := e.designFile(s, "DESIGN.md")
	page := e.designFile(s, "components/button.html")

	// DESIGN.md's frontmatter is generated from the tokens.
	front := strings.Replace(design, "oklch(0.5 0.095 182)", "red", 1)
	if front == design {
		t.Fatalf("DESIGN.md has no signal value:\n%s", design)
	}
	wantRefused(t, e.draftWrite(s, "DESIGN.md", front), "DESIGN_SYSTEM_INVALID", "edit tokens in tokens.json", "frontmatter is generated")
	if got := e.designFile(s, "DESIGN.md"); got != design {
		t.Errorf("a refused write changed DESIGN.md:\n%s", got)
	}

	// README.md is generated.
	wantRefused(t, e.draftWrite(s, "README.md", "mine"), "READ_ONLY", "README.md")

	// A script in a preview is refused, with the list of errors.
	script := strings.Replace(page, "<button", "<script>alert(1)</script><button", 1)
	wantRefused(t, e.draftWrite(s, "components/button.html", script), "DESIGN_SYSTEM_INVALID", "UNSAFE_PREVIEW")
	if got := e.designFile(s, "components/button.html"); got != page {
		t.Errorf("a refused write changed the component page")
	}

	// A preview of an unknown token.
	unknown := strings.Replace(page, "var(--colors-signal)", "var(--colors-nope)", 1)
	wantRefused(t, e.draftWrite(s, "components/button.html", unknown), "DESIGN_SYSTEM_INVALID", "UNKNOWN_TOKEN")

	// Removing a token a draft component uses.
	noSignal := strings.Replace(tokens, `"signal"`, `"dropped"`, 1)
	wantRefused(t, e.draftWrite(s, "tokens.json", noSignal), "DESIGN_SYSTEM_INVALID", "TOKEN_IN_USE", "button")
	if got := e.designFile(s, "tokens.json"); got != tokens {
		t.Errorf("a refused write changed tokens.json")
	}

	// A component with no preview, and a nameless one.
	bare := page
	for strings.Contains(bare, "<section") {
		start := strings.Index(bare, "<section")
		end := strings.Index(bare[start:], "</section>")
		bare = bare[:start] + bare[start+end+len("</section>"):]
	}
	wantRefused(t, e.draftWrite(s, "components/button.html", bare), "DESIGN_SYSTEM_INVALID", "BAD_VALUE")

	// A file name that is not the component's slug.
	wantRefused(t, e.draftWrite(s, "components/other.html", page), "DESIGN_SYSTEM_INVALID", "other.html")
	if _, err := os.Stat(filepath.Join(s.shell, designSystemDir, "components", "other.html")); err == nil {
		t.Errorf("a refused new component file is still there")
	}
	// A path that is not part of a design system.
	wantRefused(t, e.draftWrite(s, "notes.txt", "x"), "READ_ONLY", "tokens.json")
}

func TestDesignDraft_EachDesignSessionHasItsOwnAndTheStoredDesignSystemNeverChanges(t *testing.T) {
	e := newShellE2E(t)
	id := e.savedDesignSystem()
	one, two := e.designSession(id), e.designSession(id)

	tokens := e.designFile(one, "tokens.json")
	withInk := strings.Replace(tokens, `"signal"`, `"ink": {"$type": "color", "$value": "#111111"}, "signal"`, 1)
	if a := e.draftWrite(one, "tokens.json", withInk); !a.Allow {
		t.Fatalf("write: %+v", a)
	}
	if got := e.designFile(two, "tokens.json"); strings.Contains(got, `"ink"`) {
		t.Errorf("a draft shows another session's change:\n%s", got)
	}
	var got struct {
		Version int `json:"version"`
		Record  struct {
			Tokens []any `json:"tokens"`
		} `json:"record"`
	}
	e.post("get_design_system", map[string]any{"design_system_id": id}, &got)
	if got.Version != 1 || len(got.Record.Tokens) != 4 {
		t.Errorf("the design system changed: %+v", got)
	}
}

func TestDesignDraft_OtherSessionsStillSeeTheFolderReadOnlyOrNotAtAll(t *testing.T) {
	e := newShellE2E(t)
	id := e.savedDesignSystem()
	e.applyDesignSystem(e.repo, id)
	tk := e.task("Build the page")
	s := e.start(tk)
	if a, _ := e.shellCall(s, "check", "write", designSystemDir+"/tokens.json"); a.Allow || a.Code != "READ_ONLY" {
		t.Fatalf("a task session's write = %+v, want READ_ONLY", a)
	}
	idx, err := shellIndex(s.shell)
	if err != nil {
		t.Fatal(err)
	}
	for p, row := range idx {
		if strings.HasPrefix(p, designSystemDir+"/") && row.Writable {
			t.Errorf("%s is writable in a task session", p)
		}
	}
}
