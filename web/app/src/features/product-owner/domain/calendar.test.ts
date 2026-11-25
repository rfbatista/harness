import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { addDays, clockOf, daysBetween, longDay, monthOf, shortDay, stampOf, whenOf } from "./calendar";

const SP = "America/Sao_Paulo"; // UTC-3 all year since 2019

describe("a moment shown in the server's zone", () => {
  test("goes by the zone named, whatever the browser's own is", () => {
    const at = new Date("2026-11-24T02:00:00Z");
    assert.equal(whenOf(at, SP), "Mon 23 Nov, 23:00");
    assert.equal(whenOf(at, "Asia/Tokyo"), "Tue 24 Nov, 11:00");
  });

  test("a zone named by its offset is a fixed offset", () => {
    const at = new Date("2026-11-24T10:00:00Z");
    assert.equal(whenOf(at, "-03:00"), "Tue 24 Nov, 07:00");
    assert.equal(whenOf(at, "+05:30"), "Tue 24 Nov, 15:30");
    assert.equal(clockOf(new Date("2026-11-23T12:00:00Z"), "-0300"), "09:00");
  });

  test("a name the browser does not know reads as the browser's own zone, and never throws", () => {
    assert.match(clockOf(new Date("2026-11-23T12:00:00Z"), "Mars/Olympus"), /^\d\d:\d\d$/);
  });
});

describe("the dates of the digests", () => {
  test("name a day, a month and the clock in the zone", () => {
    assert.equal(shortDay("2026-11-23"), "Mon 23 Nov");
    assert.equal(longDay("2026-11-23"), "Monday 23 November");
    assert.equal(monthOf("2026-11-23"), "November 2026");
    assert.equal(clockOf(new Date("2026-11-23T12:00:00Z"), SP), "09:00");
    assert.equal(stampOf(new Date("2026-11-23T12:00:00Z"), SP), "Mon 23 Nov 09:00");
  });

  test("count and move whole days across months", () => {
    assert.equal(daysBetween("2026-11-20", "2026-11-23"), 3);
    assert.equal(daysBetween("2026-10-31", "2026-11-02"), 2);
    assert.equal(addDays("2026-11-30", 1), "2026-12-01");
    assert.equal(addDays("2026-11-01", -1), "2026-10-31");
  });
});
