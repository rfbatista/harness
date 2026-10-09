// Both ResourceGateway implementations run the same contract.

import { apiClient } from "../../../shared/infrastructure/api.js";
import { feed } from "../../../shared/infrastructure/feed.js";
import { xhrUpload } from "../../../shared/infrastructure/upload.js";
import { flush } from "../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../shared/testing/test.js";
import { resourceGatewayContract } from "../testing/resource-contract.js";
import { makeResource } from "../testing/resource-fixtures.js";
import { stubResourcesApi } from "../testing/stub-resources-api.js";
import { memoryResources } from "./memory-resources.js";
import { resourcesGateway } from "./resources-gateway.js";

file("tasks/infrastructure/resources");

resourceGatewayContract("memory", (world) => {
  const memory = memoryResources(world);
  return { gateway: memory.gateway, deleteTask: memory.deleteTask, sessions: memory };
});

function http(world) {
  const stub = stubResourcesApi(world);
  const gateway = resourcesGateway(
    apiClient({ base: "/api", fetch: stub.fetch }),
    feed({ base: "/api", EventSource: stub.EventSource }),
    xhrUpload({ base: "/api", XMLHttpRequest: stub.XMLHttpRequest }),
  );
  return { gateway, stub };
}

resourceGatewayContract("http", (world) => {
  const { gateway, stub } = http(world);
  return { gateway, deleteTask: stub.memory.deleteTask, sessions: stub.memory };
});

test("http · the upload sends ticket_id and title before the file, and no blank title", async () => {
  const sent = [];
  const upload = async (path, parts) => {
    sent.push([path, parts.map(([name]) => name)]);
    return { resource: { id: "r1", kind: "file", title: "a.txt", content_url: "/api/ticket_resources/r1/content", updated_at: "2026-10-08T12:00:00Z" } };
  };
  const gateway = resourcesGateway(apiClient({ base: "/api", fetch: async () => new Response("{}") }), feed({ base: "/api", EventSource: class {} }), upload);
  const f = new File(["x"], "a.txt");
  await gateway.upload({ ticketId: "t1", file: f, title: "Notes" });
  await gateway.upload({ ticketId: "t1", file: f, title: "  " });
  assert.deepEqual(sent, [
    ["/upload_ticket_resource", ["ticket_id", "title", "file"]],
    ["/upload_ticket_resource", ["ticket_id", "file"]],
  ]);
});

test("http · a malformed change on the project feed is dropped, the stream stays open", async () => {
  const { gateway, stub } = http({ tickets: [{ id: "t1", projectId: "p1" }], resources: [makeResource({ id: "r1" })] });
  const changes = [];
  const stop = gateway.follow("p1", (c) => changes.push(c), () => {});
  await flush();
  stub.pushProject("p1", { ticket_resource: { id: "", kind: "image" } });
  stub.pushProject("p1", { ticket_comment: {}, deleted: true });
  stub.pushProject("p1", "not even json");
  stub.pushProject("p1", { session: { status: "running" } }); // not ours to judge: ignored, not an error
  stub.pushProject("p1", { session: { id: "s-x", ticket_id: "t1", status: "running", agent_id: "a1" } });
  await gateway.retitle("r1", "After");
  await flush();
  assert.deepEqual(changes.map((c) => c.resource?.title ?? c.kind), ["session", "After"]);
  assert.deepEqual([changes[0].id, changes[0].ticketId], ["s-x", "t1"]);
  stop();
});
