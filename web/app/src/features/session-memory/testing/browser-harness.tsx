// The real ChipView on the real ChipPresenter, over fake ports, for the Go browser test (browser_memory_chip_test.go):
// it is bundled with esbuild and driven from the page through window.memoryHarness.

import { createRoot } from "react-dom/client";
import type { MemoryRead } from "../../../shared/kernel/memory/ports";
import type { MemoryKill, MemorySample } from "../../../shared/kernel/memory/sample";
import { ChipPresenter } from "../presentation/presenters/chip-presenter";
import { ChipView } from "../presentation/views/chip-view";
import { fakeSource, fakeStream } from "./doubles";

const GB = 1024 ** 3;
const SESSION = "s1";

const sample = (gb: number, at = Date.now()): MemorySample => ({
  sessionId: SESSION, taskId: "t1", sampledAt: new Date(at), rssBytes: gb * GB, budgetBytes: 4 * GB, processes: 7,
  heaviest: { pid: 1, command: "node jest", rssBytes: 0.9 * GB },
});
const kill = (pid: number): MemoryKill => ({
  sessionId: SESSION, taskId: "t1", killedAt: new Date(), pid, command: "node jest", rssBytes: 3.5 * GB, treeRssBytes: 4.4 * GB, budgetBytes: 4 * GB, signal: "SIGTERM",
});

// ?gb=3.4 sets the first sample; ?kill=1 puts a kill of this minute in the first read.
const params = new URLSearchParams(location.search);
const initial: MemoryRead = { sample: sample(Number(params.get("gb") ?? 1.2)), kills: params.has("kill") ? [kill(1)] : [], budgetSource: "repository", prevention: { heapMb: 1365, testWorkers: 2 } };
const source = fakeSource(initial);
// The first read is slow, as on the network: the chip's first render has nothing to show.
let release!: () => void;
source.gate = new Promise<void>((resolve) => (release = resolve));
const stream = fakeStream();
const presenter = new ChipPresenter({ source, stream, clock: { now: () => Date.now() }, scheduler: { after: (ms, fn) => { const id = setTimeout(fn, ms); return () => clearTimeout(id); } }, sessionId: SESSION, live: true });

const root = document.getElementById("root")!;
createRoot(root).render(<ChipView presenter={presenter} toastHost={document.body} />);
void presenter.start();

(window as unknown as { memoryHarness: unknown }).memoryHarness = {
  /** The first read answers. */
  release: () => release(),
  sample: (gb: number) => stream.sample(sample(gb)),
  kill: (pid: number) => stream.kill(kill(pid)),
  resync: () => stream.resync(),
  readFails: () => void (source.answer = new Error("offline")),
  readIs: (read: MemoryRead | null) => void (source.answer = read),
  killOf: (pid: number) => kill(pid),
};
