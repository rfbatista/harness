import { Codes, StructuredError } from "../../../../shared/domain/errors.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { flush, manualTimers } from "../../../../shared/testing/doubles.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { memoryTranscripts } from "../../infrastructure/memory-transcripts.js";
import { numbered } from "../../testing/transcript-contract.js";
import { CHUNK, history, MAX_LINES, POLL_MS } from "./history.js";

file("sessions/presentation/history");

/** A log element as far as the component reads it: scrolled to scrollTop of scrollHeight. */
function fakeLog({ scrollHeight = 10_000, clientHeight = 500, scrollTop = 0 } = {}) {
  return {
    scrollHeight,
    clientHeight,
    scrollTop,
    getBoundingClientRect: () => ({ top: 0, bottom: clientHeight }),
    querySelectorAll: () => [],
    querySelector: () => null,
  };
}

async function make({ transcripts = { s1: numbered(5) }, live = false, visible = () => true } = {}) {
  const memory = memoryTranscripts({ transcripts });
  const timers = manualTimers();
  const log = fakeLog();
  const el = { querySelector: (selector) => (selector === "[role=log]" ? log : null) };
  const mounted = mount(() => history({ transcripts: memory.gateway, timers, pageVisible: visible })({ sessionId: "s1", live }), { el });
  mounted.instance.init();
  await flush();
  mounted.tick();
  return { ...mounted, memory, timers, log };
}

const texts = (instance) => instance.lines.map((l) => l.text);

test("opens on the newest page, scrolled to the bottom", async () => {
  const { instance, log, memory } = await make({ transcripts: { s1: numbered(300) } });
  assert.deepEqual([instance.state, instance.showsLog, instance.entries.length, instance.hasOlder], ["ready", true, 200, true]);
  assert.deepEqual([texts(instance)[0], texts(instance).at(-1)], ["101", "300"]);
  assert.equal(log.scrollTop, log.scrollHeight);
  assert.deepEqual(memory.calls, [{ sessionId: "s1", before: undefined, after: undefined, limit: 200 }]);
  assert.deepEqual([instance.canLoadOlder, instance.showsJump], [true, false]);
});

test("scrolling near the top loads the page before, back to the start of the conversation", async () => {
  const { instance, log, memory } = await make({ transcripts: { s1: numbered(300) } });
  log.scrollTop = 10;
  instance.onScroll();
  await flush();
  assert.equal(memory.calls.at(-1).before, instance.entries[100].cursor);
  assert.deepEqual([instance.entries.length, texts(instance)[0], instance.hasOlder, instance.atStart], [300, "1", false, true]);
  instance.onScroll();
  await flush();
  assert.equal(memory.calls.length, 2, "nothing older to ask for");
});

test("only a window of lines renders; moving it over loaded lines asks the server for nothing", async () => {
  const { instance, memory } = await make({ transcripts: { s1: numbered(1000) } });
  for (let i = 0; i < 4; i++) {
    await instance.older();
  }
  assert.deepEqual([instance.entries.length, instance.hasOlder], [1000, false]);
  assert.deepEqual([instance.start, instance.end, instance.lines.length], [0, MAX_LINES, MAX_LINES]);
  const calls = memory.calls.length;
  instance.later();
  assert.deepEqual([instance.start, instance.end], [CHUNK, MAX_LINES + CHUNK]);
  await instance.older();
  assert.deepEqual([instance.start, instance.end, memory.calls.length], [0, MAX_LINES, calls]);
  instance.jump();
  assert.deepEqual([instance.start, instance.end, instance.following], [1000 - MAX_LINES, 1000, true]);
});

test("an ended session loads once and never polls", async () => {
  const { timers } = await make({ live: false });
  assert.deepEqual(timers.delays(), []);
});

test("a running session catches up every 2 s while the tab and the page are visible", async () => {
  let visible = true;
  const { instance, timers, memory } = await make({ live: true, visible: () => visible });
  assert.deepEqual(timers.delays(), [POLL_MS]);
  memory.append("s1", { text: "6" });
  timers.tick();
  await flush();
  assert.equal(memory.calls.at(-1).after, instance.entries[4].cursor);
  assert.deepEqual(texts(instance).slice(-2), ["5", "6"]);
  assert.deepEqual(timers.delays(), [POLL_MS]);

  visible = false;
  const calls = memory.calls.length;
  timers.tick();
  await flush();
  assert.equal(memory.calls.length, calls, "a hidden page does not poll");
  assert.deepEqual(timers.delays(), [POLL_MS], "but checks again later");

  instance.destroy();
  assert.deepEqual(timers.delays(), []);
});

test("a reader at the bottom follows new lines; one who scrolled up stays put and is offered Jump to latest", async () => {
  const { instance, timers, memory, log, tick } = await make({ live: true });
  memory.append("s1", { text: "6" });
  timers.tick();
  await flush();
  log.scrollTop = 0;
  tick();
  assert.equal(log.scrollTop, log.scrollHeight, "followed down");

  log.scrollTop = 4000; // well above the bottom
  instance.onScroll();
  assert.deepEqual([instance.following, instance.showsJump, instance.jumpLabel], [false, true, "Jump to latest"]);
  memory.append("s1", { text: "7" }, { text: "8" });
  timers.tick();
  await flush();
  tick();
  assert.equal(log.scrollTop, 4000, "the reader was not moved");
  assert.deepEqual([instance.newCount, instance.jumpLabel, texts(instance).at(-1)], [2, "2 new · Jump to latest", "8"]);

  instance.jump();
  tick();
  assert.deepEqual([instance.following, instance.newCount, instance.showsJump, log.scrollTop], [true, 0, false, log.scrollHeight]);
});

test("no saved conversation is an empty state, until the CLI writes one", async () => {
  const { instance, timers, memory } = await make({ transcripts: {}, live: true });
  assert.deepEqual([instance.state, instance.isMissing, instance.showsError, instance.showsLog], ["missing", true, false, false]);
  memory.append("s1", { kind: "user", text: "hello" });
  timers.tick();
  await flush();
  assert.deepEqual([instance.state, texts(instance)], ["ready", ["hello"]]);
});

test("a first page that fails says why and loads again on Retry", async () => {
  const memory = memoryTranscripts({ transcripts: { s1: numbered(2) } });
  memory.failNext(new StructuredError(Codes.NETWORK, "The harness server is not reachable."));
  const { instance } = mount(() => history({ transcripts: memory.gateway, timers: manualTimers() })({ sessionId: "s1" }));
  instance.init();
  await flush();
  assert.deepEqual([instance.state, instance.showsError, instance.error], ["failed", true, "The harness server is not reachable."]);
  await instance.retry();
  assert.deepEqual([instance.state, instance.showsError, texts(instance)], ["ready", false, ["1", "2"]]);
});

test("a catch-up that fails keeps the lines and says why; the next one clears it", async () => {
  const { instance, timers, memory } = await make({ live: true });
  memory.failNext(new StructuredError(Codes.UNKNOWN, "boom", 500));
  timers.tick();
  await flush();
  assert.deepEqual([instance.state, instance.error, texts(instance).length], ["ready", "boom", 5]);
  timers.tick();
  await flush();
  assert.equal(instance.showsError, false);
});

test("a tool line opens and closes on its own", async () => {
  const { instance } = await make({ transcripts: { s1: [{ kind: "tool_use", tool: "Bash", text: "ls" }, { kind: "tool_result", tool: "Bash", text: "a\nb" }] } });
  const [call, result] = instance.lines;
  instance.toggle(result);
  assert.deepEqual([instance.expanded(call), instance.expanded(result)], [false, true]);
  instance.toggle(result);
  assert.equal(instance.expanded(result), false);
});
