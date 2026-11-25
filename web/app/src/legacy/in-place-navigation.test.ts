// The Alpine in-place navigation tells the islands where the person went
// (Contract: Harness server ↔ Web UI — the top bar island, §4).

import { test } from "node:test";
import assert from "node:assert/strict";
import { startInPlaceNavigation } from "../../../src/in-place-navigation.js";

function setup(path = "/a") {
  const win = new EventTarget() as EventTarget & Record<string, unknown>;
  const location = { href: `http://harness.test${path}`, pathname: path, hash: "", origin: "http://harness.test" };
  const go = (to: string) => {
    location.pathname = to;
    location.href = `http://harness.test${to}`;
  };
  win.history = { state: null, pushState: (_s: unknown, _t: string, url: string) => go(url), replaceState: () => {} };
  win.fetch = async () => new Response("");
  win.scrollTo = () => {};
  const doc = Object.assign(new EventTarget(), { defaultView: win, location, getElementById: () => null, querySelectorAll: () => [], querySelector: () => null });
  const ajax: unknown[][] = [];
  startInPlaceNavigation({ ajax: (...args: unknown[]) => ajax.push(args) }, { Alpine: { store: () => undefined }, navigate: () => {}, doc: doc as never });
  const navigated: string[] = [];
  win.addEventListener("harness:navigated", (e) => navigated.push((e as CustomEvent<{ path: string }>).detail.path));
  const detail = (responsePath: string) => ({ requestConfig: { headers: { "HX-Target": "app-shell" } }, pathInfo: { responsePath } });
  const swapTo = (responsePath: string) => {
    doc.dispatchEvent(Object.assign(new Event("htmx:afterSwap"), { detail: detail(responsePath) }));
    doc.dispatchEvent(Object.assign(new Event("htmx:afterSettle"), { detail: detail(responsePath) }));
  };
  return { win, doc, go, ajax, navigated, swapTo };
}

test("dispatches harness:navigated with the new path after a swap", () => {
  const { navigated, swapTo } = setup("/a");
  swapTo("/projects/p1");
  assert.deepEqual(navigated, ["/projects/p1"]);
});

test("dispatches it after Back and Forward too, with the path of the entry landed on", () => {
  const { win, doc, go, navigated, swapTo, ajax } = setup("/b");
  // Back lands on an entry of another page: the page is swapped for it.
  go("/a");
  win.dispatchEvent(Object.assign(new Event("popstate"), { state: { page: "other:0" } }));
  assert.equal(ajax.length, 1);
  assert.equal(doc.location.pathname, "/a");
  swapTo("/a");
  assert.deepEqual(navigated, ["/a"]);
});

test("does not dispatch it for a swap that is not a navigation", () => {
  const { doc, navigated } = setup("/a");
  const other = { requestConfig: { headers: {} }, pathInfo: { responsePath: "/a" } };
  doc.dispatchEvent(Object.assign(new Event("htmx:afterSettle"), { detail: other }));
  assert.deepEqual(navigated, []);
});
