import { StructuredError } from "../../../../shared/domain/errors.js";
import { fixedClock } from "../../../../shared/infrastructure/clock.js";
import { mount } from "../../../../shared/testing/alpine.js";
import { assert, file, test } from "../../../../shared/testing/test.js";
import { fakeServices, logDTO } from "../../testing/fakeGateway.js";
import { serviceLog } from "./serviceLog.js";

file("services/presentation/components/serviceLog");

/** A scrollable element: the log's viewport. */
const viewport = (o = {}) => ({ scrollTop: 0, scrollHeight: 1000, clientHeight: 400, ...o });
const settle = async () => { for (let i = 0; i < 6; i++) await Promise.resolve(); };

async function open(read = logDTO({ text: "[harness] command: make air\nlistening\n", next_offset: 37 })) {
  const gateway = fakeServices();
  gateway.logRead = () => read;
  const el = { dataset: { serviceId: "s1" } };
  const { instance, tick } = mount(serviceLog({ gateway, clock: fixedClock("2026-11-07T14:32:00") }), { el });
  const port = viewport();
  instance.$refs = { log: port };
  instance.init();
  await settle();
  tick();
  return { log: instance, gateway, port, tick };
}
const texts = (log) => log.lines.map((l) => l.text);

test("it reads the last part of the log and follows the log feed from where the read ended", async () => {
  const { log, gateway } = await open();
  assert.deepEqual(gateway.logReads, [["s1", undefined]]);
  assert.deepEqual(texts(log), ["[harness] command: make air", "listening"]);
  assert.deepEqual(gateway.logFollowed, [["s1", 37]]);
  assert.equal(log.loading, false);
});

test("it opens at the bottom, following", async () => {
  const { log, port } = await open();
  assert.equal(log.following, true);
  assert.equal(port.scrollTop, port.scrollHeight);
  assert.equal(log.footer, "Following");
});

test("a chunk is appended and, while following, the view stays at the bottom", async () => {
  const { log, gateway, port, tick } = await open();
  port.scrollHeight = 1200;
  gateway.emitLog({ kind: "chunk", run_id: "r1", offset: 37, text: "next\n" });
  tick();
  assert.deepEqual(texts(log).slice(-1), ["next"]);
  assert.equal(port.scrollTop, 1200);
});

test("scrolled up, it keeps its place, counts new lines, and Jump to latest follows again", async () => {
  const { log, gateway, port, tick } = await open();
  port.scrollTop = 100;
  log.onScroll({ currentTarget: port });
  assert.equal(log.following, false);
  assert.equal(log.footer, "Paused · scrolled up");
  gateway.emitLog({ kind: "chunk", run_id: "r1", offset: 37, text: "a\nb\n" });
  tick();
  assert.equal(port.scrollTop, 100, "the view did not move");
  assert.equal(log.unseen, 2);
  assert.equal(log.unseenText, "2 new lines below");
  log.jump();
  tick();
  assert.deepEqual([log.following, log.unseen, port.scrollTop], [true, 0, port.scrollHeight]);
});

test("scrolling back to the bottom resumes following", async () => {
  const { log, port } = await open();
  port.scrollTop = 100;
  log.onScroll({ currentTarget: port });
  port.scrollTop = port.scrollHeight - port.clientHeight;
  log.onScroll({ currentTarget: port });
  assert.equal(log.following, true);
  assert.equal(log.unseen, 0);
});

test("one new line below reads in the singular", async () => {
  const { log, gateway, port } = await open();
  port.scrollTop = 0;
  log.onScroll({ currentTarget: port });
  gateway.emitLog({ kind: "chunk", run_id: "r1", offset: 37, text: "a\n" });
  assert.equal(log.unseenText, "1 new line below");
});

test("a new run clears the viewer and says so", async () => {
  const { log, gateway } = await open();
  gateway.emitLog({ kind: "reset", run_id: "r2" });
  assert.deepEqual(log.lines.map((l) => [l.kind, l.text]), [["reset", "New run started 14:32 · the log was cleared"]]);
  gateway.emitLog({ kind: "chunk", run_id: "r2", offset: 0, text: "fresh\n" });
  assert.deepEqual(texts(log).slice(-1), ["fresh"]);
});

test("a gap re-reads the log from the end of what it holds", async () => {
  const { log, gateway } = await open();
  gateway.logRead = (offset) => logDTO({ text: "missed\nmore\n", offset, next_offset: offset + 12 });
  gateway.emitLog({ kind: "chunk", run_id: "r1", offset: 99, text: "late\n" });
  await settle();
  assert.deepEqual(gateway.logReads.at(-1), ["s1", 37]);
  assert.deepEqual(texts(log).slice(-2), ["missed", "more"]);
});

test("a resynced feed re-reads from the end of what it holds", async () => {
  const { log, gateway } = await open();
  gateway.logRead = (offset) => logDTO({ text: "back\n", offset, next_offset: offset + 5 });
  gateway.logStatus("resynced");
  await settle();
  assert.deepEqual(gateway.logReads.at(-1), ["s1", 37]);
  assert.deepEqual(texts(log).slice(-1), ["back"]);
});

test("a re-read that finds another run replaces what it held", async () => {
  const { log, gateway } = await open();
  gateway.logRead = () => logDTO({ text: "new run line\n", offset: 0, next_offset: 13, run_id: "r2" });
  gateway.logStatus("resynced");
  await settle();
  assert.deepEqual(texts(log), ["new run line"]);
});

test("a log that cannot be read shows the error and Retry reads it again", async () => {
  const gateway = fakeServices();
  gateway.logError = new StructuredError("SERVICE_NOT_FOUND", "no such service");
  gateway.logRead = () => logDTO({ text: "ok\n", next_offset: 3 });
  const { instance } = mount(serviceLog({ gateway, clock: fixedClock("2026-11-07T14:32:00") }), { el: { dataset: { serviceId: "s1" } } });
  instance.$refs = { log: viewport() };
  instance.init();
  await settle();
  assert.equal(instance.error.code, "SERVICE_NOT_FOUND");
  gateway.logError = null;
  await instance.retry();
  assert.equal(instance.error, null);
  assert.deepEqual(texts(instance), ["ok"]);
});

test("the top note says the start is not shown when the read began inside the log", async () => {
  const { log } = await open(logDTO({ text: "cut off\nwhole\n", offset: 900, next_offset: 913 }));
  assert.equal(log.note, "Showing the last 64 KB. The log keeps the last 5 MB of this run.");
  assert.deepEqual(texts(log), ["whole"]);
});

test("closing stops following", async () => {
  const { log, gateway } = await open();
  log.destroy();
  assert.equal(gateway.logUnfollowed, 1);
});
