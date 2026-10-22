import { Codes } from "../../../../shared/domain/errors.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { mount, seededElement } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryReviews } from "../../infrastructure/memory-gateway.js";
import { makeReview, R0 } from "../../testing/fixtures.js";
import { reviewsInbox } from "./reviewsInbox.js";

file("reviews/presentation/reviewsInbox");

const at = (m) => new Date(R0.getTime() + m * 60_000);
const seed = {
  project_id: "p1",
  sessions: { d1: "go-developer · Server: architect channel" },
  tasks: { t1: { title: "Architect highlights", href: "/projects/p1/tasks/t1" }, t2: { title: "Write docs", href: "/projects/p1/tasks/t2" } },
  documents: { doc1: "Plan: Server" },
};

async function setup({ reviews = [makeReview()], ticketId = "t1", delivered = () => true, hash = () => "" } = {}) {
  const memory = memoryReviews({ reviews, now: () => at(5), delivered });
  const el = seededElement(seed);
  if (ticketId) el.dataset.ticketId = ticketId;
  el.dataset.architect = "";
  const mounted = mount(reviewsInbox({ gateway: memory.gateway, clock: fixedClock(at(5)), hash }), { el });
  mounted.instance.init();
  await flush();
  return { ...mounted, memory };
}

test("the task's band: its pending requests, named and linked, under how many wait", async () => {
  const { instance } = await setup({ reviews: [makeReview({ documentIds: ["doc1"], artifacts: [{ id: "artifact-123456", href: "/api/artifacts/artifact-123456/view/" }] })] });
  assert.equal(instance.headline, "1 review waits on you");
  const [card] = instance.pendingCards;
  assert.deepEqual([card.subject, card.byline, card.age], ["Spec set ready for sign-off", "from the architect · about go-developer · Server: architect channel", "5m"]);
  assert.deepEqual(card.documents, [{ id: "doc1", title: "Plan: Server", href: "/projects/p1/tasks/t1/documents/doc1" }]);
  assert.deepEqual(card.artifacts, [{ id: "artifact-123456", title: "Artifact artifact", href: "/api/artifacts/artifact-123456/view/" }]);
  assert.equal(instance.hasSettled, false);
  instance.destroy();
});

test("approving settles the request, announces it, and moves it under Earlier reviews", async () => {
  const { instance, memory } = await setup();
  await instance.approve("rv1");
  assert.equal(memory.find("rv1").state, "approved");
  assert.deepEqual([instance.hasPending, instance.settledLabel, instance.settledCards[0].stateWord], [false, "Earlier reviews (1)", "approved"]);
  assert.equal(instance.announcement, "You approved “Spec set ready for sign-off”");
  instance.destroy();
});

test("requesting changes without a note is stopped at the field; with one it is sent", async () => {
  const { instance, memory } = await setup();
  await instance.requestChanges("rv1");
  assert.equal(instance.drafts.rv1.problem, "Say what should change: the architect passes it on.");
  assert.deepEqual([instance.pendingCards[0].invalid, instance.pendingCards[0].noteId], ["true", "review-note-rv1"], "the field shows it");
  assert.equal(memory.find("rv1").state, "pending", "nothing was sent");

  instance.drafts.rv1.note = "  Rename the port.  ";
  await instance.requestChanges("rv1");
  assert.deepEqual([memory.find("rv1").state, memory.find("rv1").responseNote], ["changes_requested", "Rename the port."]);
  assert.equal(instance.drafts.rv1.problem, "");
  assert.equal(instance.settledCards[0].note, "Rename the port.");
  instance.destroy();
});

test("an answer the architect gets later says so", async () => {
  const { instance } = await setup({ delivered: () => false });
  await instance.approve("rv1");
  assert.equal(instance.settledCards[0].draft.notice, "Saved. The architect gets your answer when its current turn ends.");
  instance.destroy();
});

test("an answer to a request settled meanwhile shows the coded error and re-reads the list", async () => {
  const { instance, memory } = await setup();
  const { gateway } = memory;
  // Settled behind the band's back (another tab), with the feed not yet telling.
  await gateway.respond({ reviewId: "rv1", decision: "approved", note: "" });
  instance.reviews = [makeReview()];
  await instance.approve("rv1");
  assert.equal(instance.error.code, Codes.REVIEW_NOT_PENDING);
  await flush();
  assert.equal(instance.reviews[0].state, "approved", "the list now shows where it stands");
  instance.destroy();
});

test("requests arriving and withdrawn over the feed are announced; one withdrawn while the person writes stays until dismissed", async () => {
  const { instance, memory } = await setup({ reviews: [] });
  const raised = memory.request({ subject: "Look at the plan", createdAt: at(6) });
  await flush();
  assert.equal(instance.announcement, "The architect asks for your review: Look at the plan");
  assert.equal(instance.pendingCount, 1);

  instance.drafts[raised.id].note = "I would split";
  memory.withdraw(raised.id);
  await flush();
  assert.equal(instance.announcement, "The architect withdrew “Look at the plan”");
  assert.equal(instance.pendingCount, 0);
  assert.deepEqual(instance.pendingCards.map((c) => [c.id, c.withdrawn, c.draft.note]), [[raised.id, true, "I would split"]], "the note is kept on screen");
  instance.dismiss(raised.id);
  assert.deepEqual(instance.pendingCards, []);
  assert.equal(instance.settledCards[0].stateWord, "withdrawn");
  instance.destroy();
});

test("the person's own answer echoing on the feed is not news", async () => {
  const { instance, memory } = await setup();
  const gateway = memory.gateway;
  // The echo lands while the answer is in flight.
  instance.answeringId = "rv1";
  await gateway.respond({ reviewId: "rv1", decision: "approved", note: "" });
  await flush();
  assert.equal(instance.announcement, "");
  instance.destroy();
});

test("another task's requests stay off the band", async () => {
  const { instance, memory } = await setup();
  memory.request({ taskId: "t2", subject: "Not this task" });
  await flush();
  assert.deepEqual(instance.pendingCards.map((c) => c.id), ["rv1"]);
  instance.destroy();
});

test("the project's inbox: every pending request, grouped by task, nothing settled", async () => {
  const { instance } = await setup({
    ticketId: "",
    reviews: [
      makeReview({ id: "a", taskId: "t1", createdAt: at(1) }),
      makeReview({ id: "b", taskId: "t2", createdAt: at(3), subject: "Docs outline" }),
      makeReview({ id: "c", taskId: "t1", createdAt: at(2), state: "approved" }),
      makeReview({ id: "d", taskId: "t9", createdAt: at(0) }),
    ],
  });
  assert.equal(instance.forTask, false);
  assert.deepEqual(
    instance.groups.map((g) => [g.title, g.href, g.cards.map((c) => c.id)]),
    [["Write docs", "/projects/p1/tasks/t2", ["b"]], ["Architect highlights", "/projects/p1/tasks/t1", ["a"]], ["A task", "", ["d"]]],
  );
  assert.deepEqual(instance.settledCards, []);
  assert.equal(instance.headline, "3 reviews wait on you");
  instance.destroy();
});

test("empty once loaded", async () => {
  const { instance } = await setup({ reviews: [] });
  assert.equal(instance.isEmpty, true);
  instance.destroy();
});

test("the band keeps the server's paint until the list is read, then hides when there is nothing", async () => {
  const memory = memoryReviews({ reviews: [] });
  const el = seededElement(seed);
  el.dataset.ticketId = "t1";
  el.dataset.pending = "0";
  el.dataset.architect = "";
  const { instance } = mount(reviewsInbox({ gateway: memory.gateway, clock: fixedClock(at(5)) }), { el });
  instance.init();
  assert.deepEqual([instance.notLoaded, instance.hidesBand], [true, true], "painted hidden, stays hidden while loading");
  await flush();
  assert.deepEqual([instance.notLoaded, instance.hidesBand, instance.attentionAttr], [false, true, null]);
  memory.request({ subject: "Now one" });
  await flush();
  assert.deepEqual([instance.hidesBand, instance.attentionAttr], [false, ""]);
  instance.destroy();

  const painted = seededElement(seed);
  painted.dataset.ticketId = "t1";
  painted.dataset.pending = "2";
  const second = mount(reviewsInbox({ gateway: memoryReviews({ reviews: [] }).gateway, clock: fixedClock(at(5)) }), { el: painted }).instance;
  second.init();
  assert.equal(second.hidesBand, false, "painted with two waiting, it shows while it reads them");
  second.destroy();
});

test("only the newest request's Approve is the primary button", async () => {
  const { instance } = await setup({ reviews: [makeReview({ id: "a", createdAt: at(1) }), makeReview({ id: "b", createdAt: at(2) })] });
  assert.deepEqual(instance.pendingCards.map((c) => [c.id, c.approveVariant]), [["b", "primary"], ["a", null]]);
  instance.destroy();
});

test("a task that never had an architect reads nothing, yet shows a request that arrives", async () => {
  const memory = memoryReviews({ reviews: [makeReview()] });
  let reads = 0;
  const counting = { ...memory.gateway, listForTask: (...a) => (reads++, memory.gateway.listForTask(...a)) };
  const el = seededElement(seed);
  el.dataset.ticketId = "t1";
  el.dataset.pending = "0";
  const { instance, dispatched } = mount(reviewsInbox({ gateway: counting, clock: fixedClock(at(5)) }), { el });
  instance.init();
  await flush();
  assert.deepEqual([reads, instance.loaded, instance.hidesBand], [0, true, true]);
  memory.request({ subject: "First one" });
  await flush();
  assert.deepEqual(instance.pendingCards.map((c) => c.subject), ["First one"]);
  assert.deepEqual(dispatched.at(-1), { name: "announce", detail: { text: "The architect asks for your review: First one" } }, "told to the page's live region");
  instance.destroy();
});

const waiting = (n) => Array.from({ length: n }, (_, i) => makeReview({ id: `rv${i + 1}`, subject: `Review ${i + 1}`, createdAt: at(i) }));

test("the strip's button: how many wait, in the waiting tone, and the server's count until the list is read", async () => {
  const { instance } = await setup({ reviews: waiting(3) });
  assert.deepEqual([instance.summary.has, instance.summary.tone, instance.summary.count, instance.summary.phrase], [true, "attention", "3", "waiting"]);
  assert.equal(instance.summary.name, "Reviews, 3 waiting");
  instance.destroy();

  const el = seededElement(seed);
  el.dataset.ticketId = "t1";
  el.dataset.architect = "";
  el.dataset.pending = "2";
  const memory = memoryReviews({ reviews: [], now: () => at(5) });
  const painted = mount(reviewsInbox({ gateway: memory.gateway, clock: fixedClock(at(5)) }), { el });
  painted.instance.init(); // not read yet
  assert.deepEqual([painted.instance.summary.has, painted.instance.summary.tone, painted.instance.summary.count, painted.instance.summary.phrase], [true, "attention", "2", "waiting"]);
  painted.instance.destroy();
});

test("with nothing waiting the button counts the settled requests in a neutral tone, and is gone when there are none", async () => {
  const { instance } = await setup({ reviews: [makeReview({ state: "approved" })] });
  assert.deepEqual([instance.summary.has, instance.summary.tone, instance.summary.count, instance.summary.phrase], [true, null, "1", "settled"]);
  instance.destroy();
  const none = await setup({ reviews: [] });
  assert.equal(none.instance.summary.has, false);
  none.instance.destroy();
});

test("a request list that cannot be read says so on the button", async () => {
  const el = seededElement(seed);
  el.dataset.ticketId = "t1";
  el.dataset.architect = "";
  const gateway = { listForTask: async () => Promise.reject(new Error("down")), follow: () => () => {} };
  const { instance } = mount(reviewsInbox({ gateway, clock: fixedClock(at(5)) }), { el });
  instance.init();
  await flush();
  assert.deepEqual([instance.summary.has, instance.summary.count, instance.summary.phrase], [true, "", "couldn't load"]);
  assert.equal(instance.summary.name, "Reviews, couldn't load");
  instance.destroy();
});

test("the panel shows the first waiting request in full and folds the others behind a button", async () => {
  const { instance } = await setup({ reviews: waiting(3) });
  assert.deepEqual([instance.firstCards.length, instance.moreCards.length, instance.hasMore, instance.moreLabel], [1, 2, true, "2 more waiting"]);
  assert.equal(instance.firstCards[0].subject, "Review 3", "newest first");
  instance.toggleMore();
  assert.deepEqual([instance.moreOpen, instance.moreLabel], [true, "Hide 2"]);
  instance.toggleMore();
  assert.equal(instance.moreLabel, "2 more waiting");
  const one = await setup({ reviews: waiting(1) });
  assert.deepEqual([one.instance.hasMore, one.instance.moreCards.length], [false, 0]);
  instance.destroy();
  one.instance.destroy();
});

test("the panel says nothing waits when only settled requests are left", async () => {
  const { instance } = await setup({ reviews: [makeReview({ state: "approved" })] });
  assert.deepEqual([instance.nothingWaits, instance.hasSettled], [true, true]);
  const waits = await setup({ reviews: waiting(1) });
  assert.equal(waits.instance.nothingWaits, false);
  instance.destroy();
  waits.instance.destroy();
});

test("a link to a review that is folded unfolds it; a link to #reviews or to another page leaves the fold", async () => {
  const folded = await setup({ reviews: waiting(3), hash: () => "#review-rv1" });
  assert.equal(folded.instance.moreOpen, true);
  folded.instance.destroy();
  for (const hash of ["#reviews", "#review-rv3", "#comments", ""]) {
    const { instance } = await setup({ reviews: waiting(3), hash: () => hash });
    assert.equal(instance.moreOpen, false, hash);
    instance.destroy();
  }
});

test("each card carries the id a link to it targets", async () => {
  const { instance } = await setup({ reviews: waiting(2) });
  assert.deepEqual(instance.pendingCards.map((c) => c.anchor), ["review-rv2", "review-rv1"]);
  instance.destroy();
});

test("once the requests are read the strip is told whether there are any, settled ones included", async () => {
  const el = seededElement(seed);
  el.dataset.ticketId = "t1";
  el.dataset.architect = "";
  const memory = memoryReviews({ reviews: [makeReview({ state: "approved" })], now: () => at(5) });
  const { instance, dispatched } = mount(reviewsInbox({ gateway: memory.gateway, clock: fixedClock(at(5)) }), { el });
  instance.init();
  await flush();
  assert.deepEqual(dispatched.filter((d) => d.name === "reviews-read").map((d) => d.detail), [{ has: true }]);
  instance.destroy();
});
