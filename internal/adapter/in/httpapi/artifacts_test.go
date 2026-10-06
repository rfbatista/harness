package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// startDesignSession starts an interactive session over the API and returns
// it; its WorkingDir is the worktree artifacts come from.
func startDesignSession(t *testing.T, env *sessionTestEnv) domain.Session {
	t.Helper()
	status, started := post(t, env.srv.URL+"/api/start_interactive_session", `{"project_id":"p1","repository_id":"r1","ticket_id":"tk1"}`)
	if status != http.StatusCreated {
		t.Fatalf("start = %d (%s)", status, started.Code)
	}
	return started.Session
}

func writeIn(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func publish(t *testing.T, env *sessionTestEnv, req ports.PublishArtifactRequest) *domain.Artifact {
	t.Helper()
	a, err := env.artifacts.Publish(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func get(t *testing.T, url string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestHTTP_ArtifactsListGetDelete(t *testing.T) {
	env := newSessionTestEnv(t)
	defer env.srv.Close()
	sess := startDesignSession(t, env)
	writeIn(t, sess.WorkingDir, "design/card.html", "<h1>card</h1>")
	a := publish(t, env, ports.PublishArtifactRequest{SessionID: sess.ID, Path: "design/card.html", Title: "Card", Note: "first"})

	resp := get(t, env.srv.URL+"/api/artifacts?session_id="+sess.ID, nil)
	var listed struct {
		Artifacts []domain.Artifact `json:"artifacts"`
	}
	json.NewDecoder(resp.Body).Decode(&listed)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(listed.Artifacts) != 1 || listed.Artifacts[0].ID != a.ID || listed.Artifacts[0].Path != "design/card.html" {
		t.Fatalf("list = %d %+v", resp.StatusCode, listed)
	}
	if resp = get(t, env.srv.URL+"/api/artifacts?ticket_id=tk1", nil); resp.StatusCode != 200 {
		t.Fatalf("list by ticket = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if resp = get(t, env.srv.URL+"/api/artifacts", nil); resp.StatusCode != 400 {
		t.Fatalf("list without a filter = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	resp = get(t, env.srv.URL+"/api/artifacts/"+a.ID, nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"id":"`+a.ID+`"`) || !strings.Contains(string(body), `"url":null`) {
		t.Fatalf("get = %d %s", resp.StatusCode, body)
	}
	if resp = get(t, env.srv.URL+"/api/artifacts/ghost", nil); resp.StatusCode != 404 {
		t.Fatalf("ghost = %d", resp.StatusCode)
	}
	resp.Body.Close()

	req, _ := http.NewRequest("DELETE", env.srv.URL+"/api/artifacts/"+a.ID, nil)
	dresp, _ := http.DefaultClient.Do(req)
	dresp.Body.Close()
	if dresp.StatusCode != 204 {
		t.Fatalf("delete = %d", dresp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(sess.WorkingDir, "design", "card.html")); err != nil {
		t.Fatal("delete removed the file:", err)
	}
	if resp = get(t, env.srv.URL+"/api/artifacts/"+a.ID, nil); resp.StatusCode != 404 {
		t.Fatalf("after delete = %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHTTP_ArtifactView_HeadersAndSubresources(t *testing.T) {
	env := newSessionTestEnv(t)
	defer env.srv.Close()
	sess := startDesignSession(t, env)
	writeIn(t, sess.WorkingDir, "design/card.html", `<link rel="stylesheet" href="style.css">`)
	writeIn(t, sess.WorkingDir, "design/style.css", "h1{color:red}")
	writeIn(t, sess.WorkingDir, ".git/config", "[core]")
	a := publish(t, env, ports.PublishArtifactRequest{SessionID: sess.ID, Path: "design/card.html", Title: "Card"})
	base := env.srv.URL + "/api/artifacts/" + a.ID + "/view/"

	resp := get(t, base, nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != `<link rel="stylesheet" href="style.css">` {
		t.Fatalf("view = %d %s", resp.StatusCode, body)
	}
	for k, want := range map[string]string{
		"Content-Type":            "text/html; charset=utf-8",
		"Content-Length":          "40",
		"ETag":                    `"1"`,
		"Cache-Control":           "no-cache",
		"X-Content-Type-Options":  "nosniff",
		"Content-Disposition":     "inline",
		"Content-Security-Policy": artifactCSP,
	} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}

	if resp = get(t, base, map[string]string{"If-None-Match": `"1"`}); resp.StatusCode != 304 {
		t.Fatalf("If-None-Match = %d, want 304", resp.StatusCode)
	}
	resp.Body.Close()

	// HEAD answers with the same headers and no body (curl -I, the plan's smoke check).
	head, err := http.Head(base)
	if err != nil {
		t.Fatal(err)
	}
	hb, _ := io.ReadAll(head.Body)
	head.Body.Close()
	if head.StatusCode != 200 || len(hb) != 0 || head.Header.Get("Content-Security-Policy") != artifactCSP || head.Header.Get("ETag") != `"1"` {
		t.Fatalf("HEAD = %d body %d bytes, headers %v", head.StatusCode, len(hb), head.Header)
	}

	resp = get(t, base+"style.css", nil)
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "h1{color:red}" || resp.Header.Get("Content-Type") != "text/css; charset=utf-8" || resp.Header.Get("Content-Security-Policy") != artifactCSP {
		t.Fatalf("sibling = %d %s %v", resp.StatusCode, body, resp.Header)
	}

	for _, rel := range []string{"nope.css", "../.git/config", "../../../../etc/passwd", "..%2F..%2F..%2Fetc%2Fpasswd", "../design"} {
		resp = get(t, base+rel, nil)
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Errorf("%s = %d, want 404", rel, resp.StatusCode)
		}
	}

	// Trailing slash is significant; the slashless form redirects to it.
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	rr, _ := noRedirect.Get(strings.TrimSuffix(base, "/"))
	rr.Body.Close()
	if rr.StatusCode != 301 || rr.Header.Get("Location") != "/api/artifacts/"+a.ID+"/view/" {
		t.Fatalf("slashless = %d %q", rr.StatusCode, rr.Header.Get("Location"))
	}

	// A re-publish shows the new content and a new ETag on plain reload.
	writeIn(t, sess.WorkingDir, "design/card.html", "<h1>v2</h1>")
	publish(t, env, ports.PublishArtifactRequest{SessionID: sess.ID, Path: "design/card.html", Title: "Card"})
	resp = get(t, base, map[string]string{"If-None-Match": `"1"`})
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "<h1>v2</h1>" || resp.Header.Get("ETag") != `"2"` {
		t.Fatalf("after re-publish = %d %s %s", resp.StatusCode, body, resp.Header.Get("ETag"))
	}
}

func TestHTTP_ArtifactView_Range(t *testing.T) {
	env := newSessionTestEnv(t)
	defer env.srv.Close()
	sess := startDesignSession(t, env)
	writeIn(t, sess.WorkingDir, "clip.mp4", "0123456789")
	a := publish(t, env, ports.PublishArtifactRequest{SessionID: sess.ID, Path: "clip.mp4", Title: "Clip"})

	resp := get(t, env.srv.URL+"/api/artifacts/"+a.ID+"/view/", map[string]string{"Range": "bytes=2-5"})
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || string(body) != "2345" || resp.Header.Get("Content-Range") != "bytes 2-5/10" || resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("range = %d %q %v", resp.StatusCode, body, resp.Header)
	}
	if resp.Header.Get("Content-Type") != "video/mp4" || resp.Header.Get("Content-Security-Policy") != artifactCSP || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("range response lost headers: %v", resp.Header)
	}
}

func TestHTTP_ArtifactView_URLKindHasNoFile(t *testing.T) {
	env := newSessionTestEnv(t)
	defer env.srv.Close()
	sess := startDesignSession(t, env)
	a := publish(t, env, ports.PublishArtifactRequest{SessionID: sess.ID, URL: "http://localhost:3000", Title: "Dev"})
	resp := get(t, env.srv.URL+"/api/artifacts/"+a.ID+"/view/", nil)
	var body struct {
		Code string `json:"code"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if resp.StatusCode != 404 || body.Code != "ARTIFACT_NOT_FOUND" {
		t.Fatalf("url view = %d %q", resp.StatusCode, body.Code)
	}
}

// The artifact event rides the interactive session's existing SSE stream,
// with the artifact embedded and a seq, so the Design tab can follow it.
func TestHTTP_ArtifactEventOnInteractiveSessionStream(t *testing.T) {
	env := newSessionTestEnv(t)
	defer env.srv.Close()
	sess := startDesignSession(t, env)
	writeIn(t, sess.WorkingDir, "design/card.html", "<h1>card</h1>")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", env.srv.URL+"/api/sessions/"+sess.ID+"/events", nil)
	sresp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()

	a := publish(t, env, ports.PublishArtifactRequest{SessionID: sess.ID, Path: "design/card.html", Title: "Card", Note: "first"})

	sc := bufio.NewScanner(sresp.Body)
	var lastID string
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "id: ") {
			lastID = strings.TrimPrefix(line, "id: ")
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev ports.SessionEvent
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev) != nil || ev.Type != "artifact" {
			continue
		}
		if ev.Artifact == nil || ev.Artifact.ID != a.ID || ev.Text != "first" || lastID == "" || ev.Seq == 0 {
			t.Fatalf("artifact event = %+v (id line %q)", ev, lastID)
		}
		return
	}
	t.Fatal("no artifact event on the stream")
}
