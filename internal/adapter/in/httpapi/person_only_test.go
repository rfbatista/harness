package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// fixedVerifier accepts one credential.
type fixedVerifier string

func (f fixedVerifier) Verify(_ context.Context, c string) bool { return c != "" && c == string(f) }

type recordedRefusals struct{ got []ports.PersonRefusal }

func (r *recordedRefusals) Refused(_ context.Context, p ports.PersonRefusal) {
	r.got = append(r.got, p)
}

// fakePairing redeems one code.
type fakePairing struct{ code string }

func (f *fakePairing) Redeem(_ context.Context, code string) (string, error) {
	if code == "" || code != f.code {
		return "", &domain.StructuredError{Code: "PAIRING_CODE_INVALID", Message: "no"}
	}
	f.code = ""
	return "the-credential", nil
}
func (f *fakePairing) NewCode() string { f.code = "NEWCODE"; return f.code }

func credentialRouter(refusals ports.PersonRefusals, opts ...Option) http.Handler {
	opts = append(opts, WithPersonProof(fixedVerifier("the-credential"), refusals))
	return NewRouter(NewHandler(Services{GateRuns: &fakeGateRuns{}, Pairing: &fakePairing{code: "CODE"}}), opts...)
}

func do(h http.Handler, method, path string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "127.0.0.1:8080"
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// concrete is a classified pattern as a path to call.
func concrete(pattern string) string {
	parts := strings.Split(pattern, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "x"
		}
	}
	return strings.Join(parts, "/")
}

func personOnlyRoutes() []string {
	var out []string
	for k, c := range routeClasses {
		if c == classPerson {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func TestEveryPersonOnlyRouteRefusesACallerWithoutTheCredentialWhateverElseItPresents(t *testing.T) {
	refusals := &recordedRefusals{}
	h := credentialRouter(refusals, WithToken("api-token"))
	notThePerson := map[string]func(*http.Request){
		"no credential": func(r *http.Request) { r.Header.Set("Authorization", "Bearer api-token") },
		"a wrong credential": func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer api-token")
			r.Header.Set(PersonHeader, "guess")
		},
		"a wrong cookie": func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer api-token")
			r.AddCookie(&http.Cookie{Name: PersonCookie, Value: "guess"})
		},
		"the bearer token as the credential": func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer api-token")
			r.Header.Set(PersonHeader, "api-token")
		},
		"loopback host and own origin only": func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer api-token")
			r.Host = "127.0.0.1:8080"
			r.Header.Set("Origin", "http://127.0.0.1:8080")
		},
		"a session marker": func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer api-token")
			r.Header.Set("X-Harness-Session", "someone")
			r.Header.Set("X-Harness-Person", "")
		},
	}
	for _, route := range personOnlyRoutes() {
		method, pattern, _ := strings.Cut(route, " ")
		for name, mutate := range notThePerson {
			rec := do(h, method, concrete(pattern), mutate)
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "PERSON_ONLY") {
				t.Errorf("%s with %s answered %d %s, want 403 PERSON_ONLY", route, name, rec.Code, rec.Body)
			}
		}
	}
	if len(refusals.got) == 0 {
		t.Error("no refusal was reported")
	}
}

func TestAPersonOnlyRouteBehavesAsBeforeWithTheCredentialAsHeaderOrCookie(t *testing.T) {
	h := credentialRouter(nil)
	asHeader := func(r *http.Request) { r.Header.Set(PersonHeader, "the-credential") }
	asCookie := func(r *http.Request) { r.AddCookie(&http.Cookie{Name: PersonCookie, Value: "the-credential"}) }
	for name, mutate := range map[string]func(*http.Request){"header": asHeader, "cookie": asCookie} {
		rec := do(h, http.MethodPost, "/api/sessions/s1/gate-runs", mutate)
		if rec.Code != http.StatusOK {
			t.Errorf("the person's gate-run trigger by %s answered %d %s", name, rec.Code, rec.Body)
		}
	}
	// A route that is not person-only needs none.
	if rec := do(h, http.MethodGet, "/api/sessions/s1/gate-runs", nil); rec.Code != http.StatusOK {
		t.Errorf("a session-callable route answered %d", rec.Code)
	}
}

func TestEveryRouteIsClassified(t *testing.T) {
	e := NewRouter(NewHandler(Services{}))
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
	for _, m := range missing {
		t.Errorf("unclassified route: %s — add it to routeClasses as person-only, session-callable or open (ADR 0016)", m)
	}
}

func TestPairingRedeemsTheCodeOnceIntoACookieAndANewCodeNeedsTheCredential(t *testing.T) {
	h := credentialRouter(nil)
	rec := do(h, http.MethodGet, PairPath+"?code=CODE", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("pair = %d %s", rec.Code, rec.Body)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == PersonCookie {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Value != "the-credential" {
		t.Fatalf("cookie = %+v", cookie)
	}
	if rec := do(h, http.MethodGet, PairPath+"?code=CODE", nil); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "PAIRING_CODE_INVALID") {
		t.Errorf("a second use answered %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, http.MethodGet, PairPath+"?code=CODE", func(r *http.Request) { r.Host = "evil.example" }); rec.Code != http.StatusForbidden {
		t.Errorf("pairing from another host name answered %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, PairingCodesPath, nil); rec.Code != http.StatusForbidden {
		t.Errorf("a new code without the credential answered %d", rec.Code)
	}
	rec = do(h, http.MethodPost, PairingCodesPath, func(r *http.Request) { r.AddCookie(cookie) })
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "NEWCODE") {
		t.Errorf("a new code with the credential answered %d %s", rec.Code, rec.Body)
	}
	// A client that is not a browser redeems for the credential in the body.
	post := do(h, http.MethodPost, PairPath, func(r *http.Request) { r.Body = http.NoBody })
	if post.Code != http.StatusBadRequest && post.Code != http.StatusForbidden {
		t.Errorf("a pair POST with no code answered %d", post.Code)
	}
}

func TestTheBenchRoutesBehaveAsBeforeWithEnforcementOnAndTheCredentialAndRefuseWithout(t *testing.T) {
	h := benchHandler(t, ":memory:")
	router := NewRouter(h, WithPersonProof(fixedVerifier("the-credential"), nil))
	call := func(method, path, body string, credential bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if credential {
			req.Header.Set(PersonHeader, "the-credential")
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := call(http.MethodPost, "/projects/p1/bench/cases", benchCaseBody, false); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "PERSON_ONLY") {
		t.Fatalf("a bench case created without the credential: %d %s", rec.Code, rec.Body)
	}
	rec := call(http.MethodPost, "/projects/p1/bench/cases", benchCaseBody, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("a bench case created with the credential: %d %s", rec.Code, rec.Body)
	}
	// The reads stay open to a session; the writes of the bench are the person's.
	if rec := call(http.MethodGet, "/projects/p1/bench/cases", "", false); rec.Code != http.StatusOK {
		t.Errorf("listing cases without the credential: %d", rec.Code)
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/projects/p1/bench/runs"},
		{http.MethodPost, "/projects/p1/bench/runs/r1/cancel"},
		{http.MethodPost, "/projects/p1/bench/cases/from-trajectory"},
		{http.MethodPut, "/projects/p1/tasks/t1/trajectory-recording"},
		{http.MethodDelete, "/projects/p1/bench/cases/c1"},
	} {
		if rec := call(c.method, c.path, "{}", false); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without the credential: %d", c.method, c.path, rec.Code)
		}
	}
}
