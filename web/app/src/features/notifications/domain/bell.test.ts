import { test } from "node:test";
import assert from "node:assert/strict";
import { badgeText, bellLabel } from "./bell";

test("the badge is empty at zero or unknown, the count up to 99, and 99+ past it", () => {
  assert.deepEqual([null, 0, 1, 99, 100, 250].map(badgeText), ["", "", "1", "99", "99+", "99+"]);
});

test("the label names the exact count of what needs the person, even past 99, and the unread count beside it", () => {
  assert.deepEqual([1, 99, 100, 250].map((n) => bellLabel(n)), ["Notifications, 1 need you", "Notifications, 99 need you", "Notifications, 100 need you", "Notifications, 250 need you"]);
  assert.equal(bellLabel(2, 3), "Notifications, 2 need you, 3 unread");
  assert.equal(bellLabel(120, 4), "Notifications, 120 need you, 4 unread");
});

test("with nothing open the label names the unread ones, or nothing", () => {
  assert.equal(bellLabel(0, 3), "Notifications, 3 unread");
  assert.equal(bellLabel(null), "Notifications");
  assert.equal(bellLabel(0), "Notifications");
});
