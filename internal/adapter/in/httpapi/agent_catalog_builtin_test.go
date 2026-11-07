package httpapi

import (
	"reflect"
	"testing"
)

const servicesBuiltInMessage = "services is built into the harness and cannot be edited or deleted"

var servicesTools = []string{"list_services", "service_status", "start_service", "stop_service", "restart_service", "read_service_logs"}

func TestCatalogBuiltInServices_IsListedSynthesisedAndNeverWritten(t *testing.T) {
	f := catalogWithSkills(t)
	p := f.project("Widgets")
	before := f.read("mcp-servers.json")
	f.post("/api/set_project_mcp_servers", map[string]any{"project_id": p, "mcp_servers": []string{"services"}}, 200)
	f.post("/api/save_catalog_agent", map[string]any{"agent": map[string]any{
		"name": "alpha", "description": "First", "mcp_servers": []string{"nb", "services"},
	}}, 200)

	cat := f.get("/api/get_agent_catalog", 200)
	svc := byName(t, cat["mcp_servers"], "services")
	for k, want := range map[string]any{"built_in": true, "transport": "built_in", "command": "", "url": ""} {
		if svc[k] != want {
			t.Errorf("services %s = %v, want %v", k, svc[k], want)
		}
	}
	if len(list(svc["args"])) != 0 || svc["args"] == nil || len(svc["env"].(map[string]any)) != 0 || len(svc["headers"].(map[string]any)) != 0 {
		t.Errorf("services args/env/headers = %v %v %v", svc["args"], svc["env"], svc["headers"])
	}
	if got := strList(svc["tools"]); !reflect.DeepEqual(got, servicesTools) {
		t.Errorf("tools = %v", got)
	}
	if got := strList(svc["agents"]); !reflect.DeepEqual(got, []string{"alpha"}) {
		t.Errorf("agents = %v", got)
	}
	if projs := list(svc["projects"]); len(projs) != 1 || projs[0].(map[string]any)["id"] != p {
		t.Errorf("projects = %v", svc["projects"])
	}
	if len(problemCodes(svc)) != 0 || len(problemCodes(byName(t, cat["agents"], "alpha"))) != 0 {
		t.Errorf("problems on services or alpha: %v", svc["problems"])
	}
	nb := byName(t, cat["mcp_servers"], "nb")
	if nb["built_in"] != false {
		t.Errorf("nb built_in = %v", nb["built_in"])
	}
	if _, has := nb["tools"]; has {
		t.Errorf("nb has tools: %v", nb["tools"])
	}
	if f.read("mcp-servers.json") != before {
		t.Fatal("the catalog file was written")
	}
	if got := f.get("/api/get_project_mcp_servers?project_id="+p, 200); !reflect.DeepEqual(strList(got["mcp_servers"]), []string{"services"}) {
		t.Fatalf("project servers = %v", got)
	}
}

func TestCatalogBuiltInServices_EditingOrDeletingItIsRefused(t *testing.T) {
	f := catalogWithSkills(t)
	before := f.read("mcp-servers.json")
	cases := []struct {
		name, path string
		body       map[string]any
	}{
		{"create", "/api/save_catalog_mcp_server", map[string]any{"server": stdioServer("services")}},
		{"edit", "/api/save_catalog_mcp_server", map[string]any{"previous_name": "services", "server": stdioServer("services")}},
		{"rename from", "/api/save_catalog_mcp_server", map[string]any{"previous_name": "services", "server": stdioServer("svc")}},
		{"rename to", "/api/save_catalog_mcp_server", map[string]any{"previous_name": "nb", "server": stdioServer("services")}},
		{"delete", "/api/delete_catalog_mcp_server", map[string]any{"name": "services"}},
		{"test", "/api/test_catalog_mcp_server", map[string]any{"server": stdioServer("services")}},
	}
	for _, c := range cases {
		got := f.post(c.path, c.body, 409)
		if got["code"] != "MCP_SERVER_BUILT_IN" || got["error"] != servicesBuiltInMessage {
			t.Errorf("%s: body = %v", c.name, got)
		}
	}
	if f.read("mcp-servers.json") != before {
		t.Fatal("a refused change wrote the file")
	}
}

func TestCatalogBuiltInServices_AFileEntryIsAProblemAndTheBuiltInWins(t *testing.T) {
	f := catalogWithSkills(t)
	f.write("mcp-servers.json", `{"mcpServers":{"nb":{"command":"npx"},"services":{"command":"evil","args":["x"]}}}`)
	cat := f.get("/api/get_agent_catalog", 200)
	count := 0
	for _, s := range list(cat["mcp_servers"]) {
		if s.(map[string]any)["name"] == "services" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("services listed %d times", count)
	}
	svc := byName(t, cat["mcp_servers"], "services")
	if svc["built_in"] != true || svc["command"] != "" || len(list(svc["args"])) != 0 {
		t.Fatalf("the file entry won: %v", svc)
	}
	if got := problemCodes(svc); !reflect.DeepEqual(got, []string{"BUILT_IN_NAME"}) {
		t.Fatalf("problems = %v", got)
	}
}
