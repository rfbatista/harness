import { assert, file, test } from "../testing/test.js";
import { count, relativeTime } from "./format.js";

file("shared/presentation/format");

test("relative time is compact and never negative", () => {
  const now = new Date("2026-10-02T14:00:00Z");
  const ago = (ms) => new Date(now.getTime() - ms);
  assert.equal(relativeTime(ago(30_000), now), "now");
  assert.equal(relativeTime(ago(4 * 60_000), now), "4m");
  assert.equal(relativeTime(ago(3 * 3_600_000), now), "3h");
  assert.equal(relativeTime(ago(2 * 86_400_000), now), "2d");
  assert.equal(relativeTime(new Date(now.getTime() + 5_000), now), "now");
});

test("count pluralizes", () => {
  assert.equal(count(1, "session"), "1 session");
  assert.equal(count(0, "session"), "0 sessions");
});
