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
    assert.equal(formatShown(1023.6 * MB), "1.0 GB", "MB rounding that would reach 1024 reads in GB");
    assert.equal(formatShown(1023.4 * MB), "1023 MB");
    assert.equal(formatShown(1023.5 * MB), "1.0 GB", "the point where MB would round to 1024");
    assert.equal(formatShown(0), "0 MB");
    assert.equal(formatShown(1023 * MB), "1023 MB", "1023 MiB stays in MB");
    assert.equal(formatShown(1 * GB), "1.0 GB", "and 1 GiB is the first to read in GB");
  });

  test("the budget reads as the person set it", () => {
    assert.equal(formatBudget(4 * GB), "4 GB");
    assert.equal(formatBudget(2.5 * GB), "2.5 GB");
    assert.equal(formatBudget(512 * MB), "512 MB");
  });
});
