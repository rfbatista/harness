import { apiClient } from "../../../shared/infrastructure/api.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { guardGateway } from "./guard-gateway.js";

file("agentsystem/infrastructure/guard-gateway");

function stub(answer, status = 200) {
  const calls = [];
  const fetch = async (url, init) => {
    calls.push({ url, method: init.method });
    return new Response(JSON.stringify(answer), { status });
  };
  return { gateway: guardGateway(apiClient({ base: "/api", fetch })), calls };
}

test("the Guard line of a repository is read from its agent-system view", async () => {
  const { gateway, calls } = stub({ effective: { missing_required_gates: ["lint"] }, values: {} });
  const line = await gateway.line("p 1", "r/1");
  assert.equal(calls[0].url, "/api/projects/p%201/repositories/r%2F1/agent-system");
  assert.equal(calls[0].method, "GET");
  assert.equal(line.state, "red");
});

test("a refused read reaches the caller", async () => {
  const { gateway } = stub({ error: "repository not found", code: "REPOSITORY_NOT_FOUND" }, 404);
  await assert.rejects(gateway.line("p1", "gone"));
});
