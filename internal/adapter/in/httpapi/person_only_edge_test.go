package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// Adversarial tests of the person-only gate and the route inventory (seam C,
// ADR 0016).

// unclassifiedAPIRoutes is the inventory check of TestEveryRouteIsClassified,
// as a function, so it can be run over a router with a route added.
func unclassifiedAPIRoutes(e *echo.Echo) []string {
	var missing []string
	for _, r := range e.Routes() {
		if !strings.HasPrefix(r.Path, "/api") || strings.HasPrefix(r.Method, "echo_") {
			continue
		}
		if _, ok := routeClasses[r.Method+" "+r.Path]; !ok {
			missing = append(missing, r.Method+" "+r.Path)
		}
	}
	sort.Strings(missing)
	return missing
}

func TestTheInventoryNamesARouteAddedWithoutAClassAndAcceptsTheRealRouter(t *testing.T) {
	e := NewRouter(NewHandler(Services{}))
	if got := unclassifiedAPIRoutes(e); len(got) != 0 {
		t.Fatalf("the real router has unclassified routes: %v", got)
	}
	ok := func(c echo.Context) error { return c.NoContent(http.StatusOK) }
	e.POST("/api/sessions/:id/new_mutation", ok)
	e.DELETE("/api/zz_new_delete", ok)
	e.GET("/api/zz_new_read", ok)
	e.PUT("/api/zz_new_put", ok)
	got := unclassifiedAPIRoutes(e)
	want := []string{"DELETE /api/zz_new_delete", "GET /api/zz_new_read", "POST /api/sessions/:id/new_mutation", "PUT /api/zz_new_put"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("the inventory found %v, want %v", got, want)
	}
}

func TestEveryClassifiedRouteExistsAndHasAKnownClass(t *testing.T) {
	e := NewRouter(NewHandler(Services{}))
	registered := map[string]bool{}
	for _, r := range e.Routes() {
		registered[r.Method+" "+r.Path] = true
	}
	for k, c := range routeClasses {
		if c != classSession && c != classPerson && c != classOpen {
			t.Errorf("%s has class %d", k, c)
		}
		if !registered[k] {
			t.Errorf("%s is classified but no such route is registered: a typo leaves the real route unclassified or ungated", k)
		}
	}
}

func TestEveryRouteThatChangesStateIsPersonOnlyUnlessASessionOwnsIt(t *testing.T) {
	// A mutating verb on a route classed session-callable must be a read in
	// disguise (a POST that only lists or previews) or a hook/shim endpoint.
	// The list below is the reviewed set at this head: a new entry is a
	// decision for the person, so the test fails and names it.
	reviewed := map[string]bool{
		"POST /api/sessions/:id/architecture/check": true, "POST /api/sessions/:id/architecture/commit": true,
		"POST /api/worktrees/:id/architecture/commit": true, "POST /api/sessions/:id/branch_gate": true,
		"POST /api/sessions/:id/branch_gate/grant": true, "POST /api/sessions/:id/branch_gate/pushed": true,
		"POST /api/sessions/:id/memory/hook": true, "POST /api/sessions/:id/task_worktrees/denials": true,
	}
	for k, c := range routeClasses {
		method, pattern, _ := strings.Cut(k, " ")
		if c != classSession || method == "GET" || method == "HEAD" || reviewed[k] {
			continue
		}
		last := pattern[strings.LastIndex(pattern, "/")+1:]
		if strings.HasPrefix(last, "get_") || strings.HasPrefix(last, "list_") || strings.HasPrefix(last, "find_") ||
			strings.HasPrefix(last, "preview_") || strings.HasPrefix(last, "check_") {
			continue
		}
		t.Errorf("%s is session-callable and changes state: a session can call it with the bearer token alone", k)
	}
}

func TestAnUnclassifiedRouteIsNotLeftOpenByTheGate(t *testing.T) {
	// Fail closed: a route that reaches the gate with no class should need the
	// credential until someone classifies it. (Today the gate gates only the
	// routes classed person-only, so the inventory test is the only guard.)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/zz_unclassified", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/zz_unclassified")
	reached := false
	next := func(echo.Context) error { reached = true; return nil }
	_ = personGate(options{person: fixedVerifier("the-credential")})(next)(c)
	if reached {
		t.Error("a mutating route with no class ran with no credential")
	}
}

func TestAPersonOnlyRouteIsNeverReachedByAnotherMethodOrAnOddPathWithoutTheCredential(t *testing.T) {
	h := credentialRouter(&recordedRefusals{}, WithToken("api-token"))
	bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer api-token") }
	reached := func(method, path string) (code int, panicked bool) {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		return do(h, method, path, bearer).Code, false
	}
	for _, route := range personOnlyRoutes() {
		verb, pattern, _ := strings.Cut(route, " ")
		p := concrete(pattern)
		variants := []struct{ method, path string }{
			{http.MethodGet, p}, {http.MethodPut, p}, {http.MethodPatch, p}, {http.MethodDelete, p},
			{http.MethodHead, p}, {http.MethodOptions, p},
			{verb, p + "/"}, {verb, strings.Replace(p, "/api/", "/api//", 1)}, {verb, strings.ToUpper(p)},
			{verb, p + "?harness_person=the-credential"}, {verb, p + "?X-Harness-Person=the-credential"},
			{verb, p + ";x=1"}, {verb, strings.Replace(p, "/api/", "/api/./", 1)}, {verb, strings.Replace(p, "/api/", "/api/x/../", 1)},
			{verb, "/" + strings.TrimPrefix(p, "/") + "%20"}, {verb, strings.Replace(p, "/api", "/%61pi", 1)},
		}
		for _, v := range variants {
			if v.method == verb && v.path == p {
				continue
			}
			if c, own := routeClasses[v.method+" "+pattern]; own && v.path == p && c != classPerson {
				continue // the same path is another, classified route (a read)
			}
			code, panicked := reached(v.method, v.path)
			if panicked || (code >= 200 && code < 300) {
				t.Errorf("%s as %s %s answered %d (panic %v) with the bearer token alone", route, v.method, v.path, code, panicked)
			}
		}
	}
}

func TestTheCredentialIsReadOnlyFromTheHeaderAndTheCookieNeverFromTheQueryOrAnotherSource(t *testing.T) {
	h := credentialRouter(&recordedRefusals{}, WithToken("api-token"))
	bearer := func(r *http.Request) { r.Header.Set("Authorization", "Bearer api-token") }
	post := func(mutate func(*http.Request)) int {
		return do(h, http.MethodPost, "/api/sessions/s1/gate-runs", func(r *http.Request) { bearer(r); mutate(r) }).Code
	}
	refused := map[string]func(*http.Request){
		"in the query":                    func(r *http.Request) { r.URL.RawQuery = url.Values{PersonCookie: {"the-credential"}, "person": {"the-credential"}}.Encode() },
		"as the bearer token":             func(r *http.Request) { r.Header.Set("Authorization", "Bearer the-credential") },
		"as basic auth":                   func(r *http.Request) { r.SetBasicAuth("the-credential", "the-credential") },
		"in a cookie of another name":     func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "session", Value: "the-credential"}) },
		"with a space around":             func(r *http.Request) { r.Header.Set(PersonHeader, " the-credential x") },
		"a prefix of the credential":      func(r *http.Request) { r.Header.Set(PersonHeader, "the-credentia") },
		"extended":                        func(r *http.Request) { r.Header.Set(PersonHeader, "the-credential0") },
		"a wrong header with right cookie": func(r *http.Request) {
			r.Header.Set(PersonHeader, "guess")
			r.AddCookie(&http.Cookie{Name: PersonCookie, Value: "the-credential"})
		},
		"in the body":      func(r *http.Request) { r.Body = http.NoBody; r.Header.Set("X-Harness-Credential", "the-credential") },
		"a forged cookie":  func(r *http.Request) { r.AddCookie(&http.Cookie{Name: PersonCookie, Value: "forged"}) },
		"an empty cookie":  func(r *http.Request) { r.AddCookie(&http.Cookie{Name: PersonCookie, Value: ""}) },
		"an unrelated host": func(r *http.Request) { r.Host = "evil.example"; r.Header.Set("Origin", "http://evil.example") },
		"the session id":   func(r *http.Request) { r.Header.Set("X-Harness-Session", "s1") },
	}
	for name, mutate := range refused {
		if code := post(mutate); code != http.StatusForbidden && code != http.StatusUnauthorized {
			t.Errorf("the credential %s answered %d, want 403 (or 401)", name, code)
		}
	}
	accepted := map[string]func(*http.Request){
		"as the header": func(r *http.Request) { r.Header.Set(PersonHeader, "the-credential") },
		"as the cookie": func(r *http.Request) { r.AddCookie(&http.Cookie{Name: PersonCookie, Value: "the-credential"}) },
		"header plus a wrong cookie": func(r *http.Request) {
			r.Header.Set(PersonHeader, "the-credential")
			r.AddCookie(&http.Cookie{Name: PersonCookie, Value: "guess"})
		},
		"in a cookie after a decoy": func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "a", Value: "b"})
			r.AddCookie(&http.Cookie{Name: PersonCookie, Value: "the-credential"})
		},
	}
	for name, mutate := range accepted {
		if code := post(mutate); code != http.StatusOK {
			t.Errorf("the credential %s answered %d, want 200", name, code)
		}
	}
}

func TestAnExpiredOrReusedOrWrongPairingCodeGivesNoCookieOnEitherPairingRoute(t *testing.T) {
	h := credentialRouter(nil)
	cookieOf := func(rec *httptest.ResponseRecorder) bool {
		for _, c := range rec.Result().Cookies() {
			if c.Name == PersonCookie {
				return true
			}
		}
		return false
	}
	for name, code := range map[string]string{"wrong": "NOPE", "empty": "", "lower case": "code", "padded": " CODE "} {
		rec := do(h, http.MethodGet, PairPath+"?code="+url.QueryEscape(code), nil)
		if rec.Code != http.StatusForbidden || cookieOf(rec) || !strings.Contains(rec.Body.String(), "PAIRING_CODE_INVALID") {
			t.Errorf("%s: %d %s cookie %v", name, rec.Code, rec.Body, cookieOf(rec))
		}
	}
	// The JSON route gives the credential once, and the code is spent on the GET route too.
	post := func(body string) *httptest.ResponseRecorder {
		return do(h, http.MethodPost, PairPath, func(r *http.Request) { r.Body = nopBody(body) })
	}
	if rec := post(`{"code":"CODE"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "the-credential") {
		t.Fatalf("pair: %d %s", rec.Code, rec.Body)
	}
	if rec := post(`{"code":"CODE"}`); rec.Code != http.StatusForbidden {
		t.Errorf("the code was used twice: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, PairPath+"?code=CODE", nil); rec.Code != http.StatusForbidden || cookieOf(rec) {
		t.Errorf("the spent code on the other route: %d cookie %v", rec.Code, cookieOf(rec))
	}
	for _, host := range []string{"evil.example", "127.0.0.1.evil.example", "localhost.evil.example:8080", "192.168.1.5:8080", "0.0.0.0:8080"} {
		rec := post(`{"code":"CODE"}`)
		_ = rec
		rec = do(h, http.MethodPost, PairPath, func(r *http.Request) { r.Host = host; r.Body = nopBody(`{"code":"CODE"}`) })
		if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "the-credential") {
			t.Errorf("pairing from host %q answered %d %s", host, rec.Code, rec.Body)
		}
	}
}

func nopBody(s string) *nopCloser { return &nopCloser{strings.NewReader(s)} }

type nopCloser struct{ *strings.Reader }

func (nopCloser) Close() error { return nil }
