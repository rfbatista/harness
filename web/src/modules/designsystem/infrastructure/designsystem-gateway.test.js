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
