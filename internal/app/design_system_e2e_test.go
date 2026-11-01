//go:build !windows

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.uber.org/fx"

	"operators-mcp/internal/adapter/in/httpapi"
	"operators-mcp/internal/app/catalog"
	"operators-mcp/internal/application/orchestration"
	"operators-mcp/internal/ports"
)

// designServer is the server as it runs, with the design system API on a test
// server and one project.
type designServer struct {
	*archE2E
	cat   catalog.Catalog
	orch  *orchestration.Service
	store ports.DesignSystemStore
}

func startDesignServer(t *testing.T) *designServer {
	t.Helper()
	dir := t.TempDir()
	s := &designServer{}
	app := fx.New(
		fx.Supply(Config{HTTPAddr: "127.0.0.1:0", MCPAddr: "127.0.0.1:0", DBPath: filepath.Join(dir, "harness.db"), Root: dir, ClaudeBin: "/bin/true", SessionShell: "direct"}),
		PersistenceModule, CatalogModule, PlanningModule, WorkspacesModule,
		AgentRuntimeModule, TaskChannelModule, TextProcessingModule, NotificationsModule,
		fx.Populate(&s.cat, &s.orch, &s.store),
		fx.NopLogger,
	)
	if err := app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Stop(context.Background()) })
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.NewHandler(httpapi.Services{
		Projects: s.cat.Projects, Sessions: s.orch, DesignSystems: s.cat.DesignSystems,
	})))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})
	p, err := s.cat.Projects.CreateProject(context.Background(), "widgets", dir)
	if err != nil {
		t.Fatal(err)
	}
	s.archE2E = &archE2E{t: t, ctx: context.Background(), srv: srv, project: p.ID}
	s.follow()
	return s
}

// post calls POST /api/<verb>, answers the status and the JSON body.
func (s *designServer) post(verb string, body map[string]any) (int, map[string]any) {
	s.t.Helper()
	if _, ok := body["project_id"]; !ok {
		body["project_id"] = s.project
	}
	raw, _ := json.Marshal(body)
	res, err := http.Post(s.srv.URL+"/api/"+verb, "application/json", bytes.NewReader(raw))
	if err != nil {
		s.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	b, _ := io.ReadAll(res.Body)
	_ = json.Unmarshal(b, &out)
	return res.StatusCode, out
}

func (s *designServer) mustPost(verb string, body map[string]any) map[string]any {
	s.t.Helper()
	status, out := s.post(verb, body)
	if status != http.StatusOK {
		s.t.Fatalf("%s: %d %v", verb, status, out)
	}
	return out
}

func (s *designServer) get() map[string]any { return s.mustPost("get_design_system", map[string]any{}) }

func (s *designServer) save(base int, record map[string]any) (int, map[string]any) {
	return s.post("save_design_system", map[string]any{"base_version": base, "record": record})
}

func (s *designServer) mustSave(base int, record map[string]any) int {
	s.t.Helper()
	status, out := s.save(base, record)
	if status != http.StatusOK {
		s.t.Fatalf("save: %d %v", status, out)
	}
	return int(out["version"].(float64))
}

func (s *designServer) rename(base int, group, name, newGroup, newName string) (int, map[string]any) {
	return s.post("rename_design_token", map[string]any{"base_version": base, "group": group, "name": name, "new_group": newGroup, "new_name": newName})
}

func (s *designServer) versionRecord(version int) map[string]any {
	s.t.Helper()
	out := s.mustPost("get_design_system_version", map[string]any{"version": version})
	return out["record"].(map[string]any)
}

func dsToken(group, name, typ string, value any) map[string]any {
	return map[string]any{"group": group, "name": name, "type": typ, "value": value, "note": ""}
}

func dsPreview(html, css string) map[string]any {
	return map[string]any{"label": "Primary / default", "variant": "primary", "state": "default", "html": html, "css": css}
}

func dsComponent(slug string, previews ...map[string]any) map[string]any {
	ps := make([]any, len(previews))
	for i, p := range previews {
		ps[i] = p
	}
	return map[string]any{"slug": slug, "name": strings.ToUpper(slug[:1]) + slug[1:], "purpose": "Starts an action.", "do": []any{"Use one per view."}, "dont": []any{"Do not nest."}, "previews": ps}
}

// dsRecord is a valid record: scalar and composite tokens, and a component
// whose previews use both.
func dsRecord(components ...map[string]any) map[string]any {
	cs := make([]any, len(components))
	for i, c := range components {
		cs[i] = c
	}
	return map[string]any{
		"tokens": []any{
			dsToken("colors", "signal", "color", "oklch(0.5 0.095 182)"),
			dsToken("spacing", "md", "dimension", "1rem"),
			dsToken("typography", "headline", "typography", map[string]any{"fontFamily": "Inter, sans-serif", "fontSize": "1.5rem", "fontWeight": "600", "lineHeight": "1.2"}),
			dsToken("shadow", "card", "shadow", map[string]any{"offsetY": "2px", "blur": "8px", "color": "rgba(0,0,0,.2)"}),
		},
		"principles": "# Principles\n\n## Calm\n\nQuiet surfaces.\n",
		"components": cs,
	}
}

func buttonComponent() map[string]any {
	return dsComponent("button",
		dsPreview(`<button class="btn">Save</button>`, `.btn{background:var(--colors-signal);padding:var(--spacing-md);box-shadow:0 var(--shadow-card-offset-y) var(--shadow-card-blur) var(--shadow-card-color);font-size:var(--typography-headline-font-size);font-family:var( --typography-headline-font-family )}`),
		dsPreview(`<b class="logo">x</b>`, `.logo{background:url(data:image/png;base64,AAAA) var(--colors-signal)}`),
	)
}

// problems are the errors of a refused response as "code path" pairs.
func problems(t *testing.T, body map[string]any) []string {
	t.Helper()
	details, _ := body["details"].(map[string]any)
	list, _ := details["errors"].([]any)
	var out []string
	for _, e := range list {
		m := e.(map[string]any)
		if m["message"] == "" || m["message"] == nil {
			t.Errorf("error %v has no message", m)
		}
		out = append(out, m["code"].(string)+" "+m["path"].(string))
	}
	return out
}

func TestDesignSystem_AnEmptyOneReadsAsNotExistingAndStoresNothing(t *testing.T) {
	s := startDesignServer(t)
	got := s.get()
	if got["exists"] != false || got["version"] != float64(0) || got["project_id"] != s.project {
		t.Fatalf("%v", got)
	}
	rec := got["record"].(map[string]any)
	if tokens, ok := rec["tokens"].([]any); !ok || len(tokens) != 0 {
		t.Errorf("tokens = %v, want []", rec["tokens"])
	}
	if cs, ok := rec["components"].([]any); !ok || len(cs) != 0 {
		t.Errorf("components = %v, want []", rec["components"])
	}
	if rec["principles"] != "" {
		t.Errorf("principles = %v, want an empty string", rec["principles"])
	}
	if got["open_proposals"] != float64(0) || got["running_design_sessions"] != float64(0) {
		t.Errorf("%v", got)
	}
	if v := s.mustPost("list_design_system_versions", map[string]any{})["versions"]; len(v.([]any)) != 0 {
		t.Errorf("versions = %v", v)
	}
	if stored, err := s.store.Latest(context.Background(), s.project); err != nil || stored != nil {
		t.Errorf("reading stored %v, %v", stored, err)
	}
}

func TestDesignSystem_TheFirstSaveWritesVersionOneAndEachNextOneFollows(t *testing.T) {
	s := startDesignServer(t)
	if v := s.mustSave(0, dsRecord()); v != 1 {
		t.Fatalf("first save = %d", v)
	}
	status, out := s.post("save_design_system", map[string]any{"base_version": 1, "record": dsRecord(buttonComponent()), "message": "Add the button"})
	if status != http.StatusOK || out["version"] != float64(2) {
		t.Fatalf("%d %v", status, out)
	}
	got := s.get()
	if got["exists"] != true || got["version"] != float64(2) || got["updated_by"] != "person" || got["updated_at"] == nil {
		t.Fatalf("%v", got)
	}
	rec := got["record"].(map[string]any)
	if len(rec["tokens"].([]any)) != 4 || len(rec["components"].([]any)) != 1 {
		t.Errorf("record = %v", rec)
	}
	tok := rec["tokens"].([]any)[2].(map[string]any)
	if tok["type"] != "typography" || tok["value"].(map[string]any)["fontSize"] != "1.5rem" {
		t.Errorf("composite token = %v", tok)
	}
}

func TestDesignSystem_ASaveOnAStaleBaseIsRefusedWithTheCurrentVersion(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord())
	s.mustSave(1, dsRecord())

	for _, base := range []int{0, 1, 5} {
		status, out := s.save(base, dsRecord(buttonComponent()))
		if status != http.StatusConflict || out["code"] != "DESIGN_SYSTEM_CHANGED" {
			t.Fatalf("base %d: %d %v", base, status, out)
		}
		if cur := out["details"].(map[string]any)["current_version"]; cur != float64(2) {
			t.Errorf("current_version = %v", cur)
		}
	}
	if got := s.get(); got["version"] != float64(2) {
		t.Errorf("a refused save moved the version: %v", got["version"])
	}
	if status, out := s.post("save_design_system", map[string]any{"record": dsRecord()}); status != http.StatusBadRequest {
		t.Errorf("a save without base_version: %d %v", status, out)
	}
}

func TestDesignSystem_AnInvalidRecordIsRefusedWithEachErrorAndStoresNothing(t *testing.T) {
	s := startDesignServer(t)
	with := func(mutate func(r map[string]any)) map[string]any {
		r := dsRecord(buttonComponent())
		mutate(r)
		return r
	}
	tokens := func(r map[string]any) []any { return r["tokens"].([]any) }
	preview := func(r map[string]any, i int) map[string]any {
		return r["components"].([]any)[0].(map[string]any)["previews"].([]any)[i].(map[string]any)
	}
	tests := []struct {
		name   string
		record map[string]any
		want   string
	}{
		{"an uppercase token name", with(func(r map[string]any) { tokens(r)[0].(map[string]any)["name"] = "Signal" }), "BAD_NAME /tokens/0/name"},
		{"a group that starts with a digit", with(func(r map[string]any) { tokens(r)[0].(map[string]any)["group"] = "1x" }), "BAD_NAME /tokens/0/group"},
		{"an empty name", with(func(r map[string]any) { tokens(r)[0].(map[string]any)["name"] = "" }), "BAD_NAME /tokens/0/name"},
		{"a duplicate group and name", with(func(r map[string]any) {
			r["tokens"] = append(tokens(r), dsToken("colors", "signal", "color", "red"))
		}), "DUPLICATE_TOKEN /tokens/4"},
		{"an unknown type", with(func(r map[string]any) { tokens(r)[0].(map[string]any)["type"] = "weird" }), "BAD_VALUE /tokens/0/type"},
		{"an empty colour", with(func(r map[string]any) { tokens(r)[0].(map[string]any)["value"] = " " }), "BAD_VALUE /tokens/0/value"},
		{"an object for a colour", with(func(r map[string]any) { tokens(r)[0].(map[string]any)["value"] = map[string]any{"a": "b"} }), "BAD_VALUE /tokens/0/value"},
		{"a string for a typography", with(func(r map[string]any) { tokens(r)[2].(map[string]any)["value"] = "Inter" }), "BAD_VALUE /tokens/2/value"},
		{"a number inside a shadow", with(func(r map[string]any) { tokens(r)[3].(map[string]any)["value"] = map[string]any{"blur": 8} }), "BAD_VALUE /tokens/3/value"},
		{"an empty typography", with(func(r map[string]any) { tokens(r)[2].(map[string]any)["value"] = map[string]any{} }), "BAD_VALUE /tokens/2/value"},
		{"a number token that is no number", with(func(r map[string]any) {
			r["tokens"] = append(tokens(r), dsToken("scale", "ratio", "number", "abc"))
		}), "BAD_VALUE /tokens/4/value"},
		{"a duration token that is no duration", with(func(r map[string]any) {
			r["tokens"] = append(tokens(r), dsToken("motion", "fast", "duration", "quick"))
		}), "BAD_VALUE /tokens/4/value"},
		{"a value that closes the rule", with(func(r map[string]any) { tokens(r)[0].(map[string]any)["value"] = "red;} body{display:none" }), "BAD_VALUE /tokens/0/value"},
		{"a bad component slug", with(func(r map[string]any) { r["components"].([]any)[0].(map[string]any)["slug"] = "Bad Slug" }), "BAD_NAME /components/0/slug"},
		{"a duplicate component slug", with(func(r map[string]any) {
			r["components"] = append(r["components"].([]any), buttonComponent())
		}), "DUPLICATE_COMPONENT /components/1/slug"},
		{"a preview that names a missing token", with(func(r map[string]any) { preview(r, 0)["css"] = ".a{color:var(--colors-missing)}" }), "UNKNOWN_TOKEN /components/0/previews/0/css"},
		{"a script in the html", with(func(r map[string]any) { preview(r, 0)["html"] = `<p>hi</p><SCRIPT>alert(1)</SCRIPT>` }), "UNSAFE_PREVIEW /components/0/previews/0/html"},
		{"an event attribute", with(func(r map[string]any) { preview(r, 0)["html"] = `<button onclick="x()">Save</button>` }), "UNSAFE_PREVIEW /components/0/previews/0/html"},
		{"an event attribute after a quoted >", with(func(r map[string]any) { preview(r, 0)["html"] = `<a title=">" onclick="x">hi</a>` }), "UNSAFE_PREVIEW /components/0/previews/0/html"},
		{"an upper case event attribute", with(func(r map[string]any) { preview(r, 0)["html"] = `<a ONCLICK="x">hi</a>` }), "UNSAFE_PREVIEW /components/0/previews/0/html"},
		{"an event attribute with no space", with(func(r map[string]any) { preview(r, 0)["html"] = `<b/onclick=x>hi</b>` }), "UNSAFE_PREVIEW /components/0/previews/0/html"},
		{"a url that is no data url", with(func(r map[string]any) { preview(r, 0)["css"] = `.a{background:url("https://example.com/a.png")}` }), "UNSAFE_PREVIEW /components/0/previews/0/css"},
		{"an import", with(func(r map[string]any) { preview(r, 0)["css"] = `@import "x.css";` }), "UNSAFE_PREVIEW /components/0/previews/0/css"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, out := s.save(0, tc.record)
			if status != http.StatusBadRequest || out["code"] != "DESIGN_SYSTEM_INVALID" {
				t.Fatalf("%d %v", status, out)
			}
			if got := problems(t, out); !slices.Contains(got, tc.want) {
				t.Errorf("errors = %v, want %q among them", got, tc.want)
			}
		})
	}
	if got := s.get(); got["exists"] != false {
		t.Errorf("a refused save stored a version: %v", got)
	}
	if status, out := s.save(0, dsRecord(buttonComponent())); status != http.StatusOK {
		t.Errorf("the valid record: %d %v", status, out)
	}
}

func TestDesignSystem_ARemovedTokenThatAPreviewUsesIsRefusedNamingTheComponent(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord(buttonComponent()))
	rec := dsRecord(buttonComponent())
	rec["tokens"] = rec["tokens"].([]any)[1:] // drops colors.signal
	status, out := s.save(1, rec)
	if status != http.StatusBadRequest || !slices.ContainsFunc(problems(t, out), func(p string) bool { return strings.HasPrefix(p, "TOKEN_IN_USE ") }) {
		t.Fatalf("%d %v", status, out)
	}
	if !strings.Contains(string(mustJSON(out)), "button") {
		t.Errorf("the message does not name the component: %v", out)
	}
	if got := s.get(); got["version"] != float64(1) {
		t.Errorf("version = %v", got["version"])
	}
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func TestDesignSystem_VersionsListNewestFirstWithWhoWhenAndWhatChanged(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord())

	next := dsRecord(dsComponent("card", dsPreview(`<i></i>`, `.a{color:var(--colors-signal)}`)))
	next["tokens"] = append(next["tokens"].([]any)[:3:3], dsToken("colors", "extra", "color", "red")) // -shadow.card +colors.extra
	next["tokens"].([]any)[0].(map[string]any)["value"] = "oklch(0.6 0.1 182)"                        // ~colors.signal
	next["principles"] = "# Principles\n\n## Calm\n\nQuiet surfaces, changed.\n\n## Motion\n\nShort.\n"
	status, out := s.post("save_design_system", map[string]any{"base_version": 1, "record": next, "message": "Tune"})
	if status != http.StatusOK {
		t.Fatalf("%d %v", status, out)
	}

	list := s.mustPost("list_design_system_versions", map[string]any{})["versions"].([]any)
	if len(list) != 2 {
		t.Fatalf("%v", list)
	}
	v2, v1 := list[0].(map[string]any), list[1].(map[string]any)
	if v2["version"] != float64(2) || v1["version"] != float64(1) {
		t.Fatalf("not newest first: %v", list)
	}
	for _, v := range list {
		m := v.(map[string]any)
		if m["by"] != "person" || m["source"] != "manual" || m["at"] == nil || m["at"] == "" {
			t.Errorf("%v", m)
		}
	}
	if v2["message"] != "Tune" {
		t.Errorf("message = %v", v2["message"])
	}
	want2 := map[string]any{"tokens": "+1 ~1 -1", "principles": "+1 ~1 -0", "components": "+1 ~0 -0"}
	if got := v2["summary"]; !jsonEqual(got, want2) {
		t.Errorf("summary = %v, want %v", got, want2)
	}
	want1 := map[string]any{"tokens": "+4 ~0 -0", "principles": "+2 ~0 -0", "components": "+0 ~0 -0"}
	if got := v1["summary"]; !jsonEqual(got, want1) {
		t.Errorf("first summary = %v, want %v", got, want1)
	}

	page := s.mustPost("list_design_system_versions", map[string]any{"limit": 1})["versions"].([]any)
	if len(page) != 1 || page[0].(map[string]any)["version"] != float64(2) {
		t.Errorf("limit: %v", page)
	}
	older := s.mustPost("list_design_system_versions", map[string]any{"before": 2})["versions"].([]any)
	if len(older) != 1 || older[0].(map[string]any)["version"] != float64(1) {
		t.Errorf("before: %v", older)
	}
}

func jsonEqual(a, b any) bool { return bytes.Equal(mustJSON(a), mustJSON(b)) }

func TestDesignSystem_AVersionReadsAsItWasSavedAndUnknownOnesAreRefused(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord())
	s.mustSave(1, dsRecord(buttonComponent()))

	out := s.mustPost("get_design_system_version", map[string]any{"version": 1})
	if out["version"] != float64(1) || out["at"] == nil {
		t.Fatalf("%v", out)
	}
	if n := len(out["record"].(map[string]any)["components"].([]any)); n != 0 {
		t.Errorf("version 1 holds %d components", n)
	}
	if n := len(s.versionRecord(2)["components"].([]any)); n != 1 {
		t.Errorf("version 2 holds %d components", n)
	}

	if status, body := s.post("get_design_system_version", map[string]any{"version": 3}); status != http.StatusNotFound || body["code"] != "VERSION_NOT_FOUND" {
		t.Errorf("unknown version: %d %v", status, body)
	}
	for _, verb := range []string{"get_design_system", "save_design_system", "list_design_system_versions", "get_design_system_version", "rename_design_token"} {
		status, body := s.post(verb, map[string]any{"project_id": "nope", "version": 1, "base_version": 0, "record": dsRecord()})
		if status != http.StatusNotFound || body["code"] != "PROJECT_NOT_FOUND" {
			t.Errorf("%s on an unknown project: %d %v", verb, status, body)
		}
	}
}

func TestDesignSystem_SavesAndRenamesAreOnTheProjectFeed(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord())
	s.sawOnFeed("design_system_saved", `"project_id":"`+s.project+`"`, `"version":1`, `"source":"manual"`)
	if status, out := s.rename(1, "colors", "signal", "colors", "accent"); status != http.StatusOK {
		t.Fatalf("%d %v", status, out)
	}
	s.sawOnFeed("design_system_saved", `"project_id":"`+s.project+`"`, `"version":2`, `"source":"manual"`)
}

func TestDesignSystem_DeletingTheProjectDeletesItsDesignSystemAndVersions(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord())
	s.mustSave(1, dsRecord(buttonComponent()))
	other, err := s.cat.Projects.CreateProject(s.ctx, "gadgets", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.mustPost("save_design_system", map[string]any{"project_id": other.ID, "base_version": 0, "record": dsRecord()})

	if status, out := s.post("delete_project", map[string]any{"project_id": s.project}); status >= 300 {
		t.Fatalf("delete_project: %d %v", status, out)
	}

	if stored, err := s.store.Latest(s.ctx, s.project); err != nil || stored != nil {
		t.Errorf("the design system outlived its project: %v, %v", stored, err)
	}
	for v := 1; v <= 2; v++ {
		if got, err := s.store.Get(s.ctx, s.project, v); err != nil || got != nil {
			t.Errorf("version %d outlived its project: %v, %v", v, got, err)
		}
	}
	if got := s.mustPost("get_design_system", map[string]any{"project_id": other.ID}); got["version"] != float64(1) {
		t.Errorf("another project's design system changed: %v", got)
	}
}

func TestDesignSystem_RenamingATokenWritesOneVersionUnderTheNewName(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord())

	status, out := s.rename(1, "colors", "signal", "brand", "primary")
	if status != http.StatusOK || out["version"] != float64(2) {
		t.Fatalf("%d %v", status, out)
	}
	var found, old bool
	for _, tk := range s.versionRecord(2)["tokens"].([]any) {
		m := tk.(map[string]any)
		found = found || m["group"] == "brand" && m["name"] == "primary" && m["value"] == "oklch(0.5 0.095 182)"
		old = old || m["group"] == "colors" && m["name"] == "signal"
	}
	if !found || old {
		t.Errorf("found under the new name: %v, still under the old: %v", found, old)
	}
	list := s.mustPost("list_design_system_versions", map[string]any{})["versions"].([]any)
	top := list[0].(map[string]any)
	if top["by"] != "person" || top["source"] != "manual" || top["summary"].(map[string]any)["tokens"] != "+1 ~0 -1" {
		t.Errorf("%v", top)
	}
}

func TestDesignSystem_ARenameIsRefusedForAStaleBaseADuplicateABadNameOrAnUnknownToken(t *testing.T) {
	s := startDesignServer(t)
	s.mustSave(0, dsRecord())
	s.mustSave(1, dsRecord())

	status, out := s.rename(1, "colors", "signal", "colors", "accent")
	if status != http.StatusConflict || out["code"] != "DESIGN_SYSTEM_CHANGED" || out["details"].(map[string]any)["current_version"] != float64(2) {
		t.Errorf("stale base: %d %v", status, out)
	}
	for _, tc := range []struct {
		name                     string
		group, tname, ngrp, nnam string
		want                     string
	}{
		{"onto an existing token", "colors", "signal", "spacing", "md", "DUPLICATE_TOKEN "},
		{"to a bad name", "colors", "signal", "colors", "Not Valid", "BAD_NAME "},
		{"to a bad group", "colors", "signal", "Colors", "signal", "BAD_NAME "},
		{"of an unknown token", "colors", "ghost", "colors", "spirit", "UNKNOWN_TOKEN "},
	} {
		status, out := s.rename(2, tc.group, tc.tname, tc.ngrp, tc.nnam)
		if status != http.StatusBadRequest || out["code"] != "DESIGN_SYSTEM_INVALID" {
			t.Errorf("%s: %d %v", tc.name, status, out)
			continue
		}
		if !slices.ContainsFunc(problems(t, out), func(p string) bool { return strings.HasPrefix(p, tc.want) }) {
			t.Errorf("%s: errors = %v, want %s", tc.name, problems(t, out), tc.want)
		}
	}
	if got := s.get(); got["version"] != float64(2) {
		t.Errorf("a refused rename wrote a version: %v", got["version"])
	}
}

func TestDesignSystem_ARenameRewritesEveryPreviewReferenceIncludingCompositeKeys(t *testing.T) {
	s := startDesignServer(t)
	button := dsComponent("button",
		dsPreview(`<button class="btn" style="color:var(--colors-signal)">Save</button>`, `.btn{background:var(--colors-signal);border:1px solid var( --colors-signal );color:var(--colors-signal-strong,var(--colors-signal))}`),
		dsPreview(`<h1>x</h1>`, `h1{font-size:var(--typography-headline-font-size);font-family:var(--typography-headline-font-family);line-height:var(--typography-headline-line-height)}`),
	)
	card := dsComponent("card", dsPreview(`<div></div>`, `div{box-shadow:0 var(--shadow-card-offset-y) var(--shadow-card-blur) var(--shadow-card-color);padding:var(--spacing-md)}`))
	rec := dsRecord(button, card)
	rec["tokens"] = append(rec["tokens"].([]any), dsToken("colors", "signal-strong", "color", "navy"))
	s.mustSave(0, rec)

	if status, out := s.rename(1, "colors", "signal", "brand", "primary"); status != http.StatusOK {
		t.Fatalf("scalar: %d %v", status, out)
	}
	if status, out := s.rename(2, "typography", "headline", "type", "title"); status != http.StatusOK {
		t.Fatalf("typography: %d %v", status, out)
	}
	if status, out := s.rename(3, "shadow", "card", "elevation", "raised"); status != http.StatusOK {
		t.Fatalf("shadow: %d %v", status, out)
	}

	raw := string(mustJSON(s.versionRecord(4)["components"]))
	for _, gone := range []string{"var(--colors-signal)", "--colors-signal-strong,var(--colors-signal", "--typography-headline", "--shadow-card"} {
		if strings.Contains(raw, gone) {
			t.Errorf("%q remains after the renames: %s", gone, raw)
		}
	}
	for _, want := range []string{
		"background:var(--brand-primary)", "style=\\\"color:var(--brand-primary)\\\"", "var(--colors-signal-strong,var(--brand-primary))",
		"font-size:var(--type-title-font-size)", "font-family:var(--type-title-font-family)", "line-height:var(--type-title-line-height)",
		"var(--elevation-raised-offset-y)", "var(--elevation-raised-blur)", "var(--elevation-raised-color)", "var(--spacing-md)",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("%q is missing after the renames: %s", want, raw)
		}
	}
	if got := s.get(); got["version"] != float64(4) {
		t.Errorf("version = %v, want one version per rename", got["version"])
	}
}
