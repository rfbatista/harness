//go:build !windows

package web_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// builtInFake stands in for the server side of ADR 0088 while that side has
// not landed: the page's own requests are answered here, in the contract's
// wire format (revision 2), and every other request goes to the real server.
// Once the server lists built-in skills these tests run against it, too (see
// skipUnlessServerListsBuiltInSkills); this fake is the page-only seam.
type builtInFake struct {
	// catalog edits the get_agent_catalog answer the real server gave.
	catalog func(cat map[string]any)
	// skills answers get_catalog_skill by name with the whole body; a name not
	// here goes to the real server.
	skills map[string]map[string]any
	// refuse answers a write (save_catalog_skill, delete_catalog_skill) with
	// the coded error; it is keyed by the endpoint.
	refuse map[string]fakeRefusal
	mu sync.Mutex
	// writes records every write the page sent, as "endpoint body".
	writes []string
}

// sent is the writes the page has sent so far.
func (f *builtInFake) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...)
}

type fakeRefusal struct {
	status  int
	code    string
	message string
}

// builtInSkill is a built-in skill in the get_agent_catalog list.
func builtInSkill(name, description string, modes ...string) map[string]any {
	return map[string]any{
		"name": name, "description": description, "file_count": 1, "has_extra_files": false,
		"agents": []string{}, "bundles": []string{}, "agent_count": 0, "problems": []any{},
		"built_in": true, "modes": modes,
	}
}

// builtInSkillBody is the get_catalog_skill answer for a built-in skill.
func builtInSkillBody(name, description, body string, modes ...string) map[string]any {
	return map[string]any{
		"skill": map[string]any{
			"name": name, "description": description, "body": body,
			"files": []map[string]any{
				{"path": "SKILL.md", "dir": false, "size": len(body), "content": body, "encoding": "utf8"},
				{"path": "notes.md", "dir": false, "size": 6, "content": "notes\n", "encoding": "utf8"},
			},
			"agents": []string{}, "bundles": []string{}, "agent_count": 0,
			"built_in": true, "modes": modes,
		},
		"versions": map[string]string{},
	}
}

// install turns the interception on for the browser b.
func (f *builtInFake) install(t *testing.T, b *browser) {
	t.Helper()
	chromedp.ListenTarget(b.ctx, func(ev any) {
		e, ok := ev.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go f.answer(b.ctx, e)
	})
	patterns := []*fetch.RequestPattern{
		// The page's first paint carries the catalog as an inline seed.
		{URLPattern: "*/agents*", ResourceType: network.ResourceTypeDocument, RequestStage: fetch.RequestStageResponse},
		{URLPattern: "*/api/get_agent_catalog*", RequestStage: fetch.RequestStageResponse},
		{URLPattern: "*/api/get_catalog_skill*", RequestStage: fetch.RequestStageRequest},
		{URLPattern: "*/api/save_catalog_skill*", RequestStage: fetch.RequestStageRequest},
		{URLPattern: "*/api/save_catalog_agent*", RequestStage: fetch.RequestStageRequest},
		{URLPattern: "*/api/save_catalog_bundle*", RequestStage: fetch.RequestStageRequest},
		{URLPattern: "*/api/delete_catalog_skill*", RequestStage: fetch.RequestStageRequest},
	}
	if err := chromedp.Run(b.ctx, fetch.Enable().WithPatterns(patterns)); err != nil {
		t.Fatal(err)
	}
}

func (f *builtInFake) answer(ctx context.Context, e *fetch.EventRequestPaused) {
	ctx = cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Target)
	u, _ := url.Parse(e.Request.URL)
	endpoint := u.Path[strings.LastIndex(u.Path, "/")+1:]
	respond := func(status int64, v any) {
		out, _ := json.Marshal(v)
		_ = fetch.FulfillRequest(e.RequestID, status).
			WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "application/json"}}).
			WithBody(base64.StdEncoding.EncodeToString(out)).Do(ctx)
	}
	pass := func() { _ = fetch.ContinueRequest(e.RequestID).Do(ctx) }

	if e.ResourceType == network.ResourceTypeDocument {
		f.answerDocument(ctx, e)
		return
	}
	switch endpoint {
	case "get_agent_catalog":
		raw, err := fetch.GetResponseBody(e.RequestID).Do(ctx)
		if err != nil {
			pass()
			return
		}
		var cat map[string]any
		if json.Unmarshal(raw, &cat) != nil {
			_ = fetch.ContinueResponse(e.RequestID).Do(ctx)
			return
		}
		if f.catalog != nil {
			f.catalog(cat)
		}
		respond(200, cat)
	case "get_catalog_skill":
		if body, ok := f.skills[u.Query().Get("name")]; ok {
			respond(200, body)
			return
		}
		pass()
	default: // a write
		f.mu.Lock()
		f.writes = append(f.writes, endpoint+" "+postBody(e.Request))
		f.mu.Unlock()
		if r, ok := f.refuse[endpoint]; ok {
			respond(int64(r.status), map[string]any{"error": r.message, "code": r.code})
			return
		}
		pass()
	}
}

const seedOpen = `<script id="agent-catalog-seed" type="application/json">`

// answerDocument edits the catalog seed inside the page's HTML.
func (f *builtInFake) answerDocument(ctx context.Context, e *fetch.EventRequestPaused) {
	raw, err := fetch.GetResponseBody(e.RequestID).Do(ctx)
	if err != nil {
		_ = fetch.ContinueResponse(e.RequestID).Do(ctx)
		return
	}
	html := string(raw)
	at := strings.Index(html, seedOpen)
	end := strings.Index(html[max(at, 0):], "</script>")
	if at < 0 || end < 0 || f.catalog == nil {
		_ = fetch.ContinueResponse(e.RequestID).Do(ctx)
		return
	}
	from := at + len(seedOpen)
	to := at + end
	var cat map[string]any
	if json.Unmarshal([]byte(html[from:to]), &cat) != nil {
		_ = fetch.ContinueResponse(e.RequestID).Do(ctx)
		return
	}
	f.catalog(cat)
	out, _ := json.Marshal(cat)
	html = html[:from] + strings.ReplaceAll(string(out), "<", `\u003c`) + html[to:]
	headers := []*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}
	_ = fetch.FulfillRequest(e.RequestID, 200).WithResponseHeaders(headers).WithBody(base64.StdEncoding.EncodeToString([]byte(html))).Do(ctx)
}

// postBody is the request's body, from its post data entries (base64 on the wire).
func postBody(r *network.Request) string {
	var out strings.Builder
	for _, entry := range r.PostDataEntries {
		raw, err := base64.StdEncoding.DecodeString(entry.Bytes)
		if err != nil {
			out.WriteString(entry.Bytes)
			continue
		}
		out.Write(raw)
	}
	return out.String()
}
