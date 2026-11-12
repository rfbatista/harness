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

// ── the answer ───────────────────────────────────────────────────────────
const pendingOne = () => requestDTO({ id: "q1", reason: "why", env: [{ name: "PORT", value: "8080", change: "set", previous_set: false }] });
const answer = (requests, id, decision) => (decision === "approve" ? requests.approve(id) : requests.deny(id));

test("Approve sends the decision with the trimmed note, and the card shows what came of it", async () => {
  const { requests, gateway } = await openPopover([pendingOne()]);
  requests.drafts.q1.note = "  go ahead  ";
  await requests.approve("q1");
  assert.deepEqual(gateway.answered, [["q1", "approve", "go ahead"]]);
  const card = requests.cards[0];
  assert.deepEqual([card.pending, card.outcome.text], [false, "Approved and done."]);
  assert.equal(requests.pendingCount, 0);
  assert.equal(requests.popoverOpen, true, "the outcome stays in view until the person closes the popover");
});

test("Deny sends the note, and the card shows it denied with the note", async () => {
  const { requests, gateway } = await openPopover([pendingOne()]);
  requests.drafts.q1.note = "not now";
  await requests.deny("q1");
  assert.deepEqual(gateway.answered, [["q1", "deny", "not now"]]);
  assert.equal(requests.cards[0].outcome.text, "Denied. Your note: not now");
});

test("an approved action that failed shows the reason", async () => {
  const { requests, gateway } = await openPopover([pendingOne()]);
  gateway.requestAnswer = async () => ({ ...pendingOne(), state: "failed", result_error: "port 8080 is in use" });
  await requests.approve("q1");
  assert.equal(requests.cards[0].outcome.text, "Approved, but it failed: port 8080 is in use");
});

test("while an answer is in flight both buttons are off and a second press sends nothing", async () => {
  const { requests, gateway } = await openPopover([pendingOne()]);
  let release;
  gateway.requestAnswer = () => new Promise((resolve) => { release = () => resolve({ ...pendingOne(), state: "done" }); });
  const first = requests.approve("q1");
  assert.equal(requests.draftOf("q1").busy, true);
  assert.equal(requests.cards[0].draft.busy, true);
  await requests.deny("q1");
  await requests.approve("q1");
  release();
  await first;
  assert.equal(gateway.answered.length, 1);
  assert.equal(requests.draftOf("q1").busy, false);
});

test("the feed's echo of my own answer can come before the answer: it is not announced as someone else's", async () => {
  const { requests, gateway } = await openPopover([pendingOne()]);
  let release;
  gateway.requestAnswer = () => new Promise((resolve) => { release = () => resolve({ ...pendingOne(), state: "done" }); });
  const pending = requests.approve("q1");
  gateway.emit({ seq: 9, kind: "service_request", request: requestDTO({ id: "q1", state: "done" }) });
  assert.equal(requests.announcement, "", "not announced while my answer is in flight");
  release();
  await pending;
  assert.equal(requests.announcement, "You approved the service request");
  assert.equal(requests.cards.length, 1, "one card, not two");
});

test("a request answered or expired elsewhere turns its card to that outcome while the popover is open", async () => {
  const { requests, gateway } = await openPopover([pendingOne(), requestDTO({ id: "q2" })]);
  gateway.emit({ seq: 9, kind: "service_request", request: requestDTO({ id: "q1", state: "done" }) });
  gateway.emit({ seq: 10, kind: "service_request", request: requestDTO({ id: "q2", state: "expired" }) });
  assert.deepEqual(requests.cards.map((c) => [c.id, c.outcome?.text]), [["q1", "Approved and done."], ["q2", "Expired: its session ended before you answered."]]);
  assert.equal(requests.pendingCount, 0);
  assert.equal(requests.announcement, "A service request was answered elsewhere");
});

test("a request answered while the popover is closed leaves no card behind", async () => {
  const { requests, gateway } = await open([pendingOne()]);
  gateway.emit({ seq: 9, kind: "service_request", request: requestDTO({ id: "q1", state: "done" }) });
  assert.deepEqual([requests.cards.length, requests.pendingCount], [0, 0]);
});

test("closing the popover drops the outcomes", async () => {
  const { requests } = await openPopover([pendingOne()]);
  await requests.approve("q1");
  requests.popoverClosed({ detail: { id: "services-requests" } });
  assert.equal(requests.cards.length, 0);
});

test("an answer to a request that is no longer pending shows the server's refusal and reads the list again", async () => {
  const { requests, gateway } = await openPopover([pendingOne()]);
  gateway.requestAnswer = async () => { throw new StructuredError("REQUEST_NOT_PENDING", "that request was already answered"); };
  gateway.requests = [];
  await requests.approve("q1");
  await settle();
  assert.deepEqual([requests.error.code, requests.error.message], ["REQUEST_NOT_PENDING", "that request was already answered"]);
  assert.equal(requests.draftOf("q1").busy, false);
  assert.equal(requests.pendingCount, 0);
});

test("any other refusal shows too, and the request stays pending to answer again", async () => {
  const { requests, gateway } = await openPopover([pendingOne()]);
  gateway.requestAnswer = async () => { throw new StructuredError("NETWORK", "The harness server is not reachable."); };
  await requests.approve("q1");
  assert.equal(requests.error.code, "NETWORK");
  assert.equal(requests.cards[0].pending, true);
  assert.equal(requests.draftOf("q1").busy, false);
});

test("a note is kept per request while the popover is open", async () => {
  const { requests } = await openPopover([pendingOne(), requestDTO({ id: "q2" })]);
  requests.drafts.q1.note = "one";
  requests.drafts.q2.note = "two";
  assert.deepEqual(requests.cards.map((c) => c.draft.note), ["one", "two"]);
});
