import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { GB, MB } from "../../../../shared/testing/memory-fixtures";
import type { MemoryRead } from "../../../../shared/kernel/memory/ports";
import type { MemoryKill, MemorySample } from "../../../../shared/kernel/memory/sample";
import { fakeSource, fakeStream, manualClock } from "../../testing/doubles";
import { ChipPresenter } from "./chip-presenter";

const sample = (gb: number, over: Partial<MemorySample> = {}): MemorySample => ({
  sessionId: "s1",
  taskId: "t1",
  sampledAt: new Date("2026-11-25T22:40:01Z"),
  rssBytes: gb * GB,
  budgetBytes: 4 * GB,
  processes: 9,
  heaviest: { pid: 1, command: "node jest", rssBytes: 864 * MB },
  ...over,
});
const kill = (over: Partial<MemoryKill> = {}): MemoryKill => ({
  sessionId: "s1",
  taskId: "t1",
  killedAt: new Date("2026-11-25T22:30:00Z"),
  pid: 81234,
  command: "node jest",
  rssBytes: 3.5 * GB,
  treeRssBytes: 4.4 * GB,
  budgetBytes: 4 * GB,
  signal: "SIGTERM",
  ...over,
});
const read = (s: MemorySample, over: Partial<MemoryRead> = {}): MemoryRead => ({ sample: s, kills: [], budgetSource: "repository", prevention: { heapMb: 1365, testWorkers: 2 }, ...over });

async function setup(answer: MemoryRead) {
  const source = fakeSource(answer);
  const stream = fakeStream();
  const presenter = new ChipPresenter({ source, stream, clock: manualClock(), sessionId: "s1", live: true, time: (d) => d.toISOString().slice(11, 16) });
  await presenter.start();
  return { source, stream, presenter };
}

describe("memory popover", () => {
  test("it is closed until asked, and opening reads the session again", async () => {
    const { presenter, source } = await setup(read(sample(1.2)));
    assert.equal(presenter.viewModel.popover, null);
    await presenter.open();
    assert.equal(source.reads, 2);
    assert.notEqual(presenter.viewModel.popover, null);
    presenter.close();
    assert.equal(presenter.viewModel.popover, null);
  });

  test("it says the sample, the process count, the heaviest process, the budget and where it came from, and the prevention", async () => {
    const { presenter } = await setup(read(sample(3.4)));
    await presenter.open();
    const p = presenter.viewModel.popover!;
    assert.equal(p.title, "Memory · 3.4 GB of 4 GB · near budget");
    assert.equal(p.processes, "9");
    assert.deepEqual(p.heaviest, { command: "node jest", rss: "864 MB" });
    assert.equal(p.budget, "4 GB, fixed when the session started · from this repository");
    assert.equal(p.prevention, "2 test workers · 1365 MB heap");
    assert.deepEqual([p.state, p.fillPercent], ["near", 85]);
  });

  test("the budget's source reads as task, repository or global", async () => {
    for (const [source, words] of [["task", "from this task"], ["repository", "from this repository"], ["global", "from the global setting"]] as const) {
      const { presenter } = await setup(read(sample(1), { budgetSource: source }));
      await presenter.open();
      assert.match(presenter.viewModel.popover!.budget, new RegExp(`${words}$`));
    }
  });

  test("no eligible process reads as none; one test worker is singular", async () => {
    const { presenter } = await setup(read(sample(1), { prevention: { heapMb: 512, testWorkers: 1 } }));
    await presenter.open();
    assert.equal(presenter.viewModel.popover!.prevention, "1 test worker · 512 MB heap");
    const none = await setup(read(sample(1, { heaviest: null })));
    await none.presenter.open();
    assert.equal(none.presenter.viewModel.popover!.heaviest, null);
  });

  test("the kills of this run list newest first with time, command, rss and signal; none says Nothing yet.", async () => {
    const older = kill();
    const newer = kill({ pid: 2, killedAt: new Date("2026-11-25T22:35:00Z"), command: "pytest", rssBytes: 905 * MB, signal: "SIGKILL" });
    const { presenter } = await setup(read(sample(1), { kills: [older, newer] }));
    await presenter.open();
    const p = presenter.viewModel.popover!;
    assert.deepEqual(p.kills.map((k) => [k.time, k.command, k.rss, k.signal]), [["22:35", "pytest", "905 MB", "SIGKILL"], ["22:30", "node jest", "3.5 GB", "SIGTERM"]]);
    assert.equal(p.killsNote, "");
    const none = await setup(read(sample(1)));
    await none.presenter.open();
    assert.equal(none.presenter.viewModel.popover!.killsNote, "Nothing yet.");
  });

  test("over its budget with no kill, it says the agent itself is over and the vigia never ends it", async () => {
    const { presenter, stream } = await setup(read(sample(4.5)));
    await presenter.open();
    assert.equal(presenter.viewModel.view?.state, "over");
    assert.equal(presenter.viewModel.popover!.killsNote, "Nothing. The agent itself is over the orçamento, and the vigia never ends the agent.");
    stream.kill(kill({ killedAt: new Date("2026-11-25T22:41:00Z") }));
    assert.equal(presenter.viewModel.popover!.kills.length, 1);
    assert.equal(presenter.viewModel.popover!.killsNote, "");
  });

  test("it follows later samples and a kill that arrives live, once", async () => {
    const { presenter, stream } = await setup(read(sample(1.2)));
    await presenter.open();
    stream.sample(sample(2.2, { processes: 4 }));
    assert.deepEqual([presenter.viewModel.popover!.processes, presenter.viewModel.popover!.title], ["4", "Memory · 2.2 GB of 4 GB"]);
    stream.kill(kill());
    stream.kill(kill());
    assert.equal(presenter.viewModel.popover!.kills.length, 1);
  });

  test("it can be toggled, and a chip with nothing to show does not open", async () => {
    const { presenter } = await setup(read(sample(1.2)));
    await presenter.toggle();
    assert.notEqual(presenter.viewModel.popover, null);
    await presenter.toggle();
    assert.equal(presenter.viewModel.popover, null);
    const empty = await setup(read(sample(0, { sampledAt: null, processes: 0, heaviest: null })));
    await empty.presenter.open();
    assert.equal(empty.presenter.viewModel.popover, null);
  });

  test("when the chip goes away the popover closes with it", async () => {
    const { presenter, stream } = await setup(read(sample(1.2)));
    await presenter.open();
    stream.sample(sample(0, { processes: 0, heaviest: null }));
    assert.equal(presenter.viewModel.popover, null);
  });
});
