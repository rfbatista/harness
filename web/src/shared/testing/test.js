// A minimal test runner for the browser: no dependencies, runs where the code
// runs. Test files register with `test`; web/test/index.html calls `run`.
//
//   import { assert, test } from "../shared/testing/test.js";
//   test("adds", () => assert.equal(1 + 1, 2));

const registry = [];
let currentFile = "";

/** Groups the tests registered after it under a file name in the report. */
export function file(name) {
  currentFile = name;
}

/** @param {string} name @param {() => void | Promise<void>} fn */
export function test(name, fn) {
  registry.push({ file: currentFile, name, fn });
}

export class AssertionError extends Error {
  constructor(message) {
    super(message);
    this.name = "AssertionError";
  }
}

const show = (v) => (typeof v === "string" ? JSON.stringify(v) : stringify(v));

function stringify(v) {
  try {
    return JSON.stringify(v, (_, x) => (x instanceof Date ? `Date(${x.toISOString()})` : x));
  } catch {
    return String(v);
  }
}

export const assert = {
  ok(value, message = "expected a truthy value") {
    if (!value) throw new AssertionError(`${message} (got ${show(value)})`);
  },
  equal(actual, expected, message = "") {
    if (!Object.is(actual, expected)) {
      throw new AssertionError(`${message ? message + ": " : ""}expected ${show(expected)}, got ${show(actual)}`);
    }
  },
  deepEqual(actual, expected, message = "") {
    if (stringify(actual) !== stringify(expected)) {
      throw new AssertionError(`${message ? message + ": " : ""}expected ${stringify(expected)}, got ${stringify(actual)}`);
    }
  },
  throws(fn, code) {
    try {
      fn();
    } catch (err) {
      if (code !== undefined && err?.code !== code) {
        throw new AssertionError(`expected error code ${code}, got ${err?.code ?? err}`);
      }
      return err;
    }
    throw new AssertionError("expected the function to throw");
  },
  async rejects(promise, code) {
    try {
      await promise;
    } catch (err) {
      if (code !== undefined && err?.code !== code) {
        throw new AssertionError(`expected rejection code ${code}, got ${err?.code ?? err}`);
      }
      return err;
    }
    throw new AssertionError("expected the promise to reject");
  },
};

/**
 * Runs every registered test, one at a time.
 * @returns {Promise<{ passed: number, failed: number, failures: { test: string, error: string }[] }>}
 */
export async function run() {
  const failures = [];
  let passed = 0;
  for (const t of registry) {
    const label = t.file ? `${t.file} › ${t.name}` : t.name;
    try {
      await t.fn();
      passed++;
    } catch (err) {
      failures.push({ test: label, error: err?.stack || String(err) });
    }
  }
  return { passed, failed: failures.length, failures };
}
