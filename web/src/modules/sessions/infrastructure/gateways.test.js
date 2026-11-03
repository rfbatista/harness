// Both SessionGateway implementations run the same contract.

import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { fakeFetch, jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { makeSession } from "../testing/fixtures.js";
import { sessionGatewayContract } from "../testing/gateway-contract.js";
import { stubApi } from "../testing/stub-api.js";
import { memoryGateway } from "./memory-gateway.js";
import { sessionsGateway } from "./sessions-gateway.js";

file("sessions/infrastructure/gateways");

sessionGatewayContract(
  "memory",
  (world) => {
    const memory = memoryGateway(world);
    return { gateway: memory.gateway, upsert: memory.upsert };
  },
  makeSession,
);

sessionGatewayContract(
  "http",
  (world) => {
    const stub = stubApi(world);
    const gateway = sessionsGateway(
      apiClient({ base: "/api", fetch: stub.fetch }),
      feed({ base: "/api", EventSource: stub.EventSource }),
    );
    return { gateway, upsert: stub.memory.upsert };
  },
  makeSession,
);

test("http · the picker asks for a repository's agents and reads what is not offered there and why", async () => {
  const { fetch, calls } = fakeFetch((url) => {
    if (url.startsWith("/api/list_agents?project_id=p1&repository_id=r1")) {
      return jsonResponse(200, {
        agents: [{ name: "backend", description: "Go.", repository_prompt_set: true }, { name: "ux", description: "" }],
        not_offered: [{ name: "reviewer", reason: "unassigned" }, { name: "qa", reason: "not_enabled" }, { name: "" }],
      });
    }
    if (url.startsWith("/api/list_agents?project_id=p1")) return jsonResponse(200, { agents: [{ name: "backend" }, { name: "ux" }] });
    if (url === "/api/list_agents") return jsonResponse(200, { agents: [{ name: "backend" }, { name: "ux" }, { name: "reviewer" }, { name: "qa" }, { name: "ops" }] });
    return jsonResponse(200, { mode: "selected", agents: ["backend", "ux"] });
  });
  const gateway = sessionsGateway(apiClient({ base: "/api", fetch }), feed({ base: "/api" }));
  const choice = await gateway.agentChoices("p1", "r1");
  assert.deepEqual(choice.agents.map((a) => [a.name, a.repositoryPromptSet]), [["backend", true], ["ux", false]]);
  assert.deepEqual(choice.notOffered, [{ name: "reviewer", reason: "unassigned" }, { name: "qa", reason: "not_enabled" }]);
  assert.equal(choice.total, 5);
  assert.ok(calls.some((c) => c.url.includes("repository_id=r1")), "the repository is sent");
  const project = await gateway.agentChoices("p1");
  assert.deepEqual(project.notOffered, [], "without a repository nothing is listed as missing");
});
