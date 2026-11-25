import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { addDays, clockOf, daysBetween, longDay, monthOf, nextRun, shortDay, stampOf, whenOf } from "./calendar";

const SP = "America/Sao_Paulo"; // UTC-3 all year since 2019

describe("the next run of a digest time", () => {
  test("is today when the time is still ahead in the server's zone", () => {
    // 2026-11-24 10:00 in São Paulo is 13:00Z.
    const next = nextRun(new Date("2026-11-24T10:00:00Z"), "08:30", SP);
    assert.equal(next?.toISOString(), "2026-11-24T11:30:00.000Z");
  });

  test("is tomorrow when the time has passed, and when it is this very minute", () => {
    assert.equal(nextRun(new Date("2026-11-24T13:00:00Z"), "09:00", SP)?.toISOString(), "2026-11-25T12:00:00.000Z");
    assert.equal(nextRun(new Date("2026-11-24T12:00:00Z"), "09:00", SP)?.toISOString(), "2026-11-25T12:00:00.000Z");
  });

  test("goes by the server's zone, not by the zone of whoever computes it", () => {
    // 02:00Z is 23:00 on the 23rd in São Paulo, so 09:00 is ten hours away; in Tokyo it is 11:00 on the 24th, so 09:00 is tomorrow.
    const now = new Date("2026-11-24T02:00:00Z");
    assert.equal(whenOf(nextRun(now, "09:00", SP)!, SP), "Tue 24 Nov, 09:00");
    assert.equal(whenOf(nextRun(now, "09:00", "Asia/Tokyo")!, "Asia/Tokyo"), "Wed 25 Nov, 09:00");
  });

  test("moves across a month and a year end", () => {
    assert.equal(whenOf(nextRun(new Date("2026-11-30T20:00:00Z"), "09:00", SP)!, SP), "Tue 1 Dec, 09:00");
    assert.equal(whenOf(nextRun(new Date("2026-12-31T20:00:00Z"), "09:00", SP)!, SP), "Fri 1 Jan, 09:00");
  });

  test("keeps the wall clock across a change of offset", () => {
    // Europe/Berlin leaves summer time on 2026-10-25: 09:00 stays 09:00 on the clock.
    assert.equal(nextRun(new Date("2026-10-24T12:00:00Z"), "09:00", "Europe/Berlin")?.toISOString(), "2026-10-25T08:00:00.000Z");
    assert.equal(nextRun(new Date("2026-10-25T09:00:00Z"), "09:00", "Europe/Berlin")?.toISOString(), "2026-10-26T08:00:00.000Z");
  });

  test("is nothing for a time or a zone it cannot read", () => {
    assert.equal(nextRun(new Date(), "9:00", SP), null);
    assert.equal(nextRun(new Date(), "09:00", "Not/AZone"), null);
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
