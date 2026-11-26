package httpapi

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"operators-mcp/internal/application/services"
)

// Credenciais de acesso do serviço (ADR 0004): label and value pairs on a
// service, with their values on the person's side of the HTTP API.

const credSecret = "hunter2-credential-value"

func credentialsOf(svc map[string]any) []any {
	c, ok := svc["credentials"].([]any)
	if !ok {
		return nil
	}
	return c
}

func creds(pairs ...string) []any {
	out := []any{}
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, map[string]any{"label": pairs[i], "value": pairs[i+1]})
	}
	return out
}

func (f *servicesFixture) update(id string, in map[string]any) (int, map[string]any) {
	return f.call("POST", "/api/update_service", map[string]any{"service_id": id, "service": in})
}

func TestCredentials_ThePersonSetsThemAndReadsThemBackWithTheirValues(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	in := commandInput("web")
	in["credentials"] = creds("user", "admin", "password", credSecret)
	code, out := f.create(in)
	if code != 200 {
		t.Fatalf("create = %d %v", code, out)
	}
	id := out["service"].(map[string]any)["id"].(string)
	want := creds("user", "admin", "password", credSecret)

	if got := credentialsOf(out["service"].(map[string]any)); !reflect.DeepEqual(got, want) {
		t.Fatalf("created credentials = %v", got)
	}
	if got := credentialsOf(f.service(id)); !reflect.DeepEqual(got, want) {
		t.Fatalf("get_service credentials = %v", got)
	}
	_, list := f.call("GET", "/api/list_services?project_id="+f.projectID, nil)
	if got := credentialsOf(list["services"].([]any)[0].(map[string]any)); !reflect.DeepEqual(got, want) {
		t.Fatalf("list_services credentials = %v", got)
	}

	// An update that sends credentials replaces them; one that sends none
	// keeps them; an empty list clears them.
	upd := commandInput("web")
	upd["credentials"] = creds("user", "root")
	if code, out := f.update(id, upd); code != 200 || !reflect.DeepEqual(credentialsOf(out["service"].(map[string]any)), creds("user", "root")) {
		t.Fatalf("update = %d %v", code, out)
	}
	if code, out := f.update(id, commandInput("web")); code != 200 || !reflect.DeepEqual(credentialsOf(out["service"].(map[string]any)), creds("user", "root")) {
		t.Fatalf("an update without credentials keeps them: %d %v", code, out)
	}
	upd["credentials"] = []any{}
	if code, out := f.update(id, upd); code != 200 || len(credentialsOf(out["service"].(map[string]any))) != 0 {
		t.Fatalf("an empty list clears them: %d %v", code, out)
	}
}

func TestCredentials_AServiceWithNoneListsAnEmptyArray(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	id := f.createID(commandInput("web"))
	got, present := f.service(id)["credentials"]
	if arr, ok := got.([]any); !present || !ok || len(arr) != 0 {
		t.Fatalf("credentials = %v (present %v), want []", got, present)
	}
}

func TestCredentials_EveryChangeReachesTheFeedWithTheCredentials(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	id := f.createID(commandInput("web"))
	events := sseEvents(t, f.srv.URL+"/api/services/events?project_id="+f.projectID)

	upd := commandInput("web")
	upd["credentials"] = creds("user", "admin")
	f.update(id, upd)
	e := nextEvent(t, events)
	if e["kind"] != "service_changed" || !reflect.DeepEqual(credentialsOf(e["service"].(map[string]any)), creds("user", "admin")) {
		t.Fatalf("event = %v", e)
	}
	upd["credentials"] = []any{}
	f.update(id, upd)
	e = nextEvent(t, events)
	if e["kind"] != "service_changed" || len(credentialsOf(e["service"].(map[string]any))) != 0 {
		t.Fatalf("clearing reaches the feed: %v", e)
	}
}

func TestCredentials_BadOnesAreRefusedWithoutQuotingTheValue(t *testing.T) {
	f := newServicesFixture(t, services.Options{})
	for _, c := range []struct {
		name string
		in   []any
		want string
	}{
		{"empty label", creds("", credSecret), "label"},
		{"duplicate label", creds("user", credSecret, "user", "x"), "user"},
	} {
		in := commandInput("web")
		in["credentials"] = c.in
		code, out := f.create(in)
		_, msg := errorOf(out)
		if code != 400 || !strings.Contains(msg, c.want) || strings.Contains(msg, credSecret) {
			t.Errorf("%s: %d %v", c.name, code, out)
		}
	}
}

func labelsOf(view any) []string { return anyStrings(view.(map[string]any)["credential_labels"]) }

func TestCredentials_AgentsSetAndUnsetThemThroughTheToolsAndSeeOnlyTheLabels(t *testing.T) {
	f := newRequestsFixture(t, 5*time.Second)
	f.autoApprove()
	f.ticketID = "task-1"
	f.session("sess-1", f.projectID, f.repoID, true)
	agent := f.as("sess-1")

	created := agent.ok("create_service", map[string]any{
		"name": "app", "kind": "command", "repository": "api", "reason": "a test account",
		"command":     map[string]any{"line": "sleep 30"},
		"credentials": map[string]any{"user": "tester", "password": credSecret},
	})["service"]
	if got := labelsOf(created); !slices.Equal(got, []string{"password", "user"}) {
		t.Fatalf("created labels = %v", got)
	}
	id := f.createdID("app")
	if got := credentialsOf(f.service(id)); !reflect.DeepEqual(got, creds("password", credSecret, "user", "tester")) {
		t.Fatalf("the person reads the values: %v", got)
	}

	upd := agent.ok("update_service", map[string]any{
		"name": "app", "reason": "rotate",
		"changes": map[string]any{"credentials": map[string]any{"set": map[string]any{"password": "new-" + credSecret, "token": "t"}, "unset": []string{"user"}}},
	})
	if got := labelsOf(upd["service"]); !slices.Equal(got, []string{"password", "token"}) {
		t.Fatalf("labels after the update = %v", got)
	}
	if got := credentialsOf(f.service(id)); !reflect.DeepEqual(got, creds("password", "new-"+credSecret, "token", "t")) {
		t.Fatalf("credentials after the update: %v", got)
	}

	// Unrelated updates leave them alone.
	agent.ok("update_service", map[string]any{"name": "app", "reason": "port", "changes": map[string]any{"command": map[string]any{"ports": []int{9000}}}})
	if got := credentialsOf(f.service(id)); len(got) != 2 {
		t.Fatalf("an update of something else changed the credentials: %v", got)
	}

	// No value of a credential reaches any agent-facing output.
	for _, out := range []map[string]any{agent.ok("list_services", nil), agent.ok("service_status", named("app")), upd} {
		b, _ := json.Marshal(out)
		if strings.Contains(string(b), credSecret) {
			t.Fatalf("a credential value reached the agent: %s", b)
		}
	}
	if got := labelsOf(agent.ok("service_status", named("app"))["service"]); !slices.Equal(got, []string{"password", "token"}) {
		t.Fatalf("status labels = %v", got)
	}
	_, msg := agent.call("create_service", map[string]any{
		"name": "bad", "kind": "command", "reason": "r", "command": map[string]any{"line": "x"},
		"credentials": map[string]any{"": credSecret},
	})
	if !strings.Contains(msg, "label") || strings.Contains(msg, credSecret) {
		t.Fatalf("an empty label is refused without the value: %q", msg)
	}
}

func requestCredentials(r map[string]any) []map[string]any {
	var out []map[string]any
	for _, c := range r["credentials"].([]any) {
		out = append(out, c.(map[string]any))
	}
	return out
}

func TestCredentials_APendingRequestShowsThePersonTheChangeAndTheFeedNoValue(t *testing.T) {
	f := newRequestsFixture(t, 5*time.Second)
	f.ticketID = "task-1"
	f.session("sess-1", f.projectID, f.repoID, true)
	agent := f.as("sess-1")
	feed, err := f.svc.FollowServices(context.Background(), f.projectID)
	if err != nil {
		t.Fatal(err)
	}

	done := agent.callAsync("create_service", map[string]any{
		"name": "app", "kind": "command", "reason": "a test account", "command": map[string]any{"line": "sleep 30"},
		"credentials": map[string]any{"user": "tester", "password": credSecret},
	})
	r := f.awaitPending(1)[0]
	got := requestCredentials(r)
	if len(got) != 2 || got[0]["label"] != "password" || got[0]["value"] != credSecret || got[0]["change"] != "set" || got[1]["label"] != "user" {
		t.Fatalf("the person reads each credential with its value: %v", got)
	}
	if s, _ := r["summary"].(string); strings.Contains(s, credSecret) {
		t.Fatalf("summary holds a value: %q", s)
	}

	// A denial creates nothing.
	f.answer(r["id"].(string), "deny", "")
	f.waitingFor(done)
	if list := f.taskServices(""); len(list) != 0 {
		t.Fatalf("a denied create made %v", list)
	}

	// An approved update changes them; the request shows set and unset.
	id := f.commandIn(f.repoID, "web", "sleep 30", "")
	f.call("POST", "/api/update_service", map[string]any{"service_id": id, "service": map[string]any{
		"name": "web", "kind": "command", "command": map[string]any{"line": "sleep 30", "ports": []int{8080}}, "repository_id": f.repoID,
		"credentials": creds("old", "x"),
	}})
	done = agent.callAsync("update_service", map[string]any{
		"name": "web", "reason": "rotate",
		"changes": map[string]any{"credentials": map[string]any{"set": map[string]any{"new": credSecret}, "unset": []string{"old"}}},
	})
	r = f.awaitPending(1)[0]
	got = requestCredentials(r)
	if len(got) != 2 || got[0]["label"] != "old" || got[0]["change"] != "unset" || got[1]["label"] != "new" || got[1]["value"] != credSecret {
		t.Fatalf("update credentials = %v", got)
	}
	f.answer(r["id"].(string), "approve", "")
	out := f.waitingFor(done)
	if out.msg != "" {
		t.Fatalf("update refused: %s", out.msg)
	}
	if c := credentialsOf(f.service(id)); !reflect.DeepEqual(c, creds("new", credSecret)) {
		t.Fatalf("credentials after approval = %v", c)
	}
	b, _ := json.Marshal(out.out)
	if strings.Contains(string(b), credSecret) {
		t.Fatalf("the agent's answer holds the value: %s", b)
	}

	// The feed carried every request step with no value.
	var all []byte
	for len(feed) > 0 {
		e := <-feed
		b, _ := json.Marshal(e.Request)
		all = append(all, b...)
	}
	if strings.Contains(string(all), credSecret) {
		t.Fatalf("a feed request event holds a value: %s", all)
	}
}
