package httpapi

import (
	"fmt"
	"testing"
)

// The usage an agent shows is every session that ever ran as it, in any
// project and any status, and the architecture zones that assign it.
func TestCatalog_AnAgentShowsItsSessionAndZoneCounts(t *testing.T) {
	f := catalogWithSkills(t)
	f.write("agents.json", `{"agents":{"zeta":{"description":"Z","skills":["tdd"]},"alpha":{"description":"A"}}}`)
	run := func(sql string, args ...any) {
		t.Helper()
		if err := f.db.Exec(sql, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, st := range []string{"running", "done", "failed"} {
		run(`INSERT INTO sessions (id, agent_id, project_id, status) VALUES (?, 'zeta', ?, ?)`, fmt.Sprint("s", i), fmt.Sprint("p", i), st)
	}
	run(`INSERT INTO sessions (id, agent_id, project_id, status) VALUES ('s9', 'other', 'p1', 'done')`)
	run(`INSERT INTO zones (id, name, repository_id, access_json) VALUES ('z1', 'api', 'r1', '{"default":"read","agents":{"zeta":"write","other":"read"}}')`)
	run(`INSERT INTO zones (id, name, repository_id, access_json) VALUES ('z2', 'web', 'r1', '{"default":"read","agents":{"zeta":"read"}}')`)
	run(`INSERT INTO zones (id, name, repository_id, access_json) VALUES ('z3', 'plain', '', '{"default":"read","agents":{"zeta":"read"}}')`)

	view := f.get("/api/get_agent_catalog", 200)
	usage := func(name string) map[string]any {
		u, _ := byName(t, view["agents"], name)["usage"].(map[string]any)
		return u
	}
	if u := usage("zeta"); u["sessions"] != float64(3) || u["zones"] != float64(2) {
		t.Fatalf("zeta usage = %v", u)
	}
	if u := usage("alpha"); u == nil || u["sessions"] != float64(0) || u["zones"] != float64(0) {
		t.Fatalf("an unused agent counts zero, got %v", u)
	}
}
