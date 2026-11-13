//go:build !windows

package web_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// Service requests on a branch (spec "Web UI — service requests on a branch", testing decisions;
// contract "Harness server ↔ Web UI — services", revision 9, sections 12 and 13). The server's
// revision 9 is not in this branch, so the page is driven through revision9Gateway, a stub in front
// of the real server that answers in the revision 9 shape: a request on a branch carries its
// `branch` object, and a run an agent started shows `where.kind: "branch"`. Swapping it for the
// real server, once its branch has landed, is a matter of using w.srv.URL instead of gw.URL.

const (
	stubBranchSha = "abc1234" // a branch's tip when the request was filed
	stubRunSha    = "def5678" // its tip when the approved run started: newer
)

// revision9Gateway fronts a real servicesWorld. Every JSON body, and every event on the page
// stream, is rewritten on the way to the browser: a request whose worktree is `branch:<name>` gains
// its `branch` {name, sha, remote}, and the run of a session-started service reads as a run on
// branch feature/x at stubRunSha.
type revision9Gateway struct{ *httptest.Server }

func newRevision9Gateway(t *testing.T, w *servicesWorld) *revision9Gateway {
	t.Helper()
	target, err := url.Parse(w.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Header.Del("Accept-Encoding")
		},
		Transport:     w.srv.Client().Transport,
		FlushInterval: -1,
		ModifyResponse: func(resp *http.Response) error {
			switch ct := resp.Header.Get("Content-Type"); {
			case strings.HasPrefix(ct, "application/json"):
				body, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil {
					return err
				}
				body = rewriteRevision9(body)
				resp.Body = io.NopCloser(bytes.NewReader(body))
				resp.ContentLength = int64(len(body))
				resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
			case strings.HasPrefix(ct, "text/event-stream"):
				resp.Body = rewriteEvents(resp.Body)
			}
			return nil
		},
	}
	gw := &revision9Gateway{httptest.NewUnstartedServer(proxy)}
	gw.EnableHTTP2 = true
	gw.StartTLS()
	t.Cleanup(gw.Close)
	return gw
}

// rewriteEvents rewrites each `data:` line of an event stream as it passes.
func rewriteEvents(body io.ReadCloser) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer body.Close()
		r := bufio.NewReader(body)
		for {
			line, err := r.ReadBytes('\n')
			if rest, ok := bytes.CutPrefix(line, []byte("data: ")); ok {
				line = append([]byte("data: "), rewriteRevision9(rest)...)
			}
			if len(line) > 0 {
				if _, werr := pw.Write(line); werr != nil {
					return
				}
			}
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
		}
	}()
	return pr
}

// rewriteRevision9 returns the JSON as is when it is not JSON.
func rewriteRevision9(in []byte) []byte {
	trimmed := bytes.TrimRight(in, "\r\n")
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return in
	}
	out, err := json.Marshal(revision9Shape(v))
	if err != nil {
		return in
	}
	return append(out, in[len(trimmed):]...)
}

func revision9Shape(v any) any {
	switch x := v.(type) {
	case []any:
		for i := range x {
			x[i] = revision9Shape(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = revision9Shape(x[k])
		}
		if wt, _ := x["worktree"].(string); strings.HasPrefix(wt, "branch:") {
			name := strings.TrimPrefix(wt, "branch:")
			remote := strings.HasPrefix(name, "origin/")
			x["branch"] = map[string]any{"name": strings.TrimPrefix(name, "origin/"), "sha": stubBranchSha, "remote": remote}
		}
		if by, _ := x["started_by"].(map[string]any); by != nil && by["kind"] == "session" {
			if where, _ := x["where"].(map[string]any); where != nil && where["kind"] == "main" {
				x["where"] = map[string]any{"kind": "branch", "dir": where["dir"], "branch": "feature/x", "sha": stubRunSha, "remote": false, "session_id": ""}
				x["worktree_key"] = "branch:feature/x"
			}
		}
	}
	return v
}

func newWebService(t *testing.T, w *servicesWorld) *domain.Service {
	t.Helper()
	svc, err := w.svc.CreateService(context.Background(), w.project.ID, domain.ServiceInput{
		Name: "web", Kind: domain.ServiceKindCommand, RepositoryID: w.repo.ID, Command: &domain.ServiceCommand{Line: "echo up; sleep 30"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// A start or restart request on a branch arrives live and says which branch at which commit, as a
// run on a branch does; a request on a remote branch says so; a request on main shows no branch.
func TestAServiceRequestOnABranchShowsTheBranchAndItsCommitOnItsCard_EndToEnd(t *testing.T) {
	w := newServicesWorld(t)
	svc := newWebService(t, w)
	gw := newRevision9Gateway(t, w)
	const cards = `[...document.querySelectorAll('[data-service-request]')].map((c) => c.querySelector('h3').textContent.replace(/^Service\s+/, '').trim() + ' | ' + ([...c.querySelectorAll('[data-request-branch]')].filter((e) => e.checkVisibility()).map((e) => e.textContent.trim()).join('') || 'no branch')).sort().join('\n')`
	b := newTLSBrowser(t)
	b.run("the page is open with nothing waiting",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(gw.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`!!document.querySelector('[data-service][data-name=web]')`),
		waitUntil(`!document.querySelector('[data-requests-badge]').checkVisibility()`),
	)
	ask := func(action domain.ServiceRequestAction, worktree string, n int) *agentCall {
		call := w.agentAsks(t, ports.ServiceRequestInput{Action: action, ServiceID: svc.ID, ServiceName: "web", Worktree: worktree})
		waitForRequests(t, w, n)
		return call
	}
	ask(domain.ServiceRequestStart, "branch:feature/x", 1)
	b.run("the request arrives by the feed, and its card shows the branch and its commit",
		waitUntil(`document.querySelector('[data-requests-badge]').checkVisibility() && document.querySelector('[data-requests-badge]').textContent.includes('1')`),
		chromedp.Click(`[data-requests-badge]`, chromedp.ByQuery),
		waitUntil(`(`+cards+`) === 'Start web | branch @ feature/x · abc1234'`),
	)
	ask(domain.ServiceRequestRestart, "branch:origin/feature/y", 2)
	ask(domain.ServiceRequestRestart, "main", 3)
	b.run("a restart on a remote branch says origin/, and a request on main shows no branch",
		waitUntil(`(`+cards+`) === 'Restart web | branch @ origin/feature/y · abc1234\nRestart web | no branch\nStart web | branch @ feature/x · abc1234'`),
		waitUntil(`[...document.querySelectorAll('[data-request-worktree]')].filter((e) => e.checkVisibility()).map((e) => e.textContent).join() === 'Runs in the main checkout.'`),
		chromedp.Sleep(300*time.Millisecond),
		theme("light"),
		chromedp.Sleep(200*time.Millisecond),
		shot("request-branch-light"),
		theme("dark"),
		chromedp.Sleep(200*time.Millisecond),
		shot("request-branch-dark"),
	)
}

// Approving a start on a branch runs it, and the row's run line then reads as a run on a branch
// with the commit the run started at, which is newer than the one the card showed.
func TestAnApprovedServiceRequestOnABranchShowsAsARunOnTheBranch_EndToEnd(t *testing.T) {
	w := newServicesWorld(t)
	svc := newWebService(t, w)
	gw := newRevision9Gateway(t, w)
	call := w.agentAsks(t, ports.ServiceRequestInput{Action: domain.ServiceRequestStart, ServiceID: svc.ID, ServiceName: "web", Worktree: "branch:feature/x"})
	waitForRequests(t, w, 1)
	const where = `document.querySelector('[data-service][data-name=web] [data-service-where]')?.textContent`
	b := newTLSBrowser(t)
	b.run("the card shows the commit it was filed at",
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate(gw.URL+"/projects/"+w.project.ID+"/services"),
		waitUntil(`document.querySelector('[data-requests-badge]')?.checkVisibility()`),
		chromedp.Click(`[data-requests-badge]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-request-branch]')?.textContent === 'branch @ feature/x · `+stubBranchSha+`'`),
	)
	b.run("Approve: the card says done and the run line shows the run on the branch, at the commit it started at",
		chromedp.Click(`[data-service-request] [data-action=approve]`, chromedp.ByQuery),
		waitUntil(`document.querySelector('[data-request-outcome]')?.textContent.includes('Approved and done.')`),
		waitUntil(`(`+where+`) === 'branch @ feature/x · `+stubRunSha+`'`),
	)
	call.answer(t)
}
