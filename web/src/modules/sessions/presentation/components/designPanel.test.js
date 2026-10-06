import { FeedStatus } from "../../../../shared/domain/feed.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { flush } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryArtifacts } from "../../infrastructure/memory-artifacts.js";
import { A0, makeArtifact } from "../../testing/artifact-fixtures.js";
import { designPanel, FRESH_MS } from "./designPanel.js";

file("sessions/presentation/designPanel");

const publishInput = (overrides = {}) => ({ sessionId: "s1", kind: "page", title: "Hero", note: "first", path: "hero.html", mime: "text/html", sizeBytes: 10, ...overrides });

async function setup({ artifacts = [], live = true, sessionId = "s1" } = {}) {
  const memory = memoryArtifacts({ artifacts, now: () => A0 });
  const timers = [];
  const el = document.createElement("section");
  const mounted = mount(
    () => designPanel({ artifacts: memory.gateway, clock: fixedClock(A0), setTimeout: (fn, ms) => timers.push({ fn, ms }) })({ key: sessionId, sessionId, live }),
    { el },
  );
  mounted.instance.init();
  await flush();
  return { ...mounted, memory, timers };
}

test("loads the session's artifacts newest first and selects the newest", async () => {
  const older = makeArtifact({ id: "old", sessionId: "s1", updatedAt: new Date(A0.getTime() - 60_000) });
  const newer = makeArtifact({ id: "new", sessionId: "s1", title: "Newest", updatedAt: A0 });
  const elsewhere = makeArtifact({ id: "x", sessionId: "s2" });
  const { instance } = await setup({ artifacts: [older, newer, elsewhere] });
  assert.equal(instance.ready, true);
  assert.deepEqual(instance.cards.map((c) => c.id), ["new", "old"]);
  assert.equal(instance.current.id, "new");
  assert.deepEqual(instance.frames.map((f) => f.key), ["new@1"]);
  assert.equal(instance.currentOpenHref, "/api/artifacts/new/view/");
  assert.equal(instance.hasOpenHref, true);
  assert.equal(instance.isEmpty, false);
  instance.destroy();
});

test("with nothing published it is empty, ready, and live", async () => {
  const { instance } = await setup();
  assert.equal(instance.isEmpty, true);
  assert.equal(instance.current, null);
  assert.deepEqual(instance.frames, []);
  assert.equal(instance.hasOpenHref, false);
  assert.deepEqual([instance.feedState, instance.feedWord], ["live", "live"]);
  instance.destroy();
});

test("a publish over the stream adds a card, selects it, marks it fresh, and tells the page", async () => {
  const { instance, memory, dispatched, timers } = await setup();
  memory.publish(publishInput());
  await flush();
  assert.deepEqual(instance.cards.map((c) => [c.title, c.fresh]), [["Hero", true]]);
  assert.equal(instance.current.title, "Hero");
  const event = dispatched.find((d) => d.name === "artifact-published");
  assert.deepEqual([event.detail.artifact.title, event.detail.isNew], ["Hero", true]);
  assert.equal(timers[0].ms, FRESH_MS);
  timers[0].fn();
  assert.equal(instance.cards[0].fresh, false);
  instance.destroy();
});

test("a re-publish refreshes the card in place and remounts the preview with the new revision", async () => {
  const { instance, memory, dispatched } = await setup();
  const first = memory.publish(publishInput());
  await flush();
  memory.publish(publishInput({ note: "tighter" }));
  await flush();
  assert.equal(instance.cards.length, 1, "one card per artifact");
  assert.deepEqual([instance.cards[0].revision, instance.cards[0].note], ["rev 2", "tighter"]);
  assert.deepEqual(instance.frames.map((f) => f.key), [`${first.id}@2`]);
  assert.ok(instance.frames[0].src.endsWith("?rev=2"));
  const last = dispatched.filter((d) => d.name === "artifact-published").at(-1);
  assert.equal(last.detail.isNew, false);
  instance.destroy();
});

test("a new artifact is selected automatically only until the developer picks one by hand", async () => {
  const { instance, memory } = await setup();
  const a = memory.publish(publishInput({ path: "a.html", title: "A" }));
  memory.publish(publishInput({ path: "b.html", title: "B" }));
  await flush();
  assert.equal(instance.current.title, "B", "the latest publish is shown while nothing was chosen");
  instance.select(a.id);
  memory.publish(publishInput({ path: "c.html", title: "C" }));
  await flush();
  assert.equal(instance.current.title, "A", "a hand-picked artifact stays in front");
  memory.publish(publishInput({ path: "a.html", title: "A", note: "again" }));
  await flush();
  assert.deepEqual(instance.frames.map((f) => f.key), [`${a.id}@2`], "a re-publish of the chosen one re-renders it");
  instance.destroy();
});

test("J and K walk the cards in on-screen order", async () => {
  const { instance, memory } = await setup();
  memory.publish(publishInput({ path: "a.html", title: "A" }));
  memory.publish(publishInput({ path: "b.html", title: "B" }));
  await flush();
  assert.equal(instance.current.title, "B");
  instance.next();
  assert.equal(instance.current.title, "A");
  instance.next();
  assert.equal(instance.current.title, "A", "stops at the end");
  instance.previous();
  assert.equal(instance.current.title, "B");
  instance.destroy();
});

test("an off-machine url is listed but not embedded", async () => {
  const { instance, memory } = await setup();
  memory.publish(publishInput({ kind: "url", path: "", url: "http://example.com/", title: "Elsewhere", mime: "", sizeBytes: 0 }));
  await flush();
  assert.equal(instance.current.embed, false);
  assert.equal(instance.current.notEmbeddable, true);
  assert.equal(instance.current.src, "");
  assert.equal(instance.current.url, "http://example.com/", "the url is shown as text even when not embedded");
  assert.equal(instance.currentKind, "url");
  assert.equal(instance.hasOpenHref, false);
  instance.destroy();
});

test("an ended session loads its list but opens no stream; a done event ends an open one", async () => {
  const published = makeArtifact({ id: "a", sessionId: "s1" });
  const over = await setup({ artifacts: [published], live: false });
  assert.deepEqual(over.instance.cards.map((c) => c.id), ["a"]);
  assert.deepEqual([over.instance.feedState, over.instance.feedWord], ["done", "session ended"]);
  over.memory.publish(publishInput({ path: "late.html" }));
  await flush();
  assert.equal(over.instance.cards.length, 1, "nothing is followed");
  over.instance.destroy();

  const open = await setup();
  open.memory.end("s1");
  await flush();
  assert.deepEqual([open.instance.feedState, open.instance.feedWord], ["done", "session ended"]);
  open.memory.publish(publishInput({ path: "late.html" }));
  await flush();
  assert.equal(open.instance.cards.length, 0, "unfollowed after done");
  open.instance.destroy();
});

test("stream status shows in the bar; a resync reloads the list", async () => {
  const { instance, memory } = await setup();
  memory.feedStatus(FeedStatus.PAUSED);
  assert.deepEqual([instance.feedState, instance.feedWord], ["reconnecting", "live updates paused · retrying"]);
  memory.publish(publishInput({ path: "missed.html", title: "Missed" }));
  instance.artifacts = []; // pretend the publish was missed while paused
  memory.feedStatus(FeedStatus.RESYNCED);
  await flush();
  assert.deepEqual(instance.cards.map((c) => c.title), ["Missed"]);
  assert.equal(instance.feedState, "live");
  instance.destroy();
});

test("a failed load shows the coded error and stays empty", async () => {
  const memory = memoryArtifacts();
  memory.gateway.list = async () => {
    throw Object.assign(new Error("boom"), { code: "NETWORK" });
  };
  const { instance } = mount(() => designPanel({ artifacts: memory.gateway, clock: fixedClock(A0) })({ key: "s1", sessionId: "s1", live: true }));
  instance.init();
  await flush();
  assert.equal(instance.ready, true);
  assert.ok(instance.error, "an error is shown");
  instance.dismissError();
  assert.equal(instance.error, null);
  instance.destroy();
});

test("loadFrame fills the frame beside it with the revision's src and title; the sandbox stays as the markup set it", async () => {
  const { instance } = await setup();
  const host = document.createElement("div");
  const iframe = document.createElement("iframe");
  iframe.setAttribute("sandbox", "allow-scripts");
  host.append(iframe);
  instance.loadFrame(host, { src: "/api/artifacts/a1/view/?rev=2", frameTitle: "Hero, revision 2" });
  assert.equal(iframe.getAttribute("src"), "/api/artifacts/a1/view/?rev=2");
  assert.equal(iframe.title, "Hero, revision 2");
  assert.equal(iframe.getAttribute("sandbox"), "allow-scripts", "never loosened");
  instance.destroy();
});

test("a replayed or stale revision changes nothing: no card, no highlight, no announcement", async () => {
  const { instance, memory, dispatched } = await setup();
  memory.publish(publishInput());
  const second = memory.publish(publishInput({ note: "tighter" }));
  await flush();
  const before = dispatched.length;
  instance.freshIds = [];
  instance.onEvent({ kind: "published", artifact: makeArtifact({ id: second.id, sessionId: "s1", revision: 1, note: "first" }) });
  instance.onEvent({ kind: "published", artifact: makeArtifact({ id: second.id, sessionId: "s1", revision: 2, note: "tighter" }) });
  assert.equal(dispatched.length, before, "nothing announced");
  assert.deepEqual(instance.cards.map((c) => [c.revision, c.note, c.fresh]), [["rev 2", "tighter", false]]);
  assert.deepEqual(instance.frames.map((f) => f.key), [`${second.id}@2`]);
  instance.destroy();
});

test("destroyed while loading, it never opens the stream", async () => {
  const memory = memoryArtifacts({ now: () => A0 });
  let finish;
  memory.gateway.list = () => new Promise((resolve) => (finish = resolve));
  const { instance, dispatched } = mount(() => designPanel({ artifacts: memory.gateway, clock: fixedClock(A0) })({ key: "s1", sessionId: "s1", live: true }));
  const started = instance.init();
  instance.destroy();
  finish([]);
  await started;
  memory.publish(publishInput());
  await flush();
  assert.equal(instance.cards.length, 0, "nothing followed after destroy");
  assert.equal(dispatched.length, 0);
});

test("the page saying the session ended stops the follow, as a done event would", async () => {
  const { instance, memory } = await setup();
  instance.sessionEnded({ detail: { id: "other" } });
  assert.equal(instance.feedState, "live", "another session's end is not this one's");
  instance.sessionEnded({ detail: { id: "s1" } });
  assert.deepEqual([instance.feedState, instance.feedWord], ["done", "session ended"]);
  memory.publish(publishInput());
  await flush();
  assert.equal(instance.cards.length, 0, "unfollowed");
  instance.destroy();
});
