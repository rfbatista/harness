import { StructuredError } from "../../../../shared/domain/errors.js";
import { FeedStatus } from "../../../../shared/domain/feed.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { fakeServices, pageEl, requestDTO } from "../../testing/fakeGateway.js";
import { serviceRequests } from "./serviceRequests.js";

file("services/presentation/components/serviceRequests");

const settle = async () => { for (let i = 0; i < 6; i++) await Promise.resolve(); };

async function open(requests = [requestDTO()]) {
  const gateway = fakeServices();
  gateway.requests = requests;
  const el = pageEl({ project_id: "p1" });
  const { instance } = mount(serviceRequests({ gateway }), { el });
  instance.init();
  await settle();
  return { requests: instance, gateway };
}

test("it reads the pending requests of the project once and counts them", async () => {
  const { requests, gateway } = await open([requestDTO(), requestDTO({ id: "q2" }), requestDTO({ id: "q3", state: "done" })]);
  assert.deepEqual(gateway.requestReads, [["p1", { state: "pending" }]]);
  assert.equal(requests.pendingCount, 2);
  assert.deepEqual([requests.shown, requests.countText, requests.badgeText, requests.badgeName], [true, "2", "Requests 2", "Requests, 2 waiting on you"]);
});

test("the badge is hidden when nothing waits", async () => {
  const { requests } = await open([]);
  assert.deepEqual([requests.shown, requests.countText], [false, "0"]);
});

test("it keeps no variable values from what it read to count", async () => {
  const { requests } = await open([requestDTO({ env: [{ name: "PORT", value: "8080", change: "set", previous_set: false }] })]);
  assert.equal(JSON.stringify(requests.requests).includes("8080"), false);
});

test("a request filed, answered or expired arrives on the feed and moves the count, in this tab or another", async () => {
  const { requests, gateway } = await open([]);
  gateway.emit({ seq: 3, kind: "service_request", request: requestDTO({ id: "q9" }) });
  assert.equal(requests.pendingCount, 1);
  gateway.emit({ seq: 4, kind: "service_request", request: requestDTO({ id: "q9", state: "done" }) });
  assert.equal(requests.pendingCount, 0);
  gateway.emit({ seq: 5, kind: "service_request", request: requestDTO({ id: "q8" }) });
  gateway.emit({ seq: 6, kind: "service_request", request: requestDTO({ id: "q8", state: "expired" }) });
  assert.equal(requests.pendingCount, 0);
});

test("a service event is not a request, and a request event never carries values into the list", async () => {
  const { requests, gateway } = await open([]);
  gateway.emit({ seq: 3, kind: "service_changed", service: { id: "s1", name: "api", kind: "command", status: "stopped" }, running_count: 0 });
  assert.equal(requests.requests.length, 0);
  gateway.emit({ seq: 4, kind: "service_request", request: requestDTO({ env: [{ name: "PORT", value: "8080", change: "set", previous_set: false }] }) });
  assert.equal(JSON.stringify(requests.requests).includes("8080"), false);
});

test("a resynced feed reads the list again; a live one does not", async () => {
  const { requests, gateway } = await open([]);
  gateway.requests = [requestDTO()];
  gateway.status(FeedStatus.LIVE);
  assert.equal(gateway.requestReads.length, 1);
  gateway.status(FeedStatus.RESYNCED);
  await settle();
  assert.equal(requests.pendingCount, 1);
});

test("a list that cannot be read hides the badge and says why; it is read again on the next resync", async () => {
  const gateway = fakeServices();
  gateway.requestsError = new StructuredError("NETWORK", "down");
  const { instance } = mount(serviceRequests({ gateway }), { el: pageEl({ project_id: "p1" }) });
  instance.init();
  await settle();
  assert.deepEqual([instance.shown, instance.error.code], [false, "NETWORK"]);
  gateway.requestsError = null;
  gateway.requests = [requestDTO()];
  gateway.status(FeedStatus.RESYNCED);
  await settle();
  assert.deepEqual([instance.shown, instance.error], [true, null]);
});
