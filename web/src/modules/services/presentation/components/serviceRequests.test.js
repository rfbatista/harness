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
  assert.deepEqual([requests.shown, requests.countText, requests.badgeName], [true, "2", "Requests, 2 waiting on you"]);
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

// ── the popover and its cards ────────────────────────────────────────────
const withValues = (o = {}) => requestDTO({
  id: "q1", action: "update", changes: [{ field: "command.line", before: "make dev", after: "npm run dev" }],
  env: [{ name: "PORT", value: "8080", change: "set", previous_set: true }, { name: "TOKEN", value: "s3cret", change: "set", previous_set: false }], ...o,
});
const toggle = (name, requestId = "q1") => ({ currentTarget: { dataset: { name, requestId } } });

async function openPopover(requests = [withValues()]) {
  const world = await open(requests);
  await world.requests.openPopover();
  await settle();
  return world;
}

test("opening the popover reads the pending requests again, for their values, and tells the page to show it", async () => {
  const { requests, gateway } = await openPopover();
  assert.equal(gateway.requestReads.length, 2);
  assert.deepEqual(gateway.requestReads[1], ["p1", { state: "pending" }]);
  assert.equal(requests.popoverOpen, true);
  assert.equal(requests.cards.length, 1);
});

test("a card's variables carry their values only once the popover read them, masked until revealed", async () => {
  const { requests } = await open([withValues()]);
  assert.equal(requests.cards[0].variables.every((v) => v.shown === "••••••••"), true);
  assert.equal(JSON.stringify(requests.cards).includes("s3cret"), false);
  await requests.openPopover();
  await settle();
  assert.equal(JSON.stringify(requests.cards).includes("s3cret"), false, "masked, so not in what the card binds");
  requests.toggle(toggle("TOKEN"));
  assert.deepEqual(requests.cards[0].variables.map((v) => [v.name, v.masked]), [["PORT", true], ["TOKEN", false]]);
  assert.equal(requests.cards[0].variables[1].shown, "s3cret");
});

test("Reveal all shows every value, and Hide all hides them", async () => {
  const { requests } = await openPopover();
  requests.toggleAll({ currentTarget: { dataset: { requestId: "q1" } } });
  assert.deepEqual(requests.cards[0].variables.map((v) => v.shown), ["8080", "s3cret"]);
  assert.equal(requests.cards[0].allLabel, "Hide all");
  requests.toggleAll({ currentTarget: { dataset: { requestId: "q1" } } });
  assert.equal(requests.cards[0].variables.every((v) => v.masked), true);
});

test("closing the popover drops the values and the reveal state", async () => {
  const { requests } = await openPopover();
  requests.toggleAll({ currentTarget: { dataset: { requestId: "q1" } } });
  requests.popoverClosed({ detail: { id: "services-requests" } });
  assert.equal(requests.popoverOpen, false);
  assert.equal(JSON.stringify(requests.requests).includes("s3cret"), false);
  assert.equal(JSON.stringify(requests.cards).includes("s3cret"), false);
  await requests.openPopover();
  await settle();
  assert.equal(requests.cards[0].variables.every((v) => v.masked), true, "reveal does not survive a close");
});

test("a request that arrives while the popover is open is read for its values; one that arrives while it is closed is not", async () => {
  const { requests, gateway } = await open([]);
  gateway.emit({ seq: 3, kind: "service_request", request: requestDTO({ id: "q5" }) });
  await settle();
  assert.equal(gateway.requestReads.length, 1, "closed: no read in the background");
  await requests.openPopover();
  await settle();
  const before = gateway.requestReads.length;
  gateway.requests = [withValues({ id: "q6" }), requestDTO({ id: "q5" })];
  gateway.emit({ seq: 4, kind: "service_request", request: requestDTO({ id: "q6" }) });
  await settle();
  assert.equal(gateway.requestReads.length, before + 1);
  assert.deepEqual(requests.cards.map((c) => c.id), ["q6", "q5"]);
});

test("a read that fails while opening shows the error and keeps the cards masked", async () => {
  const { requests, gateway } = await open([withValues()]);
  gateway.requestsError = new StructuredError("NETWORK", "down");
  await requests.openPopover();
  await settle();
  assert.equal(requests.error.code, "NETWORK");
  assert.equal(requests.cards.length, 1);
  gateway.requestsError = null;
  await requests.retry();
  assert.equal(requests.error, null);
});

test("each card names the action and service, the agent with a link to its session, and the reason", async () => {
  const { requests } = await openPopover([withValues({ reason: "the port clashes" })]);
  const card = requests.cards[0];
  assert.deepEqual([card.title, card.agentLine, card.reason, card.sessionHref], ["Change api", "go-developer · session abcdef12", "the port clashes", "/projects/p1/tasks/t1?session=abcdef12-x"]);
  assert.equal(requests.headline, "1 request waits on you");
});

test("values never reach the console or the browser's storage", async () => {
  const seen = [];
  const original = { log: console.log, error: console.error, warn: console.warn };
  console.log = console.error = console.warn = (...a) => seen.push(a);
  try {
    const { requests } = await openPopover();
    requests.toggleAll({ currentTarget: { dataset: { requestId: "q1" } } });
  } finally {
    Object.assign(console, original);
  }
  assert.deepEqual(seen, []);
});
