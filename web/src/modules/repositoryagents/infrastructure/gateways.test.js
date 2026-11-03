// Both RepositoryAgentsGateway implementations run the same contract.

import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { repositoryAgentsGatewayContract } from "../testing/gateway-contract.js";
import { stubRepositoryAgentsApi } from "../testing/stub-api.js";
import { memoryRepositoryAgents } from "./memory-gateway.js";
import { repositoryAgentsGateway } from "./repository-agents-gateway.js";

file("repositoryagents/infrastructure/gateways");

repositoryAgentsGatewayContract("memory", (world) => memoryRepositoryAgents(world));

function http(world) {
  const stub = stubRepositoryAgentsApi(world);
  const gateway = repositoryAgentsGateway(apiClient({ base: "/api", fetch: stub.fetch }), feed({ base: "/api", EventSource: stub.EventSource }));
  return { gateway, stub };
}

repositoryAgentsGatewayContract("http", (world) => http(world));

test("http · feed messages it does not understand are skipped, the stream stays open", async () => {
  const { gateway, stub } = http({ catalog: [], projects: [], repositories: [] });
  const changes = [];
  const stop = gateway.followChanges((c) => changes.push(c));
  await flush();
  stub.pushRaw("not json");
  stub.pushRaw({ agent_catalog_changed: { paths: [] } });
  stub.pushRaw({ repository_agents_changed: { project_id: "p1" } });
  stub.pushRaw({ repository_agents_changed: { project_id: "p1", repository_id: "r9", agents: ["x", 3] } });
  await flush();
  assert.deepEqual(changes.map((c) => [c.repositoryId, c.agents]), [["r9", ["x"]]]);
  stop();
});

test("http · AGENT_NOT_ASSIGNED carries the agents that are assigned, in camelCase", async () => {
  const fetch = async () =>
    new Response(JSON.stringify({ error: "AGENT_NOT_ASSIGNED: x", code: "AGENT_NOT_ASSIGNED", agent: "x", repository_id: "r1", repository_name: "atlas-api", assigned: ["a", "b"] }), {
      status: 409,
      headers: { "Content-Type": "application/json" },
    });
  const gateway = repositoryAgentsGateway(apiClient({ base: "/api", fetch }), feed({ base: "/api" }));
  const err = await assert.rejects(gateway.getRepositoryAgents("r1"), "AGENT_NOT_ASSIGNED");
  assert.deepEqual(err.details, { agent: "x", repositoryId: "r1", repositoryName: "atlas-api", assigned: ["a", "b"] });
});
