package httpapi

import (
	"reflect"
	"strings"
	"testing"

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
