import { describe, test } from "node:test";
import assert from "node:assert/strict";
import { GB, MB } from "../../testing/memory-fixtures";
import { formatBudget, formatShown } from "./bytes";

describe("memory rounding", () => {
  test("from 1 GB it shows 0.1 GB, below it whole MB", () => {
    assert.equal(formatShown(3.45 * GB), "3.5 GB");
    assert.equal(formatShown(3.44 * GB), "3.4 GB");
    assert.equal(formatShown(4 * GB), "4.0 GB");
    assert.equal(formatShown(905 * MB), "905 MB");
    assert.equal(formatShown(1023.6 * MB), "1024 MB", "never rounds up into a unit it is not in");
    assert.equal(formatShown(0), "0 MB");
  });

  test("the budget reads as the person set it", () => {
    assert.equal(formatBudget(4 * GB), "4 GB");
    assert.equal(formatBudget(2.5 * GB), "2.5 GB");
    assert.equal(formatBudget(512 * MB), "512 MB");
  });
});
