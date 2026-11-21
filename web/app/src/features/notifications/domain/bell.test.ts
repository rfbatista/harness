import { test } from "node:test";
import assert from "node:assert/strict";
import { badgeText, bellLabel } from "./bell";

test("the badge is empty at zero or unknown, the count up to 99, and 99+ past it", () => {
  assert.deepEqual([null, 0, 1, 99, 100, 250].map(badgeText), ["", "", "1", "99", "99+", "99+"]);
});

test("the label names the exact count, even past 99", () => {
  assert.deepEqual([null, 0, 1, 99, 100, 250].map(bellLabel), ["Notifications", "Notifications", "Notifications, 1 unread", "Notifications, 99 unread", "Notifications, 100 unread", "Notifications, 250 unread"]);
});
