package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeArchitectureAPI answers the repository architecture endpoints of
// "Contract: Harness server ↔ Web UI — repository architecture record" the
// way the server does, as far as the browser can tell: versions and
// ARCHITECTURE_CHANGED, validation errors with JSON pointer paths, the layer
// preview with overlaps, export refusals, and the project feed events. The
// browser tests run against it, so they hold at the UI's seam whichever side
// of the contract is built first.
type fakeArchitectureAPI struct {
	mu      sync.Mutex
	feed    *sseFeed
	exists  bool
	version int
	record  map[string]any
	export  map[string]any
	// files is the checkout's file list, for the layer preview.
	files []string
	// agents is the catalog; discovered is what discovery returns.
	agents     []string
	discovered map[string]any
	events     []map[string]any
	// exportErr, when set, is the code the next export refuses with.
	exportErr string
	calls     []string
	// refuseSaveWith, when set, makes the next save fail with ARCHITECTURE_INVALID and these errors.
	refuseSaveWith []archError
	// saveDelay, when set, holds each save's answer back after the save is stored and announced.
	saveDelay time.Duration
	// saved is every record a save stored, newest last.
	saved []map[string]any
	// contexts are the project's bounded context names (nil: sessions, billing);
	// contextProjects are the project ids list_bounded_contexts was asked for.
	contexts        []string
	contextProjects []string
}

func newFakeArchitectureAPI(feed *sseFeed) *fakeArchitectureAPI {
	return &fakeArchitectureAPI{
		feed:   feed,
		agents: []string{"software-architect", "go-developer", "devops"},
		files: []string{
			"internal/domain/zone.go", "internal/domain/session.go",
			"internal/ports/zones.go", "internal/adapter/in/web/web.go",
			"cmd/server/main.go", "README.md",
		},
		discovered: map[string]any{
			"instructions": []any{map[string]any{"path": "CLAUDE.md", "kind": "rules", "loaded": "always"}},
			"tools":        []any{map[string]any{"name": "codegraph", "kind": "mcp", "source": "mcp catalog"}},
			"sandboxes":    []any{map[string]any{"name": "seatbelt", "source": "sandbox settings"}},
			"agents":       []any{"software-architect", "go-developer", "devops"},
		},
		export: map[string]any{"state": "never_exported", "path": ".harness/architecture.yaml"},
	}
}

// starter is what get answers with when no record exists.
func starterRecord() map[string]any {
	return map[string]any{
		"version": 1, "repository": "harness",
		"instructions": []any{}, "tools": []any{}, "sandboxes": []any{},
		"orchestration": map[string]any{"routing": []any{}, "handoffs": []any{}},
		"layers":        []any{},
		"unlayered":     map[string]any{"default": "read", "agents": map[string]any{"software-architect": "change", "devops": "change"}},
		"rules": []any{map[string]any{
			"id": "stay-in-your-layer", "kind": "edit_outside_layer", "mode": "warning",
			"message": "You may only change files in the layers your agent owns. Ask the architect.",
		}},
	}
}

// seed stores a saved record, as if the person had saved it earlier.
func (f *fakeArchitectureAPI) seed(record map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exists, f.version, f.record = true, 1, record
}

func (f *fakeArchitectureAPI) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// saveElsewhere is a save made by another tab: the version moves on and the
// project feed announces it.
func (f *fakeArchitectureAPI) saveElsewhere(t *testing.T, mutate func(rec map[string]any)) {
	t.Helper()
	f.mu.Lock()
	mutate(f.record)
	f.version++
	v := f.version
	f.mu.Unlock()
	f.emit(map[string]any{"repository_architecture_changed": map[string]any{"repository_id": "r1", "version": v}})
}

func (f *fakeArchitectureAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/architecture.schema.json" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema"}`))
		return
	}
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	if r.URL.Path == "/api/save_repository_architecture" {
		defer f.delaySaveAnswer() // runs after the lock below is released
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, strings.TrimPrefix(r.URL.Path, "/api/"))
	switch r.URL.Path {
	case "/api/get_repository_architecture":
		f.json(w, f.state())
	case "/api/discover_repository_architecture":
		f.json(w, f.discovered)
	case "/api/preview_layers":
		f.json(w, f.preview(in))
	case "/api/save_repository_architecture":
		f.save(w, in)
	case "/api/delete_repository_architecture":
		f.exists, f.version, f.record = false, 0, nil
		f.json(w, map[string]any{})
	case "/api/export_repository_architecture":
		f.doExport(w, in)
	case "/api/list_bounded_contexts":
		f.listBoundedContexts(w, r.URL.Query().Get("project_id"))
	case "/api/list_architecture_rule_events":
		f.json(w, map[string]any{"events": append([]map[string]any{}, f.events...)})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeArchitectureAPI) delaySaveAnswer() {
	f.mu.Lock()
	d := f.saveDelay
	f.mu.Unlock()
	time.Sleep(d)
}

func (f *fakeArchitectureAPI) state() map[string]any {
	rec := f.record
	if !f.exists {
		rec = starterRecord()
	}
	return map[string]any{
		"repository_id": "r1", "exists": f.exists, "version": f.version,
		"updated_at": time.Now().Format(time.RFC3339), "record": rec, "export": f.export,
	}
}

func (f *fakeArchitectureAPI) json(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeArchitectureAPI) fail(w http.ResponseWriter, status int, code, msg string, details any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"error": msg, "code": code}
	if details != nil {
		body["details"] = details
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fakeArchitectureAPI) save(w http.ResponseWriter, in map[string]any) {
	base, _ := in["base_version"].(float64)
	if int(base) != f.version {
		f.fail(w, http.StatusConflict, "ARCHITECTURE_CHANGED", "The architecture was saved by someone else.", nil)
		return
	}
	rec, _ := in["record"].(map[string]any)
	errs := validateArchitecture(rec)
	if len(f.refuseSaveWith) > 0 {
		errs, f.refuseSaveWith = f.refuseSaveWith, nil
	}
	if len(errs) > 0 {
		f.fail(w, http.StatusBadRequest, "ARCHITECTURE_INVALID", "The architecture breaks validation.", map[string]any{"errors": errs})
		return
	}
	f.exists, f.record = true, rec
	f.saved = append(f.saved, rec)
	f.version++
	if st, _ := f.export["state"].(string); st == "in_sync" {
		f.export["state"] = "database_ahead"
	}
	f.emit(map[string]any{"repository_architecture_changed": map[string]any{"repository_id": "r1", "version": f.version}})
	f.json(w, f.state())
}

func (f *fakeArchitectureAPI) doExport(w http.ResponseWriter, in map[string]any) {
	if v, _ := in["version"].(float64); int(v) != f.version {
		f.fail(w, http.StatusConflict, "ARCHITECTURE_CHANGED", "The architecture changed.", nil)
		return
	}
	if f.exportErr != "" {
		code := f.exportErr
		f.exportErr = ""
		f.fail(w, http.StatusConflict, code, "refused: "+code, nil)
		return
	}
	f.export = map[string]any{
		"state": "in_sync", "path": ".harness/architecture.yaml",
		"exported_version": f.version, "exported_commit": "abc1234def", "exported_at": time.Now().Format(time.RFC3339),
	}
	f.emit(map[string]any{"repository_architecture_changed": map[string]any{"repository_id": "r1", "version": f.version}})
	f.json(w, map[string]any{"commit": "abc1234def", "path": ".harness/architecture.yaml", "export": f.export})
}

type archError = map[string]any

var nameRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// validateArchitecture is the part of the server's validation the browser
// tests reach: missing fields, a bad regex, duplicate names.
func validateArchitecture(rec map[string]any) []archError {
	var errs []archError
	add := func(path, code, msg string) {
		errs = append(errs, archError{"path": path, "code": code, "message": msg})
	}
	seen := map[string]bool{}
	layers, _ := rec["layers"].([]any)
	for i, raw := range layers {
		l, _ := raw.(map[string]any)
		name, _ := l["name"].(string)
		if seen[name] {
			add(fmt.Sprintf("/layers/%d/name", i), "DUPLICATE_NAME", "Two layers share a name.")
		}
		seen[name] = true
		m, _ := l["match"].(map[string]any)
		folder, _ := m["folder"].(string)
		rx, hasRx := m["regex"].(string)
		switch {
		case folder == "" && !hasRx:
			add(fmt.Sprintf("/layers/%d/match", i), "MISSING_FIELD", "A layer needs a folder or a regex.")
		case hasRx:
			if _, err := regexp.Compile(rx); err != nil {
				add(fmt.Sprintf("/layers/%d/match/regex", i), "INVALID_REGEX", "The regex does not compile.")
			}
		}
	}
	rules, _ := rec["rules"].([]any)
	for i, raw := range rules {
		ru, _ := raw.(map[string]any)
		if msg, _ := ru["message"].(string); strings.TrimSpace(msg) == "" {
			add(fmt.Sprintf("/rules/%d/message", i), "MISSING_FIELD", "A rule needs a message.")
		}
		if ru["kind"] == "command" {
			if c, _ := ru["command"].(string); c == "" {
				add(fmt.Sprintf("/rules/%d/command", i), "MISSING_FIELD", "A command rule needs a command.")
			}
			if a, _ := ru["at"].(string); a == "" {
				add(fmt.Sprintf("/rules/%d/at", i), "MISSING_FIELD", "A command rule needs a lifecycle point.")
			}
		}
	}
	tools, _ := rec["tools"].([]any)
	for i, raw := range tools {
		tl, _ := raw.(map[string]any)
		if w, _ := tl["writes"].(bool); w {
			if u, _ := tl["usage"].(string); strings.TrimSpace(u) == "" {
				add(fmt.Sprintf("/tools/%d/usage", i), "MISSING_FIELD", "A tool that writes needs usage prose.")
			}
		}
	}
	return errs
}

// preview matches the checkout's files against the draft layers the way the
// contract says: one winning layer per path, narrowest first.
func (f *fakeArchitectureAPI) preview(in map[string]any) map[string]any {
	layers, _ := in["layers"].([]any)
	type lay struct {
		name  string
		rx    *regexp.Regexp
		isRx  bool
		depth int
		order int
	}
	var ls []lay
	var out []any
	for i, raw := range layers {
		l, _ := raw.(map[string]any)
		name, _ := l["name"].(string)
		m, _ := l["match"].(map[string]any)
		item := lay{name: name, order: i}
		if rx, ok := m["regex"].(string); ok {
			c, err := regexp.Compile(rx)
			if err != nil {
				out = append(out, map[string]any{"name": name, "file_count": 0, "sample": []any{}, "error": map[string]any{"code": "INVALID_REGEX", "message": err.Error()}})
				ls = append(ls, lay{name: name, order: i, depth: -1})
				continue
			}
			item.rx, item.isRx = c, true
		} else if folder, _ := m["folder"].(string); folder != "" {
			if strings.HasPrefix(folder, "/") || strings.Contains(folder, "..") {
				out = append(out, map[string]any{"name": name, "file_count": 0, "sample": []any{}, "error": map[string]any{"code": "INVALID_FOLDER", "message": "bad folder"}})
				ls = append(ls, lay{name: name, order: i, depth: -1})
				continue
			}
			item.rx = regexp.MustCompile("^" + regexp.QuoteMeta(strings.Trim(folder, "/")) + "(/|$)")
			item.depth = strings.Count(folder, "/") + 1
		} else {
			item.depth = -1
		}
		ls = append(ls, item)
	}
	counts := map[string]int{}
	samples := map[string][]any{}
	var unl []any
	var overlaps []any
	for _, p := range f.files {
		var hits []lay
		for _, l := range ls {
			if l.rx != nil && l.rx.MatchString(p) {
				hits = append(hits, l)
			}
		}
		if len(hits) == 0 {
			unl = append(unl, p)
			continue
		}
		win := hits[0]
		for _, h := range hits[1:] {
			if (h.isRx && !win.isRx) || (h.isRx == win.isRx && !h.isRx && h.depth > win.depth) {
				win = h
			}
		}
		counts[win.name]++
		samples[win.name] = append(samples[win.name], p)
		if len(hits) > 1 {
			var names []any
			for _, h := range hits {
				names = append(names, h.name)
			}
			overlaps = append(overlaps, map[string]any{"path": p, "layers": names, "winner": win.name})
		}
	}
	have := map[string]bool{}
	for _, raw := range out {
		have[raw.(map[string]any)["name"].(string)] = true
	}
	var res []any
	for _, l := range ls {
		if have[l.name] {
			for _, raw := range out {
				if raw.(map[string]any)["name"] == l.name {
					res = append(res, raw)
				}
			}
			continue
		}
		s := samples[l.name]
		if s == nil {
			s = []any{}
		}
		res = append(res, map[string]any{"name": l.name, "file_count": counts[l.name], "sample": s, "error": nil})
	}
	if res == nil {
		res = []any{}
	}
	if overlaps == nil {
		overlaps = []any{}
	}
	if unl == nil {
		unl = []any{}
	}
	return map[string]any{"layers": res, "unlayered": map[string]any{"file_count": len(unl), "sample": unl}, "overlaps": overlaps}
}

// emit sends one event to every follower of the project feed.
func (f *fakeArchitectureAPI) emit(v any) {
	b, _ := json.Marshal(v)
	f.feed.mu.Lock()
	defer f.feed.mu.Unlock()
	for _, c := range f.feed.conns {
		c <- b
	}
}

// bumpSilently is a save made elsewhere that no feed event announced.
func (f *fakeArchitectureAPI) bumpSilently(mutate func(rec map[string]any)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	mutate(f.record)
	f.version++
}

func (f *fakeArchitectureAPI) lastSaved() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.saved) == 0 {
		return nil
	}
	return f.saved[len(f.saved)-1]
}

// listBoundedContexts answers GET /api/list_bounded_contexts?project_id=…
// with { "bounded_contexts": [ { id, name } ] }.
func (f *fakeArchitectureAPI) listBoundedContexts(w http.ResponseWriter, projectID string) {
	f.contextProjects = append(f.contextProjects, projectID)
	names := f.contexts
	if names == nil {
		names = []string{"sessions", "billing"}
	}
	out := []any{}
	for i, n := range names {
		out = append(out, map[string]any{"id": fmt.Sprintf("bc%d", i+1), "name": n, "description": ""})
	}
	f.json(w, map[string]any{"bounded_contexts": out})
}

// discoverCalls is how many times the page asked for the agent catalog.
func (f *fakeArchitectureAPI) discoverCalls() int {
	n := 0
	for _, c := range f.called() {
		if c == "discover_repository_architecture" {
			n++
		}
	}
	return n
}
