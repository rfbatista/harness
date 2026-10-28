// Both CatalogGateway implementations run the same contract.

import { apiClient } from "../../../shared/infrastructure/api.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { catalogGatewayContract } from "../testing/gateway-contract.js";
import { makeWorld } from "../testing/fixtures.js";
import { stubCatalogApi } from "../testing/stub-api.js";
import { catalogGateway } from "./catalog-gateway.js";
import { memoryCatalog } from "./memory-gateway.js";

file("agents/infrastructure/gateways");

catalogGatewayContract("memory", (world) => memoryCatalog(world));

function http(world) {
  const stub = stubCatalogApi(world);
  return { gateway: catalogGateway(apiClient({ base: "/api", fetch: stub.fetch })), server: stub.server, stub };
}

catalogGatewayContract("http", http);

test("http · every verb goes to the contract's path with its body, base_versions, force and dry_run", async () => {
  const { gateway, stub } = http(makeWorld());
  const versions = { "agents.json": "sha256:a1" };
  await gateway.getCatalog();
  await gateway.getSkill("tdd");
  await gateway.checkEnvVars(["A", "B"]);
  await gateway.saveAgent({ previousName: "backend", agent: { name: "backend", description: "d", aliases: ["be"], entries: ["tdd"], mcpServers: ["github"], allowedTools: ["Read"] } }, { baseVersions: versions, dryRun: true });
  await gateway.deleteBundle("ux", { baseVersions: versions, force: true });
  await gateway.testMcpServer({ name: "github", transport: "stdio", command: "npx", args: ["-y"], env: { T: "${T}" } });
  await gateway.setProjectMcpServers("p1", ["github"]);
  const saved = await gateway.saveAgent({ agent: { name: "new-one", description: "d", aliases: [], entries: [], mcpServers: [], allowedTools: [] } });
  await gateway.undoChange(saved.change.id);
  const calls = stub.requests.map(([method, path, body]) => [method, path, body]);
  assert.deepEqual(calls.slice(0, 7), [
    ["GET", "/api/get_agent_catalog", {}],
    ["GET", "/api/get_catalog_skill", { name: "tdd" }],
    ["GET", "/api/check_env_vars", { names: "A,B" }],
    ["POST", "/api/save_catalog_agent", { previous_name: "backend", agent: { name: "backend", description: "d", aliases: ["be"], entries: ["tdd"], mcp_servers: ["github"], allowed_tools: ["Read"] }, base_versions: versions, dry_run: true }],
    ["POST", "/api/delete_catalog_bundle", { name: "ux", base_versions: versions, force: true }],
    ["POST", "/api/test_catalog_mcp_server", { server: { name: "github", transport: "stdio", command: "npx", args: ["-y"], env: { T: "${T}" }, url: "", headers: {} } }],
    ["POST", "/api/set_project_mcp_servers", { project_id: "p1", mcp_servers: ["github"] }],
  ]);
  assert.deepEqual(calls[8], ["POST", "/api/undo_catalog_change", { change_id: saved.change.id }]);
});

test("http · a write sends nothing the caller did not ask for", async () => {
  const { gateway, stub } = http(makeWorld());
  await gateway.deleteAgent("backend");
  assert.deepEqual(stub.requests[0], ["POST", "/api/delete_catalog_agent", { name: "backend" }]);
});
