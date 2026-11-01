import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { fakeFetch, jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { designSystemGateway } from "./designsystem-gateway.js";

file("designsystem/infrastructure/designsystem-gateway");

function gatewayAnswering(answer) {
  const { fetch, calls } = fakeFetch(answer);
  const gateway = designSystemGateway(apiClient({ base: "/api", fetch }), feed({ base: "/api" }));
  return { gateway, calls };
}

test("the project's repositories come with their opt-in and path", async () => {
  const { gateway, calls } = gatewayAnswering(() =>
    jsonResponse(200, { repositories: [{ repository_id: "r1", name: "spillway", uses_design_system: true, path: "ui/", export: null }, { repository_id: "r2", name: "docs", uses_design_system: false, path: ".harness/design-system/", export: null }] }),
  );
  const rows = await gateway.repositories("p1");
  assert.equal(calls[0].url, "/api/list_design_system_repositories");
  assert.equal(calls[0].init.body, '{"project_id":"p1"}');
  assert.deepEqual(rows.map((r) => [r.id, r.name, r.uses, r.path]), [["r1", "spillway", true, "ui/"], ["r2", "docs", false, ".harness/design-system/"]]);
});

test("setting the opt-in sends the repository, the switch and the path, and answers the stored ones", async () => {
  const { gateway, calls } = gatewayAnswering(() => jsonResponse(200, { repository: { id: "r1", uses_design_system: true, design_system_path: "ui/" } }));
  const saved = await gateway.setRepository("r1", true, "ui/");
  assert.equal(calls[0].url, "/api/set_repository_design_system");
  assert.deepEqual(JSON.parse(calls[0].init.body), { repository_id: "r1", uses_design_system: true, path: "ui/" });
  assert.deepEqual([saved.uses, saved.path], [true, "ui/"]);
});

test("an empty path is left out, so the server keeps its own", async () => {
  const { gateway, calls } = gatewayAnswering(() => jsonResponse(200, { repository: { id: "r1", uses_design_system: false, design_system_path: ".harness/design-system/" } }));
  await gateway.setRepository("r1", false, "");
  assert.deepEqual(JSON.parse(calls[0].init.body), { repository_id: "r1", uses_design_system: false });
});

test("an answer without a repository is unreadable", async () => {
  const { gateway } = gatewayAnswering(() => jsonResponse(200, {}));
  await assert.rejects(gateway.setRepository("r1", true, ""), "BAD_RESPONSE");
});

const row = (id, created) => ({ design_system_id: id, name: id, created_at: created });

test("the record is read and saved on the project's earliest design system, by its id", async () => {
  const { gateway, calls } = gatewayAnswering((url) => {
    if (url.endsWith("/list_design_systems")) return jsonResponse(200, { design_systems: [row("b", "2026-02-01T00:00:00Z"), row("a", "2026-03-01T00:00:00Z"), row("c", "2026-01-01T00:00:00Z")] });
    if (url.endsWith("/save_design_system")) return jsonResponse(200, { version: 1 });
    return jsonResponse(200, { exists: true, version: 1, record: { tokens: [], principles: "", components: [] } });
  });
  assert.equal((await gateway.get("p1")).version, 1);
  assert.equal(await gateway.save("p1", 1, { tokens: [] }, "m"), 1);
  assert.deepEqual(calls.map((c) => c.url), ["/api/list_design_systems", "/api/get_design_system", "/api/save_design_system"]);
  assert.deepEqual(JSON.parse(calls[1].init.body), { design_system_id: "c" });
  assert.equal(JSON.parse(calls[2].init.body).design_system_id, "c");
});

test("a project without a design system reads as empty and the first save creates one", async () => {
  const { gateway, calls } = gatewayAnswering((url) => {
    if (url.endsWith("/list_design_systems")) return jsonResponse(200, { design_systems: [] });
    if (url.endsWith("/create_design_system")) return jsonResponse(200, { design_system_id: "n1" });
    return jsonResponse(200, { version: 1 });
  });
  const state = await gateway.get("p1");
  assert.equal(state.exists, false);
  assert.deepEqual(await gateway.versions("p1"), []);
  await gateway.save("p1", 0, { tokens: [] }, "");
  assert.deepEqual(calls.map((c) => c.url), ["/api/list_design_systems", "/api/list_design_systems", "/api/list_design_systems", "/api/create_design_system", "/api/save_design_system"]);
  assert.equal(JSON.parse(calls[4].init.body).design_system_id, "n1");
});
