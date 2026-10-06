// Both ArtifactGateway implementations run the same contract.

import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { artifactGatewayContract } from "../testing/artifact-contract.js";
import { stubArtifactsApi } from "../testing/stub-artifacts-api.js";
import { artifactsGateway } from "./artifacts-gateway.js";
import { memoryArtifacts } from "./memory-artifacts.js";

file("sessions/infrastructure/artifacts");

artifactGatewayContract("memory", (world) => {
  const memory = memoryArtifacts(world);
  return { gateway: memory.gateway, publish: memory.publish, end: memory.end };
});

function http(world) {
  const stub = stubArtifactsApi(world);
  const gateway = artifactsGateway(apiClient({ base: "/api", fetch: stub.fetch }), feed({ base: "/api", EventSource: stub.EventSource }));
  return { gateway, stub };
}

artifactGatewayContract("http", (world) => {
  const { gateway, stub } = http(world);
  return { gateway, publish: stub.memory.publish, end: stub.memory.end };
});

test("http · a malformed artifact event is dropped, the stream stays open", async () => {
  const { gateway, stub } = http({ artifacts: [] });
  const events = [];
  const close = gateway.follow("s1", (e) => events.push(e), () => {});
  await flush();
  stub.pushRaw("s1", { seq: 9, session_id: "s1", type: "artifact", artifact: { id: "", kind: "page" } });
  stub.pushRaw("s1", "not even json");
  stub.memory.publish({ sessionId: "s1", kind: "page", title: "After", note: "", path: "a.html", mime: "text/html", sizeBytes: 1 });
  await flush();
  assert.deepEqual(events.map((e) => e.artifact?.title), ["After"]);
  close();
});

test("http · list asks for the session's artifacts by session_id", async () => {
  const { gateway, stub } = http({ artifacts: [] });
  stub.memory.publish({ sessionId: "s/1", kind: "file", title: "", note: "", path: "out.bin", mime: "application/octet-stream", sizeBytes: 5 });
  const [a] = await gateway.list("s/1");
  assert.equal(a.path, "out.bin");
  assert.equal(a.src, "/api/artifacts/art-1/view/");
});
