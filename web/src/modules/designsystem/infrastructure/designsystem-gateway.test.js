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

test("the tab's switch applies the project's first design system when the page names none, or removes the one in use", async () => {
  const { gateway, calls } = gatewayAnswering((url) => systems(url) ?? jsonResponse(200, { repository: { id: "r1", design_system_id: JSON.parse(calls.at(-1).init.body).design_system_id, design_system_path: "ui/" } }));
  const on = await gateway.setRepository("r1", true, "ui/", "p1");
  assert.deepEqual([JSON.parse(calls[1].init.body).design_system_id, on.uses], ["ds1", true]);
  const off = await gateway.setRepository("r1", false, "ui/", "p1");
  assert.deepEqual([JSON.parse(calls[2].init.body).design_system_id, off.uses], [null, false]);
});

const full = { exists: true, version: 1, record: { tokens: [], principles: "", components: [] } };

test("the record is read, saved, renamed and listed by the design system's id, with no lookup", async () => {
  const { gateway, calls } = gatewayAnswering((url) => (url.endsWith("/save_design_system") || url.endsWith("/rename_design_token") ? jsonResponse(200, { version: 2 }) : url.endsWith("/list_design_system_versions") ? jsonResponse(200, { versions: [] }) : jsonResponse(200, full)));
  assert.equal((await gateway.get("ds1")).version, 1);
  assert.equal(await gateway.save("ds1", 1, { tokens: [] }, "m"), 2);
  assert.equal(await gateway.rename("ds1", 2, { group: "colors", name: "a" }, { group: "colors", name: "b" }), 2);
  await gateway.versions("ds1", { limit: 5 });
  await gateway.version("ds1", 1);
  assert.deepEqual(calls.map((c) => c.url), ["/api/get_design_system", "/api/save_design_system", "/api/rename_design_token", "/api/list_design_system_versions", "/api/get_design_system_version"]);
  for (const c of calls) assert.equal(JSON.parse(c.init.body).design_system_id, "ds1");
});

test("the identity of a design system is its name and description, and null once it is gone", async () => {
  const live = gatewayAnswering(() => jsonResponse(200, { ...full, name: "Brand", slug: "brand", description: "The brand site" }));
  assert.deepEqual(await live.gateway.identity("ds1"), { name: "Brand", description: "The brand site" });
  const gone = gatewayAnswering(() => jsonResponse(404, { error: "no such design system", code: "DESIGN_SYSTEM_NOT_FOUND" }));
  assert.equal(await gone.gateway.identity("ds1"), null);
  const down = gatewayAnswering(() => jsonResponse(500, { error: "boom", code: "INTERNAL" }));
  await assert.rejects(down.gateway.identity("ds1"), "INTERNAL");
});

test("the tab's repositories and switch use the design system the page names", async () => {
  const { gateway, calls } = gatewayAnswering((url) => (url.endsWith("/list_design_system_repositories") ? jsonResponse(200, { repositories: [] }) : jsonResponse(200, { repository: { id: "r1", design_system_id: "ds2", design_system_path: "ui/" } })));
  await gateway.repositories("p1", "ds2");
  await gateway.setRepository("r1", true, "ui/", "p1", false, "ds2");
  assert.deepEqual(calls.map((c) => c.url), ["/api/list_design_system_repositories", "/api/set_repository_design_system"]);
  assert.equal(JSON.parse(calls[0].init.body).design_system_id, "ds2");
  assert.equal(JSON.parse(calls[1].init.body).design_system_id, "ds2");
});

/** The feed is the project's; the gateway hands the page only what names its design system. */
function followed(designSystemId) {
  const handlers = [];
  const feedDouble = { follow: (_url, h) => (handlers.push(h), () => {}) };
  const gateway = designSystemGateway(apiClient({ base: "/api", fetch: fakeFetch(() => jsonResponse(200, {})).fetch }), feedDouble);
  const seen = { saved: [], changed: 0 };
  gateway.follow("p1", designSystemId, { onSaved: (s) => seen.saved.push(s), onChanged: () => seen.changed++ });
  return { send: (dto) => handlers[0].onMessage(dto), seen };
}

test("a save in another design system raises nothing; one in this one does", () => {
  const { send, seen } = followed("ds1");
  send({ design_system_saved: { project_id: "p1", design_system_id: "ds2", version: 9, source: "manual" } });
  assert.deepEqual(seen.saved, []);
  send({ design_system_saved: { project_id: "p1", design_system_id: "ds1", version: 4, source: "manual" } });
  assert.deepEqual(seen.saved, [{ version: 4, source: "manual" }]);
});

test("a change of the identity raises only for this design system", () => {
  const { send, seen } = followed("ds1");
  send({ design_system_changed: { project_id: "p1", design_system_id: "ds2" } });
  send({ design_system_changed: { project_id: "other", design_system_id: "ds1" } });
  assert.equal(seen.changed, 0);
  send({ design_system_changed: { project_id: "p1", design_system_id: "ds1" } });
  assert.equal(seen.changed, 1);
});

test("the project's design systems come as list rows with their repositories", async () => {
  const { gateway, calls } = gatewayAnswering(() =>
    jsonResponse(200, { design_systems: [{ design_system_id: "ds1", name: "App", slug: "app", description: "", version: 2, repositories: [{ repository_id: "r1", name: "spillway", export_state: "never_exported" }], open_proposals: 0 }] }),
  );
  const rows = await gateway.list("p1");
  assert.equal(calls[0].url, "/api/list_design_systems");
  assert.equal(calls[0].init.body, '{"project_id":"p1"}');
  assert.deepEqual(rows.map((r) => [r.id, r.slug, r.versionLabel, r.usedBy]), [["ds1", "app", "v2", "spillway"]]);
});

test("a design system is created from a name, a slug and a description, and answers its id", async () => {
  const { gateway, calls } = gatewayAnswering(() => jsonResponse(200, { design_system_id: "ds9" }));
  assert.equal(await gateway.create("p1", { name: "Marketing", slug: "marketing", description: "Campaigns" }), "ds9");
  assert.equal(calls[0].url, "/api/create_design_system");
  assert.deepEqual(JSON.parse(calls[0].init.body), { project_id: "p1", name: "Marketing", slug: "marketing", description: "Campaigns" });
});

test("a taken slug arrives as a coded error that names its field", async () => {
  const { gateway } = gatewayAnswering(() => jsonResponse(409, { error: "taken", code: "SLUG_TAKEN", details: { field: "slug" } }));
  const err = await gateway.create("p1", { name: "App", slug: "app", description: "" }).catch((e) => e);
  assert.deepEqual([err.code, err.details.field], ["SLUG_TAKEN", "slug"]);
});

test("a rename or a new description sends only what changes; the slug never goes", async () => {
  const { gateway, calls } = gatewayAnswering(() => jsonResponse(200, { design_system: {} }));
  await gateway.update("ds1", { name: "Apps" });
  await gateway.update("ds1", { description: "Product apps" });
  assert.deepEqual(calls.map((c) => JSON.parse(c.init.body)), [{ design_system_id: "ds1", name: "Apps" }, { design_system_id: "ds1", description: "Product apps" }]);
});

test("a design system is deleted with the name typed to confirm it", async () => {
  const { gateway, calls } = gatewayAnswering(() => jsonResponse(200, {}));
  await gateway.remove("ds1", "App");
  assert.equal(calls[0].url, "/api/delete_design_system");
  assert.deepEqual(JSON.parse(calls[0].init.body), { design_system_id: "ds1", confirm_name: "App" });
});

test("a design system in use is refused with the repositories that use it", async () => {
  const { gateway } = gatewayAnswering(() => jsonResponse(409, { error: "in use", code: "DESIGN_SYSTEM_IN_USE", details: { repositories: [{ repository_id: "r1", name: "spillway" }] } }));
  const err = await gateway.remove("ds1", "App").catch((e) => e);
  assert.equal(err.code, "DESIGN_SYSTEM_IN_USE");
  assert.deepEqual(err.details.repositories.map((r) => r.name), ["spillway"]);
});

test("the list follows the project's design system saves and changes, whichever design system", () => {
  const handlers = [];
  const gateway = designSystemGateway(apiClient({ base: "/api", fetch: fakeFetch(() => jsonResponse(200, {})).fetch }), { follow: (url, h) => (handlers.push([url, h]), () => {}) });
  let raised = 0;
  gateway.followProject("p1", { onChanged: () => raised++ });
  const [url, h] = handlers[0];
  assert.equal(url, "/events?project_id=p1");
  h.onMessage({ design_system_saved: { project_id: "other", design_system_id: "ds1", version: 1 } });
  h.onMessage({ design_system_changed: { project_id: "other", design_system_id: "ds1" } });
  h.onMessage({ session: {} });
  assert.equal(raised, 0);
  h.onMessage({ design_system_saved: { project_id: "p1", design_system_id: "ds1", version: 1 } });
  h.onMessage({ design_system_changed: { project_id: "p1", design_system_id: "ds2" } });
  assert.equal(raised, 2);
});
