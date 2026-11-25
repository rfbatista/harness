import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { ProjectDigests } from "../../application/project-digests";
import { ProductOwnerError } from "../../application/ports";
import { parseMarkdown } from "../../domain/markdown";
import { memoryProductOwner } from "../../infrastructure/memory-product-owner";
import { digest, digestItem } from "../../testing/digest-fixtures";
import { DigestsTabPresenter } from "./digests-tab-presenter";

const settled = () => new Promise((resolve) => setImmediate(resolve));
// 2026-11-24 07:00 in São Paulo.
const NOW = Date.parse("2026-11-24T10:00:00Z");

const setup = async (opts: { enabled?: boolean; digestTime?: string; digests?: ReturnType<typeof digest>[]; select?: string } = {}) => {
  const server = memoryProductOwner();
  server.seedProject("p1", opts.enabled ?? true);
  if (opts.digestTime !== undefined) await server.updateSettings("p1", { digestTime: opts.digestTime });
  for (const d of opts.digests ?? []) server.seedDigest("p1", d);
  const digests = new ProjectDigests({ gateway: server, events: server });
  const tab = new DigestsTabPresenter(digests, "p1", { now: () => NOW }, opts.select);
  digests.attach("p1");
  await settled();
  return { server, digests, tab };
};

const moved = (title: string, from: string, to: string) => digestItem({ kind: "task_moved", title, url: `/tasks/${title}`, from, to });
const waiting = (title: string) => digestItem({ kind: "waiting_on_person", title, url: `/tasks/${title}`, from: null, to: null });
const ended = (title: string, url: string) => digestItem({ kind: "session_ended", title, url, from: null, to: null });
const failed = (title: string, url: string) => digestItem({ kind: "session_failed", title, url, from: null, to: null });

describe("the Digests tab", () => {
  test("is loading until the digests and the settings are read", async () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    const digests = new ProjectDigests({ gateway: server, events: server });
    const tab = new DigestsTabPresenter(digests, "p1", { now: () => NOW });
    digests.attach("p1");
    assert.equal(tab.viewModel.state, "loading");
    await settled();
    assert.equal(tab.viewModel.state, "empty");
  });

  test("lists the dates newest first and selects the newest", async () => {
    const { tab } = await setup({ digestTime: "09:00", digests: [digest("2026-11-19"), digest("2026-11-21"), digest("2026-11-20")] });
    const rows = tab.viewModel.rows.filter((r) => r.kind === "digest");
    assert.deepEqual(rows.map((r) => r.kind === "digest" && r.label), ["Sat 21 Nov", "Fri 20 Nov", "Thu 19 Nov"]);
    assert.equal(tab.viewModel.selected?.date, "2026-11-21");
    assert.deepEqual(rows.map((r) => r.kind === "digest" && r.current), [true, false, false]);
  });

  test("counts what each digest holds and puts what waits on the person apart", async () => {
    const items = [moved("a", "review", "done"), moved("b", "backlog", "in_progress"), ended("x", "/s/1"), ended("y", "/s/2"), failed("z", "/s/3"), waiting("w1"), waiting("w2")];
    const { tab } = await setup({ digests: [digest("2026-11-23", { items })] });
    const row = tab.viewModel.rows.find((r) => r.kind === "digest");
    assert.ok(row && row.kind === "digest");
    assert.deepEqual([row.time, row.counts, row.attention], ["09:00", "2 moved · 2 sessions · 1 failed", "2 wait on you"]);
    const one = await setup({ digests: [digest("2026-11-23", { items: [waiting("w1")] })] });
    const single = one.tab.viewModel.rows.find((r) => r.kind === "digest");
    assert.ok(single && single.kind === "digest");
    assert.deepEqual([single.counts, single.attention], ["", "1 waits on you"]);
  });

  test("shows a stretch of quiet days as one grey row, and no row between consecutive dates", async () => {
    const { tab } = await setup({ digests: [digest("2026-11-23"), digest("2026-11-20"), digest("2026-11-19"), digest("2026-11-17")] });
    assert.deepEqual(
      tab.viewModel.rows.map((r) => (r.kind === "gap" ? `gap ${r.label}` : r.kind === "month" ? `month ${r.label}` : r.label)),
      ["month November 2026", "Mon 23 Nov", "gap Sat 21 – Sun 22", "Fri 20 Nov", "Thu 19 Nov", "gap Wed 18 Nov", "Tue 17 Nov"],
    );
  });

  test("heads each month and says the list has gaps", async () => {
    const { tab } = await setup({ digests: [digest("2026-11-02"), digest("2026-10-30")] });
    assert.deepEqual(tab.viewModel.rows.filter((r) => r.kind === "month").map((r) => r.label), ["November 2026", "October 2026"]);
    assert.equal(tab.viewModel.gapNote, "Quiet days are not recorded, so the list has gaps.");
  });

  test("shows the selected digest with its sections in order, omitting an empty one", async () => {
    const items = [ended("architect on Fast pages", "/sessions/s1"), moved("Fast pages", "review", "done"), waiting("Spawn agents"), failed("qa on Branches", "/sessions/s2")];
    const { tab } = await setup({ digests: [digest("2026-11-23", { items, periodStart: new Date("2026-11-20T12:00:00Z"), periodEnd: new Date("2026-11-23T12:00:00Z"), summary: "A **busy** day." })] });
    const shown = tab.viewModel.selected;
    assert.ok(shown);
    assert.equal(shown.heading, "Monday 23 November");
    assert.equal(shown.covers, "Covers Fri 20 Nov 09:00 to Mon 23 Nov 09:00");
    assert.deepEqual(shown.blocks, parseMarkdown("A **busy** day."));
    assert.deepEqual(shown.sections.map((s) => [s.key, s.title, s.items.length]), [["waiting", "Waiting on you", 1], ["moved", "Tasks that moved", 1], ["sessions", "Sessions finished or failed", 2]]);
    assert.deepEqual(shown.sections[1]!.items[0], { title: "Fast pages", href: "/tasks/Fast pages", from: "Review", to: "Done", outcome: "" });
    assert.deepEqual(shown.sections[2]!.items.map((i) => [i.title, i.href, i.outcome]), [["architect on Fast pages", "/sessions/s1", "finished"], ["qa on Branches", "/sessions/s2", "failed"]]);
    const quiet = await setup({ digests: [digest("2026-11-23", { items: [moved("a", "review", "done")] })] });
    assert.deepEqual(quiet.tab.viewModel.selected?.sections.map((s) => s.key), ["moved"]);
  });

  test("links an item only to a page of the web UI", async () => {
    const { tab } = await setup({ digests: [digest("2026-11-23", { items: [moved("ok", "a", "b"), digestItem({ title: "bad", url: "https://elsewhere.test/x" }), digestItem({ title: "worse", url: "//elsewhere.test/x" })] })] });
    assert.deepEqual(tab.viewModel.selected?.sections[0]!.items.map((i) => i.href), ["/tasks/ok", "", ""]);
  });

  test("selecting a date shows that digest, and a date it does not hold changes nothing", async () => {
    const { tab } = await setup({ digests: [digest("2026-11-23"), digest("2026-11-20", { summary: "Friday." })] });
    tab.select("2026-11-20");
    assert.deepEqual(tab.viewModel.selected?.blocks, parseMarkdown("Friday."));
    assert.deepEqual(tab.viewModel.rows.filter((r) => r.kind === "digest").map((r) => r.kind === "digest" && r.current), [false, true]);
    tab.select("2026-01-01");
    assert.equal(tab.viewModel.selected?.date, "2026-11-20");
  });

  test("tells who listens which date the person chose", async () => {
    const chosen: string[] = [];
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    server.seedDigest("p1", digest("2026-11-23"));
    server.seedDigest("p1", digest("2026-11-20"));
    const digests = new ProjectDigests({ gateway: server, events: server });
    const tab = new DigestsTabPresenter(digests, "p1", { now: () => NOW }, "", (date) => chosen.push(date));
    digests.attach("p1");
    await settled();
    tab.select("2026-11-20");
    tab.select("2026-01-01");
    assert.deepEqual(chosen, ["2026-11-20"]);
  });

  test("opens at the date the address names", async () => {
    const { tab } = await setup({ select: "2026-11-20", digests: [digest("2026-11-23"), digest("2026-11-20", { summary: "Friday." })] });
    assert.deepEqual(tab.viewModel.selected?.blocks, parseMarkdown("Friday."));
  });

  test("with no digest and a time set, says the first digest covers the period from when the time was set, and when it records", async () => {
    const { tab } = await setup({ digestTime: "09:00" });
    assert.equal(tab.viewModel.state, "empty");
    assert.match(tab.viewModel.emptyText, /covers the period from when the time was set/);
    assert.match(tab.viewModel.emptyText, /Tue 24 Nov, 09:00/);
    assert.match(tab.viewModel.emptyText, /A quiet day records nothing/);
    assert.equal(tab.viewModel.head, "Next: Tue 24 Nov, 09:00");
    assert.equal(tab.viewModel.timeLabel, "Change time");
  });

  test("with no digest and no time, says so and offers to set one", async () => {
    const { tab } = await setup();
    assert.equal(tab.viewModel.state, "empty");
    assert.match(tab.viewModel.emptyText, /No digest time is set/);
    assert.match(tab.viewModel.emptyText, /covers the period from when the time was set/);
    assert.equal(tab.viewModel.head, "No digest time set");
    assert.equal(tab.viewModel.timeLabel, "Set time");
    assert.equal(tab.viewModel.timeHref, "/projects/p1/settings#product-owner");
  });

  test("off: says no digest is recorded, keeps the kept ones readable, and turns it on", async () => {
    const { tab, server } = await setup({ enabled: false, digestTime: "09:00", digests: [digest("2026-11-23"), digest("2026-11-20")] });
    assert.equal(tab.viewModel.state, "ready");
    assert.equal(tab.viewModel.offNote, "The product owner is off, so no digest is recorded. The 2 digests below are kept.");
    assert.equal(tab.viewModel.head, "Paused while the product owner is off");
    assert.equal(tab.viewModel.selected?.date, "2026-11-23");
    await tab.turnOn();
    assert.equal(tab.viewModel.offNote, "");
    assert.equal(tab.viewModel.head, "Next: Tue 24 Nov, 09:00");
    assert.equal((await server.settings("p1")).enabled, true);
  });

  test("off with nothing kept is the empty state under the banner", async () => {
    const { tab } = await setup({ enabled: false });
    assert.equal(tab.viewModel.state, "empty");
    assert.equal(tab.viewModel.offNote, "The product owner is off, so no digest is recorded.");
  });

  test("a refusal to turn it on is shown and the banner stays", async () => {
    const { tab, server } = await setup({ enabled: false });
    server.failWith(new ProductOwnerError("NETWORK_ERROR", "offline"));
    await tab.turnOn();
    assert.equal(tab.viewModel.turnOnError, "offline");
    assert.notEqual(tab.viewModel.offNote, "");
  });

  test("a failed read shows why and reads again on retry", async () => {
    const server = memoryProductOwner();
    server.seedProject("p1", true);
    server.seedDigest("p1", digest("2026-11-23"));
    server.failWith(new ProductOwnerError("NETWORK_ERROR", "offline"));
    const digests = new ProjectDigests({ gateway: server, events: server });
    const tab = new DigestsTabPresenter(digests, "p1", { now: () => NOW });
    digests.attach("p1");
    await settled();
    assert.deepEqual([tab.viewModel.state, tab.viewModel.error], ["error", "offline"]);
    server.failWith(null);
    tab.retry();
    await settled();
    assert.deepEqual([tab.viewModel.state, tab.viewModel.selected?.date], ["ready", "2026-11-23"]);
  });

  test("follows the settings event: a new time moves the next run", async () => {
    const { tab, server } = await setup({ digestTime: "09:00" });
    await server.updateSettings("p1", { digestTime: "08:30" });
    assert.equal(tab.viewModel.head, "Next: Tue 24 Nov, 08:30");
  });
});
