import { test } from "node:test";
import assert from "node:assert/strict";
import { navigationSignalContract } from "../testing/navigation-signal-contract";
import { browserNavigationSignal, NAVIGATED_EVENT } from "./browser-navigation-signal";
import { memoryNavigationSignal } from "./memory-navigation-signal";

function stubWindow(pathname = "/") {
  const target = new EventTarget();
  return Object.assign(target, { location: { pathname } });
}

navigationSignalContract("browser adapter on a stub window", () => {
  const win = stubWindow();
  return {
    signal: browserNavigationSignal(win),
    navigate: (path) => void win.dispatchEvent(new CustomEvent(NAVIGATED_EVENT, { detail: { path } })),
  };
});

navigationSignalContract("in-memory adapter", () => {
  const signal = memoryNavigationSignal();
  return { signal, navigate: signal.emit };
});

test("the browser adapter delivers the location's path on popstate", () => {
  const win = stubWindow("/agents");
  const seen: string[] = [];
  browserNavigationSignal(win).subscribe((path) => seen.push(path));
  win.dispatchEvent(new Event("popstate"));
  assert.deepEqual(seen, ["/agents"]);
});

test("the browser adapter ignores a harness:navigated event whose detail is missing, not an object, or has no string path", () => {
  const win = stubWindow();
  const seen: string[] = [];
  browserNavigationSignal(win).subscribe((path) => seen.push(path));
  for (const detail of [undefined, null, "/agents", {}, { path: 42 }, ["/agents"]]) win.dispatchEvent(new CustomEvent(NAVIGATED_EVENT, { detail }));
  assert.deepEqual(seen, []);
});
