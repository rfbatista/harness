// Both ProjectGateway implementations run the same contract.

import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { fakeFetch, flush, jsonResponse } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { projectGatewayContract } from "../testing/gateway-contract.js";
import { stubProjectsApi } from "../testing/stub-api.js";
import { memoryProjects } from "./memory-gateway.js";
import { projectsGateway } from "./projects-gateway.js";

file("projects/infrastructure/gateways");

projectGatewayContract("memory", (world) => memoryProjects(world));

function http(world) {
  const stub = stubProjectsApi(world);
  const gateway = projectsGateway(apiClient({ base: "/api", fetch: stub.fetch }), feed({ base: "/api", EventSource: stub.EventSource }));
  return { gateway, stub };
}

projectGatewayContract("http", (world) => {
  const { gateway, stub } = http(world);
  return { gateway, repositories: stub.memory.repositories, endSession: stub.memory.endSession };
});

test("http · catalog messages it does not understand are skipped, the stream stays open", async () => {
  const { gateway, stub } = http({ dirs: ["/src"] });
  const changes = [];
  const stop = gateway.followCatalog((c) => changes.push(c));
  await flush();
  stub.pushRaw("not json");
  stub.pushRaw({ project: { id: "" } });
  stub.pushRaw({ ticket: { id: "t1" } });
  stub.pushRaw({ project: { id: "p9", name: "later", root_dir: "/src", color: "teal" } });
  await flush();
  assert.deepEqual(changes.map((c) => [c.kind, c.project?.name]), [["upsert", "later"]]);
  stop();
});

test("http · a delete succeeds on any 2xx, with or without a body", async () => {
  for (const answer of [() => new Response(null, { status: 204 }), () => jsonResponse(200, {})]) {
    const { fetch, calls } = fakeFetch(answer);
    await projectsGateway(apiClient({ base: "/api", fetch }), feed({ base: "/api" })).deleteProject("p1");
    assert.equal(calls[0].init.body, '{"project_id":"p1"}');
  }
});
