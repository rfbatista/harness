package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestAgentSystemAndBenchRoutesAreClassifiedAndTheWritesArePersonOnly(t *testing.T) {
	h := credentialRouter(nil)
	session := func(r *http.Request) { r.Header.Set("X-Harness-Session", "s1") }
	for _, route := range []string{
		"PATCH /api/projects/:project/bench/cases/:case",
		"PUT /api/projects/:project/tasks/:task/trajectory-recording",
		"POST /api/projects/:project/bench/runs",
	} {
		if routeClasses[route] != classPerson {
			t.Errorf("%s is not person-only", route)
		}
		method, pattern, _ := strings.Cut(route, " ")
		rec := do(h, method, concrete(pattern), session)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "PERSON_ONLY") {
			t.Errorf("%s with a session credential answered %d %s, want 403 PERSON_ONLY", route, rec.Code, rec.Body)
		}
	}
	for _, route := range []string{
		"GET /api/projects/:project/repositories/:repository/agent-system/check-sh-import",
		"GET /api/projects/:project/bench/scorecards/:scorecard/compare",
	} {
		if routeClasses[route] != classSession {
			t.Errorf("%s is not session-callable", route)
		}
	}
}
