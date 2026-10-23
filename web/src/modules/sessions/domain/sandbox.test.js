import { assert, file, test } from "../../../shared/testing/test.js";
import { accessOf, allowable, allowPathHref, filterViolations, hintFor, summarize, violationRows } from "./sandbox.js";

file("sessions/domain/sandbox");

const at = (m) => new Date(2026, 9, 23, 21, m);
const v = (operation, path, process, m) => ({ at: at(m), operation, path, process });
const log = [
  v("file-read-data", "/Users/me/.ssh/id_ed25519", "git", 41),
  v("file-write-create", "/Users/me/.cache/pnpm/tmp", "pnpm", 30),
  v("file-read-data", "/Users/me/.ssh/id_ed25519", "git", 20),
  v("network-outbound", "*:443", "node", 10),
];

test("an operation is a read, a write or something else", () => {
  assert.deepEqual(["file-read-data", "file-read-metadata", "file-write-create", "network-outbound", "process-exec"].map(accessOf), ["read", "read", "write", "other", "other"]);
});

test("repeats of the same operation, path and process are one row with a count and their span", () => {
  const rows = violationRows(log, true);
  assert.deepEqual(rows.map((r) => [r.path, r.count]), [["/Users/me/.ssh/id_ed25519", 2], ["/Users/me/.cache/pnpm/tmp", 1], ["*:443", 1]]);
  assert.deepEqual([rows[0].first, rows[0].last], [at(20), at(41)]);
  assert.equal(violationRows(log, false).length, 4);
});

test("filters by operation, and by path or process whatever the case", () => {
  assert.equal(filterViolations(log, { access: "write" }).length, 1);
  assert.equal(filterViolations(log, { query: "PNPM" }).length, 1);
  assert.equal(filterViolations(log, { query: ".SSH" }).length, 2);
  assert.equal(filterViolations(log, { access: "read", query: "pnpm" }).length, 0);
});

test("the summary counts denials and different paths", () => {
  assert.deepEqual(summarize(log), { denied: 4, paths: 3 });
});

test("credential paths are never offered for allowing, and say why", () => {
  assert.equal(allowable(log[0]), false);
  assert.ok(/SSH keys/.test(hintFor(log[0])));
  assert.equal(allowable(log[1]), true);
  assert.equal(allowable(log[3]), false);
});

test("Allow path goes to the settings' sandbox section with the path", () => {
  assert.equal(allowPathHref("/Users/me/.cache/pnpm"), "/agent-isolation?allow_path=%2FUsers%2Fme%2F.cache%2Fpnpm#sandbox");
});
