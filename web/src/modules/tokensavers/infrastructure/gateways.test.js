// Both TokenSaverGateway implementations run the same contract.

import { apiClient } from "../../../shared/infrastructure/api.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { tokenSaverGatewayContract } from "../testing/gateway-contract.js";
import { makeWorld } from "../testing/fixtures.js";
import { stubTokenSaversApi } from "../testing/stub-api.js";
import { memoryTokenSavers } from "./memory-gateway.js";
import { tokenSaversGateway } from "./tokensavers-gateway.js";

file("tokensavers/infrastructure/gateways");

tokenSaverGatewayContract("memory", (world) => {
  const memory = memoryTokenSavers(world);
  return { gateway: memory.gateway, memory };
});

function http(world) {
  const stub = stubTokenSaversApi(world);
  return { gateway: tokenSaversGateway(apiClient({ base: "/api", fetch: stub.fetch })), memory: stub.memory, stub };
}

tokenSaverGatewayContract("http", http);

test("http · the paths and bodies are the server's", async () => {
  const { gateway, stub } = http(makeWorld());
  await gateway.setActivation("project", "p1", "caveman", "on");
  await gateway.listSavers();
  await gateway.installSaver("caveman");
  await gateway.listIndexes();
  await gateway.rebuildIndex("r1");
  await gateway.setCavemanIntensity("lite");
  assert.deepEqual(stub.requests, [
    ["POST", "/api/set_token_saver_activation", { level: "project", owner_id: "p1", saver: "caveman", value: "on" }],
    ["GET", "/api/list_token_savers", {}],
    ["POST", "/api/install_token_saver", { saver: "caveman" }],
    ["GET", "/api/list_repository_indexes", {}],
    ["POST", "/api/rebuild_repository_index", { repository_id: "r1" }],
    ["POST", "/api/update_settings", { settings: { "token_savers.caveman.intensity": "lite" } }],
  ]);
});
