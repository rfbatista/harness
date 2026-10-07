// Both ArtifactGateway implementations run the same contract.

import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { artifactGatewayContract } from "../testing/artifact-contract.js";
import { makeArtifact } from "../testing/artifact-fixtures.js";
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

test("http · a malformed artifact change on the project feed is dropped, the stream stays open", async () => {
  const { gateway, stub } = http({ artifacts: [makeArtifact({ id: "logo", scope: "project" })], tickets: [{ id: "t2", projectId: "p1" }] });
  const changes = [];
  const close = gateway.followProject("p1", (c) => changes.push(c), () => {});
  await flush();
  stub.pushProject("p1", { artifact: { id: "", kind: "page" } });
  stub.pushProject("p1", { artifact: {}, deleted: true });
  stub.pushProject("p1", "not even json");
  await gateway.attach("logo", "t2");
  await flush();
  assert.deepEqual(changes.map((c) => [c.kind, c.artifact.id]), [["changed", "logo"]]);
  close();
});

test("http · listTask asks by ticket_id; attach and detach post the ids", async () => {
  const { gateway, stub } = http({ artifacts: [makeArtifact({ id: "logo", scope: "project" })], tickets: [{ id: "t 2", projectId: "p1" }] });
  const calls = [];
  const fetch = stub.fetch;
  const spy = artifactsGateway(
    apiClient({ base: "/api", fetch: (input, init) => (calls.push([init?.method ?? "GET", String(input), init?.body ?? ""]), fetch(input, init)) }),
    feed({ base: "/api", EventSource: stub.EventSource }),
  );
  await spy.attach("logo", "t 2");
  assert.deepEqual((await spy.listTask("t 2")).map((a) => a.id), ["logo"]);
  await spy.detach("logo", "t 2");
  assert.deepEqual(
    calls.map(([m, url, body]) => [m, url.replace(/^.*\/api/, "/api"), body && JSON.parse(body)]),
    [
      ["POST", "/api/attach_artifact_to_ticket", { artifact_id: "logo", ticket_id: "t 2" }],
      ["GET", "/api/artifacts?ticket_id=t+2", ""],
      ["POST", "/api/detach_artifact_from_ticket", { artifact_id: "logo", ticket_id: "t 2" }],
    ],
  );
  assert.deepEqual(await gateway.listTask("t 2"), []);
});
