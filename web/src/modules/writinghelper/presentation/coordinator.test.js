import { assert, file, test } from "../../../shared/testing/test.js";
import { createCoordinator } from "./coordinator.js";

file("writinghelper/presentation/coordinator");

const fakePen = () => ({ closed: [], unavailable: null, close(toField) { this.closed.push(toField); }, setUnavailable(info) { this.unavailable = info; } });

test("opening a second pen closes the first without taking the focus", () => {
  const c = createCoordinator();
  const a = fakePen();
  const b = fakePen();
  c.claim(a);
  c.claim(b);
  assert.deepEqual(a.closed, [false]);
  assert.deepEqual(b.closed, []);
});

test("a pen claiming again does not close itself", () => {
  const c = createCoordinator();
  const a = fakePen();
  c.claim(a);
  c.claim(a);
  assert.deepEqual(a.closed, []);
});

test("a released pen is not closed by the next", () => {
  const c = createCoordinator();
  const a = fakePen();
  c.claim(a);
  c.release(a);
  c.claim(fakePen());
  assert.deepEqual(a.closed, []);
});

test("unavailable reaches every pen, and a pen added later", () => {
  const c = createCoordinator();
  const a = fakePen();
  const b = fakePen();
  c.add(a);
  c.add(b);
  c.markUnavailable({ reason: "not signed in", fixUrl: "/x" });
  assert.deepEqual(a.unavailable, { reason: "not signed in", fixUrl: "/x" });
  assert.deepEqual(b.unavailable, { reason: "not signed in", fixUrl: "/x" });
  const late = fakePen();
  c.add(late);
  assert.deepEqual(late.unavailable, { reason: "not signed in", fixUrl: "/x" });
});
