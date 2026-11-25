import { test } from "node:test";
import assert from "node:assert/strict";
import { apiClient } from "../../../shared/infrastructure/api-client";
import { memoryFeed } from "../../../shared/infrastructure/memory-feed";
import { ProductOwnerError } from "../application/ports";
import { fakeProductOwnerServer } from "../testing/fake-server";
import { message, person } from "../testing/fixtures";
import { digestContract } from "../testing/digest-contract";
import { productOwnerContract } from "../testing/product-owner-contract";
import { eventWire, itemWire } from "../testing/wire";
import { eventsPath, httpProductOwner } from "./http-product-owner";
import { memoryProductOwner } from "./memory-product-owner";

/** The HTTP adapter over the wire; the memory server's events reach it through the memory feed, as the stream would. */
function http() {
  const server = memoryProductOwner();
  const fake = fakeProductOwnerServer(server);
  const feed = memoryFeed();
  const gateway = httpProductOwner(apiClient(fake.fetch), feed);
  // The stream: every event and status the memory server sends reaches the feed, project by project.
  const projects = new Set<string>();
  const bridge = (projectId: string) => {
    if (projects.has(projectId)) return;
    projects.add(projectId);
    server.follow(projectId, (e) => feed.emit(eventsPath(projectId), eventWire(projectId, e)), (s) => feed.setStatus(eventsPath(projectId), s));
  };
  return { server, gateway, fake, feed, events: { follow: (p: string, ...rest: Parameters<typeof gateway.follow> extends [string, ...infer R] ? R : never) => (bridge(p), gateway.follow(p, ...rest)) } };
}

productOwnerContract("HTTP adapter against a fake server on the wire", () => {
  const { server, gateway, events } = http();
  return { server, gateway, events };
});

productOwnerContract("in-memory adapter", () => {
  const server = memoryProductOwner();
  return { server, gateway: server, events: server };
});

digestContract("HTTP adapter against a fake server on the wire", () => {
  const { server, gateway, events } = http();
  return { server, gateway, events };
});

digestContract("in-memory adapter", () => {
  const server = memoryProductOwner();
  return { server, gateway: server, events: server };
});

test("the HTTP adapter fails as a ProductOwnerError when the network is down", async () => {
  const { gateway, fake } = http();
  fake.breakNetwork();
  await assert.rejects(gateway.settings("p1"), (err: unknown) => err instanceof ProductOwnerError && err.code === "NETWORK_ERROR");
});

test("the HTTP adapter reads an answer that does not match the contract as BAD_RESPONSE, naming the field", async () => {
  const { gateway, fake } = http();
  fake.answerWith({ project_id: "p1", enabled: "yes" });
  await assert.rejects(gateway.settings("p1"), (err: unknown) => err instanceof ProductOwnerError && err.code === "BAD_RESPONSE" && err.message.includes("enabled"));
});

test("the HTTP adapter reads a refusal code it does not know as UNKNOWN", async () => {
  const { gateway, server } = http();
  server.seedProject("p1", true);
  server.failWith(new ProductOwnerError("UNKNOWN", "boom"));
  await assert.rejects(gateway.settings("p1"), (err: unknown) => err instanceof ProductOwnerError && err.code === "UNKNOWN");
});

test("the HTTP adapter sends the contract's bodies", async () => {
  const { gateway, server, fake } = http();
  server.seedProject("p1", true);
  server.seedThread("p1", { threadId: "t1" });
  const started = await gateway.startThread("p1", "hello");
  await gateway.stopTurn(started.threadId);
  await gateway.renameThread("t1", "Name");
  await gateway.setEnabled("p1", false);
  await gateway.thread("t1", "i1").catch(() => {});
  assert.deepEqual(fake.requests.map((r) => [r.method, r.url, r.body]), [
    ["POST", "/api/start_product_owner_thread", { project_id: "p1", message: "hello" }],
    ["POST", "/api/stop_product_owner_turn", { thread_id: started.threadId }],
    ["POST", "/api/rename_product_owner_thread", { thread_id: "t1", title: "Name" }],
    ["POST", "/api/update_product_owner", { project_id: "p1", enabled: false }],
    ["GET", "/api/product_owner_thread?thread_id=t1&before=i1", undefined],
  ]);
});

test("the HTTP adapter follows the project's event feed, ignores what the feed carries that is not the product owner's, and drops a message it cannot read", () => {
  const { gateway, feed } = http();
  const seen: string[] = [];
  const stop = gateway.follow("p1", (e) => seen.push(e.type), () => {});
  const path = eventsPath("p1");
  assert.equal(path, "/events?project_id=p1");
  assert.equal(feed.followers(path), 1);
  feed.emit(path, "garbage");
  feed.emit(path, { session: { id: "s1" }, ticket: { id: "t1" } });
  feed.emit(path, { product_owner_delta: { project_id: "p1", thread_id: "t1" } });
  feed.emit(path, { product_owner_delta: { project_id: "p2", thread_id: "t9", item_id: "i9", text: "not mine" } });
  feed.emit(path, { product_owner_delta: { project_id: "p1", thread_id: "t1", item_id: "i1", text: "ok" } });
  assert.deepEqual(seen, ["delta"]);
  stop();
  assert.equal(feed.followers(path), 0);
});

test("a thread item of a kind the web UI does not know is a BAD_RESPONSE, not a crash", async () => {
  const { gateway, server, fake } = http();
  server.seedProject("p1", true);
  server.seedThread("p1", { threadId: "t1" }, [person("hi")]);
  fake.answerWith({ thread: { thread_id: "t1", project_id: "p1", title: "x", turn: "idle", session_id: "s" }, items: [{ ...(itemWire(message()) as object), kind: "hologram" }], has_earlier: false });
  await assert.rejects(gateway.thread("t1"), (err: unknown) => err instanceof ProductOwnerError && err.code === "BAD_RESPONSE" && err.message.includes("items[0].kind"));
});

test("the HTTP adapter reads a digest whose date is not YYYY-MM-DD as BAD_RESPONSE, naming the field", async () => {
  const { gateway, fake } = http();
  const bad = { digest_id: "d", date: "23/11/2026", period_start: "2026-11-23T00:00:00Z", period_end: "2026-11-23T09:00:00Z", summary: "s", items: [] };
  fake.answerWith({ digests: [bad], has_earlier: false });
  await assert.rejects(gateway.digests("p1"), (err: unknown) => err instanceof ProductOwnerError && err.code === "BAD_RESPONSE" && err.message.includes("date"));
});
