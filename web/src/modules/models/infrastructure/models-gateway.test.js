import { apiClient } from "../../../shared/infrastructure/api.js";
import { fakeFetch, jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { modelsGateway } from "./models-gateway.js";

file("models/infrastructure/models-gateway");

test("setModel writes the tier's setting and answers it as stored", async () => {
  const { fetch, calls } = fakeFetch((_url, init) => jsonResponse(200, { settings: JSON.parse(init.body).settings }));
  const gateway = modelsGateway(apiClient({ base: "/api", fetch }));
  assert.equal(await gateway.setModel("models.claude.implementation", "haiku"), "haiku");
  assert.equal(calls[0].url, "/api/update_settings");
  assert.deepEqual(JSON.parse(calls[0].init.body), { settings: { "models.claude.implementation": "haiku" } });
});

test("setModel refuses an answer without the setting", async () => {
  const { fetch } = fakeFetch(() => jsonResponse(200, { settings: {} }));
  const gateway = modelsGateway(apiClient({ base: "/api", fetch }));
  await assert.rejects(gateway.setModel("models.claude.architect", "opus"), "BAD_RESPONSE");
});
