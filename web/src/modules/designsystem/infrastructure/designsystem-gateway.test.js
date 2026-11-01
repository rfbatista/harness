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

const systems = (url) => (url.endsWith("/list_design_systems") ? jsonResponse(200, { design_systems: [{ design_system_id: "ds1", name: "App", created_at: "2026-01-01T00:00:00Z", version: 12 }, { design_system_id: "ds2", name: "Brand", created_at: "2026-02-01T00:00:00Z", version: 0 }] }) : null);

test("the project's repositories are asked about its first design system and come with the one each uses", async () => {
  const { gateway, calls } = gatewayAnswering(
    (url) =>
      systems(url) ??
      jsonResponse(200, {
        repositories: [
          { repository_id: "r1", name: "spillway", design_system: { design_system_id: "ds1", name: "App" }, applied_here: true, path: "ui/", export: null },
          { repository_id: "r2", name: "docs", design_system: { design_system_id: "ds2", name: "Brand" }, applied_here: false, path: ".harness/design-system/", export: null },
          { repository_id: "r3", name: "plain", design_system: null, applied_here: false, path: ".harness/design-system/", export: null },
        ],
      }),
  );
  const rows = await gateway.repositories("p1");
  assert.equal(calls[1].url, "/api/list_design_system_repositories");
  assert.equal(calls[1].init.body, '{"design_system_id":"ds1"}');
  assert.deepEqual(rows.map((r) => [r.id, r.uses, r.appliedId, r.appliedName, r.path]), [["r1", true, "ds1", "App", "ui/"], ["r2", false, "ds2", "Brand", ".harness/design-system/"], ["r3", false, "", "", ".harness/design-system/"]]);
});

test("a project without design systems has no repository rows", async () => {
  const { gateway } = gatewayAnswering(() => jsonResponse(200, { design_systems: [] }));
  assert.deepEqual(await gateway.repositories("p1"), []);
});

test("the design systems are listed with their name and version", async () => {
  const { gateway } = gatewayAnswering((url) => systems(url));
  assert.deepEqual(await gateway.designSystems("p1"), [{ id: "ds1", name: "App", version: 12 }, { id: "ds2", name: "Brand", version: 0 }]);
});

test("applying sends the repository, the design system, the path and the move, and answers the stored ones", async () => {
  const { gateway, calls } = gatewayAnswering(() => jsonResponse(200, { repository: { id: "r1", design_system_id: "ds2", design_system_path: "ui/" } }));
  const saved = await gateway.apply("r1", "ds2", "ui/", true);
  assert.equal(calls[0].url, "/api/set_repository_design_system");
  assert.deepEqual(JSON.parse(calls[0].init.body), { repository_id: "r1", design_system_id: "ds2", path: "ui/", confirm_move: true });
  assert.deepEqual([saved.designSystemId, saved.path], ["ds2", "ui/"]);
});

test("removing sends null, leaves an empty path out and answers no design system", async () => {
  const { gateway, calls } = gatewayAnswering(() => jsonResponse(200, { repository: { id: "r1", design_system_id: null, design_system_path: ".harness/design-system/" } }));
  const saved = await gateway.apply("r1", null, "");
  assert.deepEqual(JSON.parse(calls[0].init.body), { repository_id: "r1", design_system_id: null });
  assert.equal(saved.designSystemId, "");
});

test("a move the server wants confirmed arrives as an error naming the current design system", async () => {
  const { gateway } = gatewayAnswering(() => jsonResponse(409, { error: "uses App", code: "MOVE_NOT_CONFIRMED", details: { current: { design_system_id: "ds1", name: "App" } } }));
  await assert.rejects(gateway.apply("r1", "ds2", ""), "MOVE_NOT_CONFIRMED");
});

test("an answer without a repository is unreadable", async () => {
  const { gateway } = gatewayAnswering(() => jsonResponse(200, {}));
  await assert.rejects(gateway.apply("r1", "ds1", ""), "BAD_RESPONSE");
});

test("the tab's switch applies the project's first design system, or removes the one in use", async () => {
  const { gateway, calls } = gatewayAnswering((url) => systems(url) ?? jsonResponse(200, { repository: { id: "r1", design_system_id: JSON.parse(calls.at(-1).init.body).design_system_id, design_system_path: "ui/" } }));
  const on = await gateway.setRepository("r1", true, "ui/", "p1");
  assert.deepEqual([JSON.parse(calls[1].init.body).design_system_id, on.uses], ["ds1", true]);
  const off = await gateway.setRepository("r1", false, "ui/", "p1");
  assert.deepEqual([JSON.parse(calls[2].init.body).design_system_id, off.uses], [null, false]);
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
