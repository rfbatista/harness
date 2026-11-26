import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { GB, MB } from "../../../../shared/testing/memory-fixtures";
import type { MemoryRead } from "../../../../shared/kernel/memory/ports";
import type { MemorySample } from "../../../../shared/kernel/memory/sample";
import { fakeSource, fakeStream, manualClock } from "../../testing/doubles";
import { ChipPresenter } from "./chip-presenter";

const sample = (gb: number, over: Partial<MemorySample> = {}): MemorySample => ({
  sessionId: "s1",
  taskId: "t1",
  sampledAt: new Date("2026-11-25T22:40:01Z"),
  rssBytes: gb * GB,
  budgetBytes: 4 * GB,
  processes: 7,
  heaviest: { pid: 1, command: "node jest", rssBytes: 900 * MB },
  ...over,
});
const read = (s: MemorySample): MemoryRead => ({ sample: s, kills: [], budgetSource: "repository", prevention: { heapMb: 1365, testWorkers: 2 } });

function setup(answer: MemoryRead | null | Error = null, live = true) {
  const source = fakeSource(answer);
  const stream = fakeStream();
  const clock = manualClock();
  const presenter = new ChipPresenter({ source, stream, clock, sessionId: "s1", live });
  return { source, stream, clock, presenter };
}

describe("memory chip", () => {
  test("a session that is not live has no chip and follows nothing", async () => {
    const { presenter, source, stream } = setup(read(sample(1)), false);
    await presenter.start();
    assert.equal(presenter.viewModel.view, null);
    assert.deepEqual([source.reads, stream.followers], [0, 0]);
  });

  test("it paints from the initial read before any event", async () => {
    const { presenter } = setup(read(sample(3.4)));
    await presenter.start();
    assert.equal(presenter.viewModel.view?.num, "3.4");
    assert.equal(presenter.viewModel.view?.state, "near");
  });

  test("no chip when the read is 404, before the first sample, or at rss 0", async () => {
    const a = setup(null);
    await a.presenter.start();
    assert.equal(a.presenter.viewModel.view, null);
    const b = setup(read(sample(0, { sampledAt: null, processes: 0, heaviest: null })));
    await b.presenter.start();
    assert.equal(b.presenter.viewModel.view, null);
    const shown = () => b.presenter.viewModel.view;
    b.stream.sample(sample(1.2));
    assert.equal(shown()?.num, "1.2", "the first sample brings the chip");
    b.stream.sample(sample(0, { processes: 0, heaviest: null }));
    assert.equal(shown(), null, "a session that ended clears it");
  });

  test("the stream's samples follow, and the state waits out the hold", async () => {
    const { presenter, stream, clock } = setup(read(sample(1.0)));
    await presenter.start();
    assert.equal(presenter.viewModel.view?.state, "normal");
    stream.sample(sample(3.3)); // 82 %
    assert.equal(presenter.viewModel.view?.state, "near", "the first change after the read is free: the read set the tracker long ago");
    clock.advance(1000);
    stream.sample(sample(4.2));
    assert.equal(presenter.viewModel.view?.state, "near", "over waits for the 3 s hold");
    clock.advance(2500);
    stream.sample(sample(4.2));
    assert.equal(presenter.viewModel.view?.state, "over");
    assert.equal(presenter.viewModel.view?.word, "over budget");
  });

  test("it tells its listeners only when what shows changed", async () => {
    const { presenter, stream } = setup(read(sample(2.0)));
    await presenter.start();
    let changes = 0;
    presenter.subscribe(() => changes++);
    changes = 0;
    stream.sample(sample(2.0));
    stream.sample(sample(2.001));
    assert.equal(changes, 0);
    stream.sample(sample(2.2));
    assert.equal(changes, 1);
  });

  test("a samples of another session is ignored", async () => {
    const { presenter, stream } = setup(read(sample(2.0)));
    await presenter.start();
    stream.sample(sample(3.9, { sessionId: "other" }));
    assert.equal(presenter.viewModel.view?.num, "2.0");
  });

  test("a reconnect keeps the last value and re-reads; a failed re-read changes nothing", async () => {
    const { presenter, stream, source } = setup(read(sample(2.0)));
    await presenter.start();
    stream.sample(sample(2.5));
    source.answer = read(sample(2.7));
    stream.resync();
    assert.equal(presenter.viewModel.view?.num, "2.5", "the old value stays while it re-reads");
    await presenter.settled();
    assert.equal(source.reads, 2);
    assert.equal(presenter.viewModel.view?.num, "2.7");
    source.answer = new Error("offline");
    stream.resync();
    await presenter.settled();
    assert.equal(presenter.viewModel.view?.num, "2.7");
  });

  test("a read that arrives after a newer sample does not roll it back", async () => {
    const { presenter, stream } = setup(read(sample(2.0)));
    const starting = presenter.start();
    stream.sample(sample(3.0, { sampledAt: new Date("2026-11-25T22:40:09Z") }));
    await starting;
    assert.equal(presenter.viewModel.view?.num, "3.0");
  });

  test("disposing stops following the stream", async () => {
    const { presenter, stream } = setup(read(sample(2.0)));
    await presenter.start();
    assert.equal(stream.followers, 1);
    presenter.dispose();
    assert.equal(stream.followers, 0);
  });
});
